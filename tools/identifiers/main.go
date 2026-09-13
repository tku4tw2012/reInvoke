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
	wampRegister   = 64
	wampRegistered = 65
	wampInvocation = 68
	wampYield      = 70
)

var hexIdentity = regexp.MustCompile(`^[0-9a-f]{12}$`)

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
		"roles": map[string]interface{}{"callee": map[string]interface{}{}},
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

func (c *connection) register(procedure string) error {
	c.next++
	request := c.next
	if err := c.writeFrame([]interface{}{
		wampRegister, request, map[string]interface{}{}, procedure,
	}); err != nil {
		return err
	}
	for {
		message, err := c.readFrame()
		if err != nil {
			return err
		}
		if messageType(message) == wampRegistered {
			if id, ok := unsigned(message[1]); ok && id == request {
				return nil
			}
		}
		// A router may deliver other traffic before the reply; refusing here
		// would hide a genuine rejection, so only an explicit ERROR fails.
		if messageType(message) == 8 {
			return fmt.Errorf("registration rejected: %v", message)
		}
	}
}

func main() {
	log.SetFlags(0)
	host := flag.String("router-host", "127.0.0.1", "WAMP router host")
	port := flag.Int("router-port", 9999, "WAMP router port")
	realm := flag.String("realm", "default", "WAMP realm")
	identity := flag.String("identity-hex", "", "twelve lowercase hex digits shared by mac-hex and unique-hex")
	flag.Parse()

	value := strings.ToLower(strings.ReplaceAll(*identity, ":", ""))
	if !hexIdentity.MatchString(value) {
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
	const procedure = "com.harman.identifiersGet"
	if err := client.register(procedure); err != nil {
		log.Printf("IDENTIFIERS_REGISTER_FAILED: %v", err)
		os.Exit(1)
	}
	log.Printf("IDENTIFIERS_READY %s", procedure)

	result := map[string]interface{}{"mac-hex": value, "unique-hex": value}
	for {
		message, err := client.readFrame()
		if err != nil {
			log.Print("IDENTIFIERS_ROUTER_CLOSED")
			os.Exit(1)
		}
		if messageType(message) != wampInvocation || len(message) < 2 {
			continue
		}
		request, ok := unsigned(message[1])
		if !ok {
			continue
		}
		if err := client.writeFrame([]interface{}{
			wampYield, request, map[string]interface{}{}, []interface{}{}, result,
		}); err != nil {
			log.Print("IDENTIFIERS_YIELD_FAILED")
			os.Exit(1)
		}
		log.Print("IDENTIFIERS_ANSWERED")
	}
}
