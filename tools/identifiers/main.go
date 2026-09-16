// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
//
// Answer com.harman.identifiersGet for the donor Bluetooth service.
//
// The donor stack calls this procedure during BtSystemControl initialisation
// and refuses to continue without it; the stock provider lives in the vendor
// system-manager, which this runtime does not ship. This supplies exactly that
// one procedure and nothing else, so the donor service can reach the stage
// where its own behaviour can be observed.

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	wampHello      = 1
	wampWelcome    = 2
	wampPublish    = 16
	wampCall       = 48
	wampResult     = 50
	wampRegister   = 64
	wampRegistered = 65
	wampInvocation = 68
	wampYield      = 70
)

var hexIdentity = regexp.MustCompile(`^[0-9a-f]{12}$`)

// stateProcedure is the donor's out-of-box-experience query. It calls this
// at startup; the reply opens its radio-enable gate.
const stateProcedure = "com.harman.stateGet"

// unsigned mirrors the decoder's integer handling: MessagePack may present the
// same value as any of these Go types depending on its encoded width.
func unsigned(value interface{}) (uint64, bool) {
	switch number := value.(type) {
	case uint64:
		return number, true
	case int64:
		if number >= 0 {
			return uint64(number), true
		}
	case int:
		if number >= 0 {
			return uint64(number), true
		}
	}
	return 0, false
}

type connection struct {
	socket net.Conn
	next   uint64
}

func (c *connection) writeFrame(message []interface{}) error {
	payload, err := encodeMessagePack(message)
	if err != nil {
		return err
	}
	if len(payload) > 0xffffff {
		return errors.New("frame exceeds RawSocket limit")
	}
	header := []byte{0, byte(len(payload) >> 16), byte(len(payload) >> 8), byte(len(payload))}
	if _, err := c.socket.Write(header); err != nil {
		return err
	}
	_, err = c.socket.Write(payload)
	return err
}

func (c *connection) readFrame() ([]interface{}, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(c.socket, header); err != nil {
		return nil, err
	}
	if header[0] != 0 {
		return nil, fmt.Errorf("unsupported RawSocket frame type %d", header[0])
	}
	length := int(header[1])<<16 | int(header[2])<<8 | int(header[3])
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.socket, payload); err != nil {
		return nil, err
	}
	decoded, err := decodeMessagePack(payload)
	if err != nil {
		return nil, err
	}
	message, ok := decoded.([]interface{})
	if !ok {
		return nil, errors.New("message is not an array")
	}
	return message, nil
}

func messageType(message []interface{}) uint64 {
	if len(message) == 0 {
		return 0
	}
	value, _ := unsigned(message[0])
	return value
}

func (c *connection) negotiate(realm string) error {
	if _, err := c.socket.Write([]byte{0x7f, 0xf2, 0, 0}); err != nil {
		return fmt.Errorf("write handshake: %w", err)
	}
	handshake := make([]byte, 4)
	if _, err := io.ReadFull(c.socket, handshake); err != nil {
		return fmt.Errorf("read handshake: %w", err)
	}
	if handshake[0] != 0x7f || handshake[1]&0x0f != 2 {
		return fmt.Errorf("handshake rejected: %x", handshake)
	}
	if err := c.writeFrame([]interface{}{wampHello, realm, map[string]interface{}{
		// publisher is required: bonefish rejects a PUBLISH from a session
		// that did not advertise the role and then closes the connection,
		// which reads exactly like a malformed payload. Observed live while
		// probing the donor's OOBE gate.
		"roles": map[string]interface{}{
			"callee":     map[string]interface{}{},
			"caller":     map[string]interface{}{},
			"publisher":  map[string]interface{}{},
			"subscriber": map[string]interface{}{},
		},
	}}); err != nil {
		return err
	}
	response, err := c.readFrame()
	if err != nil {
		return err
	}
	if messageType(response) != wampWelcome {
		return fmt.Errorf("expected WELCOME, received %v", response)
	}
	return nil
}

// publish emits a WAMP event. The frame carries positional args only, with no
// kwargs map, matching what the router logs for the runtime's own publishers.
func (c *connection) publish(topic string, args []interface{}) error {
	c.next++
	return c.writeFrame([]interface{}{
		wampPublish, c.next, map[string]interface{}{}, topic, args,
	})
}

