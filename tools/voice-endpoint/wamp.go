// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
	"unicode/utf8"

	"github.com/tku4tw2012/reinvoke/tools/control/msgpack"
)

const (
	controlTimeout      = 2 * time.Second
	maxControlFrame     = 64 * 1024
	maxControlSkip      = 32
	maxControlErrorArgs = 512
)

// Phase calls are serialized and bounded independently of the button subscriber
// so an unresponsive feedback owner cannot block the worker's audio pipes.
func callWAMP(ctx context.Context, address, realm, procedure string, args []interface{}, kwargs ...map[string]interface{}) error {
	if len(kwargs) > 1 {
		return errors.New("too many WAMP keyword argument dictionaries")
	}
	ctx, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	connection, peerLimit, err := openControlWAMP(ctx, address, realm, "caller")
	if err != nil {
		return err
	}
	defer connection.Close()
	stop := closeOnCancel(ctx, connection)
	defer stop()
	const requestID = uint64(1)
	message := []interface{}{48, requestID, map[string]interface{}{}, procedure, args}
	if len(kwargs) == 1 {
		message = append(message, kwargs[0])
	}
	if err := writeWAMP(connection, peerLimit, message); err != nil {
		return fmt.Errorf("send WAMP CALL: %w", err)
	}
	for count := 0; count < maxControlSkip; count++ {
		message, err := readWAMP(connection, peerLimit)
		if err != nil {
			return fmt.Errorf("read WAMP CALL response: %w", err)
		}
		switch controlNumber(message[0]) {
		case 50:
			if len(message) < 3 || len(message) > 5 {
				return errors.New("invalid WAMP RESULT")
			}
			if controlNumber(message[1]) != requestID {
				continue
			}
			details, ok := message[2].(map[string]interface{})
			if !ok || details["progress"] != nil && details["progress"] != false {
				return errors.New("invalid or unexpected progressive WAMP RESULT")
			}
			if err := checkWAMPArguments(message[3:]); err != nil {
				return err
			}
			return nil
		case 8:
			if len(message) < 5 || len(message) > 7 {
				return errors.New("invalid WAMP ERROR")
			}
			if controlNumber(message[1]) != 48 || controlNumber(message[2]) != requestID {
				continue
			}
			if _, ok := message[3].(map[string]interface{}); !ok {
				return errors.New("invalid WAMP ERROR details")
			}
			uri, ok := message[4].(string)
			if !ok || len(uri) > 256 {
				return errors.New("WAMP CALL rejected with invalid error URI")
			}
			arguments, err := wampErrorArguments(message[5:])
			if err != nil {
				return fmt.Errorf("WAMP CALL rejected: %q (invalid ERROR arguments: %w)", uri, err)
			}
			return fmt.Errorf("WAMP CALL rejected: %q%s", uri, arguments)
		case 6:
			return errors.New("WAMP router closed the session before CALL completion")
		}
	}
	return errors.New("WAMP router sent too many unrelated CALL responses")
}

func openControlWAMP(ctx context.Context, address, realm, role string) (net.Conn, int, error) {
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, 0, fmt.Errorf("connect WAMP router: %w", err)
	}
	ready := false
	defer func() {
		if !ready {
			connection.Close()
		}
	}()
	stop := closeOnCancel(ctx, connection)
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := connection.SetDeadline(deadline); err != nil {
		return nil, 0, fmt.Errorf("set WAMP deadline: %w", err)
	}
	if err := writeAll(connection, []byte{0x7f, 0xf2, 0, 0}); err != nil {
		return nil, 0, fmt.Errorf("write RawSocket handshake: %w", err)
	}
	var handshake [4]byte
	if _, err := io.ReadFull(connection, handshake[:]); err != nil {
		return nil, 0, fmt.Errorf("read RawSocket handshake: %w", err)
	}
	if handshake[0] != 0x7f || handshake[1]&0x0f != 2 || handshake[2] != 0 || handshake[3] != 0 {
		return nil, 0, errors.New("WAMP RawSocket handshake rejected")
	}
	peerLimit := 1 << (9 + uint(handshake[1]>>4))
	if err := writeWAMP(connection, peerLimit, []interface{}{
		1, realm, map[string]interface{}{
			"roles": map[string]interface{}{role: map[string]interface{}{}},
		},
	}); err != nil {
		return nil, 0, fmt.Errorf("send WAMP HELLO: %w", err)
	}
	welcome, err := readWAMP(connection, peerLimit)
	if err != nil {
		return nil, 0, fmt.Errorf("read WAMP WELCOME: %w", err)
	}
	if len(welcome) != 3 || controlNumber(welcome[0]) != 2 || controlNumber(welcome[1]) == 0 {
		return nil, 0, errors.New("WAMP router did not send a valid WELCOME")
	}
	if _, ok := welcome[2].(map[string]interface{}); !ok {
		return nil, 0, errors.New("WAMP WELCOME details are invalid")
	}
	ready = true
	return connection, peerLimit, nil
}

