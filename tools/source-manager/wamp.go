// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

// WAMP transport for the source manager.
//
// Lifted unchanged from the identifiers service rather than rewritten: both
// speak the same RawSocket framing to the same router, and a second dialect
// of the same protocol is a defect waiting to happen.

package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

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