func (c *connection) register(procedure string) (uint64, error) {
	c.next++
	request := c.next
	if err := c.writeFrame([]interface{}{
		wampRegister, request, map[string]interface{}{}, procedure,
	}); err != nil {
		return 0, err
	}
	for {
		message, err := c.readFrame()
		if err != nil {
			return 0, err
		}
		if messageType(message) == wampRegistered && len(message) > 2 {
			if id, ok := unsigned(message[1]); ok && id == request {
				registration, _ := unsigned(message[2])
				return registration, nil
			}
		}
		// A router may deliver other traffic before the reply; refusing here
		// would hide a genuine rejection, so only an explicit ERROR fails.
		if messageType(message) == 8 {
			return 0, fmt.Errorf("registration rejected: %v", message)
		}
	}
}

func (c *connection) callProcedure(procedure string, arguments []interface{}, timeout time.Duration) ([]interface{}, error) {
	if timeout <= 0 {
		return nil, errors.New("call timeout must be positive")
	}
	if err := c.socket.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	c.next++
	request := c.next
	if err := c.writeFrame([]interface{}{
		wampCall, request, map[string]interface{}{}, procedure, arguments,
	}); err != nil {
		return nil, fmt.Errorf("send call: %w", err)
	}
	for {
		message, err := c.readFrame()
		if err != nil {
			return nil, fmt.Errorf("read call reply: %w", err)
		}
		switch messageType(message) {
		case wampResult:
			if len(message) < 3 {
				return nil, errors.New("malformed call result")
			}
			if id, ok := unsigned(message[1]); ok && id == request {
				return message, nil
			}
		case 8:
			if len(message) < 5 {
				return nil, errors.New("malformed call error")
			}
			original, _ := unsigned(message[1])
			if id, ok := unsigned(message[2]); original == wampCall && ok && id == request {
				return nil, fmt.Errorf("call rejected: %v", message[4:])
			}
		}
	}
}