func wampErrorArguments(arguments []interface{}) (string, error) {
	if err := checkWAMPArguments(arguments); err != nil {
		return "", err
	}
	if len(arguments) == 0 {
		return "", nil
	}
	data, err := json.Marshal(arguments[0])
	if err != nil {
		return "", err
	}
	if len(data) > maxControlErrorArgs {
		const suffix = "... (truncated)"
		length := maxControlErrorArgs - len(suffix)
		for !utf8.Valid(data[:length]) {
			length--
		}
		return " args=" + string(data[:length]) + suffix, nil
	}
	return " args=" + string(data), nil
}

func checkWAMPArguments(arguments []interface{}) error {
	if len(arguments) > 0 {
		if _, ok := arguments[0].([]interface{}); !ok {
			return errors.New("invalid WAMP positional results")
		}
	}
	if len(arguments) > 1 {
		if _, ok := arguments[1].(map[string]interface{}); !ok {
			return errors.New("invalid WAMP keyword results")
		}
	}
	return nil
}

func controlNumber(value interface{}) uint64 {
	switch number := value.(type) {
	case uint64:
		return number
	case int64:
		if number >= 0 {
			return uint64(number)
		}
	}
	return 0
}

func writeControlFrame(writer io.Writer, kind byte, payload []byte, peerLimit int) error {
	if len(payload) > maxControlFrame || len(payload) > peerLimit {
		return errors.New("WAMP frame exceeds the bounded peer limit")
	}
	header := []byte{kind, byte(len(payload) >> 16), byte(len(payload) >> 8), byte(len(payload))}
	if err := writeAll(writer, header); err != nil {
		return err
	}
	return writeAll(writer, payload)
}

func writeWAMP(writer io.Writer, peerLimit int, message []interface{}) error {
	payload, err := msgpack.Encode(message)
	if err != nil {
		return err
	}
	return writeControlFrame(writer, 0, payload, peerLimit)
}

func readWAMP(connection net.Conn, peerLimit int) ([]interface{}, error) {
	for skipped := 0; skipped < maxControlSkip; skipped++ {
		var header [4]byte
		if _, err := io.ReadFull(connection, header[:]); err != nil {
			return nil, err
		}
		length := int(header[1])<<16 | int(header[2])<<8 | int(header[3])
		if length > maxControlFrame || header[0] > 2 {
			return nil, errors.New("invalid or oversized WAMP RawSocket frame")
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(connection, payload); err != nil {
			return nil, err
		}
		switch header[0] {
		case 1:
			if err := writeControlFrame(connection, 2, payload, peerLimit); err != nil {
				return nil, err
			}
			continue
		case 2:
			continue
		}
		decoded, err := msgpack.Decode(payload)
		if err != nil {
			return nil, fmt.Errorf("invalid WAMP MessagePack: %w", err)
		}
		message, ok := decoded.([]interface{})
		if !ok || len(message) == 0 || controlNumber(message[0]) == 0 {
			return nil, errors.New("WAMP message must be a typed array")
		}
		return message, nil
	}
	return nil, errors.New("WAMP router sent too many control frames")
}
