// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

// WAMP transport for the source manager.
//
// Framing and encoding follow the identifiers service, because a second
// dialect of the same protocol against the same router is a defect waiting to
// happen. The session handling deliberately differs: identifiers only answers
// calls, while this service also makes them, and that changes the threading
// rules. See callProcedure.

package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
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

	// writeMu serialises frame writes. A frame is a header write followed by a
	// payload write, and the heartbeat ticker runs on a different goroutine
	// from the one answering invocations, so unsynchronised writes interleave
	// two frames and desynchronise the stream for good.
	writeMu sync.Mutex

	// pending routes call replies back to the caller. Only the reader
	// goroutine reads the socket; a second read loop inside a call would steal
	// and discard invocations that arrived while it waited.
	pendingMu sync.Mutex
	pending   map[uint64]chan []interface{}
}

func newConnection(socket net.Conn) *connection {
	return &connection{socket: socket, pending: map[uint64]chan []interface{}{}}
}

// deliver hands a call reply to the waiting caller. It reports whether the
// message was a reply, so the reader can fall through to invocations.
func (c *connection) deliver(message []interface{}) bool {
	var id uint64
	switch messageType(message) {
	case wampResult:
		if len(message) < 2 {
			return false
		}
		value, ok := unsigned(message[1])
		if !ok {
			return false
		}
		id = value
	case wampError:
		if len(message) < 3 {
			return false
		}
		original, _ := unsigned(message[1])
		if original != wampCall {
			return false
		}
		value, ok := unsigned(message[2])
		if !ok {
			return false
		}
		id = value
	default:
		return false
	}
	c.pendingMu.Lock()
	waiter, waiting := c.pending[id]
	if waiting {
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
	if !waiting {
		return true
	}
	waiter <- message
	return true
}

func (c *connection) writeFrame(message []interface{}) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
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

// callProcedure invokes a remote procedure and waits for its reply. It never
// reads the socket itself: the reader goroutine owns reads and hands the reply
// over. The earlier version looped on readFrame here, which both discarded
// invocations that arrived while it waited and left a socket deadline set that
// later killed the reader.
func (c *connection) callProcedure(procedure string, arguments []interface{}, timeout time.Duration) ([]interface{}, error) {
	if timeout <= 0 {
		return nil, errors.New("call timeout must be positive")
	}
	c.pendingMu.Lock()
	c.next++
	request := c.next
	waiter := make(chan []interface{}, 1)
	c.pending[request] = waiter
	c.pendingMu.Unlock()
	abandon := func() {
		c.pendingMu.Lock()
		delete(c.pending, request)
		c.pendingMu.Unlock()
	}
	if err := c.writeFrame([]interface{}{
		wampCall, request, map[string]interface{}{}, procedure, arguments,
	}); err != nil {
		abandon()
		return nil, fmt.Errorf("send call: %w", err)
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case message := <-waiter:
		if messageType(message) == wampError {
			return nil, fmt.Errorf("call rejected: %v", message[4:])
		}
		if len(message) < 3 {
			return nil, errors.New("malformed call result")
		}
		return message, nil
	case <-timer.C:
		abandon()
		return nil, fmt.Errorf("call %s timed out", procedure)
	}
}