func main() {
	log.SetFlags(0)
	host := flag.String("router-host", "127.0.0.1", "WAMP router host")
	port := flag.Int("router-port", 9999, "WAMP router port")
	realm := flag.String("realm", "default", "WAMP realm")
	identity := flag.String("identity-hex", "", "twelve lowercase hex digits shared by mac-hex and unique-hex")
	identityFile := flag.String("identity-file", "", "path to a file holding the identity hex; wins over -identity-hex when set")
	deviceName := flag.String("device-name", "reInvoke", "name reported to the donor service")
	deviceNameFile := flag.String("device-name-file", "", "path to a file holding the device name; wins over -device-name when set")
	call := flag.String("call", "", "diagnostic: invoke this procedure and exit")
	callArgs := flag.String("call-args", "", "diagnostic: JSON array of positional arguments")
	flag.Parse()

	// A file input is read here rather than shell-expanded into a flag by the
	// init. Candidate 05 used "--identity-hex $(cat ...)" against a path the
	// bootstrap shadows with its own bind mount, so the flag silently became
	// an empty string and this service rejected it and crash-looped forever.
	// Reading the file directly turns that into a specific, named failure.
	if *identityFile != "" {
		content, err := os.ReadFile(*identityFile)
		if err != nil {
			log.Printf("IDENTIFIERS_IDENTITY_FILE_UNREADABLE %s: %v", *identityFile, err)
			os.Exit(1)
		}
		*identity = strings.TrimSpace(string(content))
	}
	if *deviceNameFile != "" {
		content, err := os.ReadFile(*deviceNameFile)
		if err != nil {
			log.Printf("IDENTIFIERS_NAME_FILE_UNREADABLE %s: %v", *deviceNameFile, err)
			os.Exit(1)
		}
		*deviceName = strings.TrimSpace(string(content))
	}

	value := strings.ToLower(strings.ReplaceAll(*identity, ":", ""))
	// Only the serving mode needs an identity; the diagnostic caller does not.
	if *call == "" && !hexIdentity.MatchString(value) {
		log.Print("IDENTIFIERS_IDENTITY_INVALID")
		os.Exit(1)
	}

	address := net.JoinHostPort(*host, fmt.Sprint(*port))
	socket, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		log.Print("IDENTIFIERS_ROUTER_UNAVAILABLE")
		os.Exit(1)
	}
	defer socket.Close()

	client := &connection{socket: socket}
	if err := client.negotiate(*realm); err != nil {
		log.Printf("IDENTIFIERS_REALM_REJECTED: %v", err)
		os.Exit(1)
	}

	// Diagnostic mode: invoke one procedure and report the reply. The donor
	// stack is event driven, so activation has to be observed, not assumed.
	if *call != "" {
		var arguments []interface{}
		if *callArgs != "" {
			if err := json.Unmarshal([]byte(*callArgs), &arguments); err != nil {
				log.Printf("IDENTIFIERS_CALL_ARGS_INVALID: %v", err)
				os.Exit(1)
			}
		}
		reply, err := client.callProcedure(*call, arguments, 15*time.Second)
		if err != nil {
			log.Printf("IDENTIFIERS_CALL_FAILED: %v", err)
			os.Exit(1)
		}
		log.Printf("IDENTIFIERS_CALL_REPLY %v", reply)
		return
	}
	const procedure = "com.harman.identifiersGet"
	identityResult := map[string]interface{}{"mac-hex": value, "unique-hex": value}

	// The donor service calls several procedures that the vendor
	// system-manager used to provide. Their argument shapes are not
	// documented, so every invocation is logged and answered permissively;
	// the log is the evidence for what the donor actually expects.
	//
	// com.harman.volumeGet is deliberately ABSENT: reinvoke-mcu-interface
	// owns it, and whichever service registers first wins. Candidate 05 hid
	// this because its identity provider crash-looped and never registered
	// anything. Once the identity was fixed, this service won the race and
	// mcu-interface could never complete registration, so it reconnected
	// every five seconds forever and every physical control was dead while
	// the LEDs still animated. Observed on hardware; MCU registration
	// succeeded immediately once this service released the name. Nothing
	// here may claim a procedure mcu-interface owns (tools/mcu-interface/
	// wamp.go's `procedures`).
	responses := map[string]map[string]interface{}{
		procedure: identityResult,
		// The donor CALLS this at startup and gates its entire radio-enable
		// path on the answer. Recovered from the binary, then confirmed live:
		// wamp_on_pair returns immediately unless byte 0x95 is set, that byte
		// is written only by handle_oobe_state_change, and the reply is
		// matched against "system" then "normal". Answering produces:
		//   system state is normal
		//   OOBE is finished, initializing...
		// Nothing else in this runtime provides it, so without this the
		// adapter is never enabled and hci0 stays 00:00:00:00:00:00.
		stateProcedure:                 {"system": map[string]interface{}{"state": "normal"}},
		"com.harman.deviceNameGet":     {"name": *deviceName, "device-name": *deviceName},
		"com.harman.source.register":   {},
		"com.harman.source.get-active": {"source": "bluetooth"},
	}
	names := make(map[uint64]string, len(responses))
	for name := range responses {
		registration, err := client.register(name)
		if err != nil {
			// The runtime already provides some of these. Only the gaps are
			// this service's business, so an existing owner is not an error.
			if strings.Contains(err.Error(), "procedure_already_exists") {
				log.Printf("IDENTIFIERS_ALREADY_PROVIDED %s", name)
				continue
			}
			log.Printf("IDENTIFIERS_REGISTER_FAILED %s: %v", name, err)
			os.Exit(1)
		}
		names[registration] = name
	}
	log.Printf("IDENTIFIERS_READY %d procedures", len(responses))

	for {
		message, err := client.readFrame()
		if err != nil {
			log.Print("IDENTIFIERS_ROUTER_CLOSED")
			os.Exit(1)
		}
		if messageType(message) != wampInvocation || len(message) < 3 {
			continue
		}
		request, ok := unsigned(message[1])
		if !ok {
			continue
		}
		registration, _ := unsigned(message[2])
		name := names[registration]
		log.Printf("IDENTIFIERS_CALL %s payload=%v", name, message[3:])
		result := responses[name]
		if result == nil {
			result = map[string]interface{}{}
		}
		if err := client.writeFrame([]interface{}{
			wampYield, request, map[string]interface{}{}, []interface{}{}, result,
		}); err != nil {
			log.Print("IDENTIFIERS_YIELD_FAILED")
			os.Exit(1)
		}
		log.Printf("IDENTIFIERS_ANSWERED %s", name)
	}
}
