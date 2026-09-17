// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
)

// readOneFrame reads a single RawSocket frame from the peer side of a pipe.
func readOneFrame(t *testing.T, socket net.Conn) []interface{} {
	t.Helper()
	header := make([]byte, 4)
	if _, err := io.ReadFull(socket, header); err != nil {
		t.Fatalf("read header: %v", err)
	}
	length := int(header[1])<<16 | int(header[2])<<8 | int(header[3])
	payload := make([]byte, length)
	if _, err := io.ReadFull(socket, payload); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	decoded, err := decodeMessagePack(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	message, ok := decoded.([]interface{})
	if !ok {
		t.Fatalf("frame is not an array: %T", decoded)
	}
	return message
}

// readFrameOrError reads one frame without needing the test goroutine, so a
// desynchronised stream is reported rather than crashing a helper.
func readFrameOrError(socket net.Conn) ([]interface{}, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(socket, header); err != nil {
		return nil, err
	}
	length := int(header[1])<<16 | int(header[2])<<8 | int(header[3])
	payload := make([]byte, length)
	if _, err := io.ReadFull(socket, payload); err != nil {
		return nil, err
	}
	decoded, err := decodeMessagePack(payload)
	if err != nil {
		return nil, err
	}
	message, ok := decoded.([]interface{})
	if !ok {
		return nil, errors.New("frame is not an array")
	}
	return message, nil
}

func writeOneFrame(t *testing.T, socket net.Conn, message []interface{}) {
	t.Helper()
	payload, err := encodeMessagePack(message)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	header := []byte{0, byte(len(payload) >> 16), byte(len(payload) >> 8), byte(len(payload))}
	if _, err := socket.Write(append(header, payload...)); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// yieldingConn records everything written and yields the processor inside each
// Write. Two goroutines writing an unsynchronised frame will therefore always
// interleave their header and payload, which makes the corruption deterministic
// instead of a race that usually happens not to fire.
type yieldingConn struct {
	mu      sync.Mutex
	written []byte
}

func (c *yieldingConn) Write(payload []byte) (int, error) {
	runtime.Gosched()
	c.mu.Lock()
	c.written = append(c.written, payload...)
	c.mu.Unlock()
	runtime.Gosched()
	return len(payload), nil
}
func (c *yieldingConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c *yieldingConn) Close() error                     { return nil }
func (c *yieldingConn) LocalAddr() net.Addr              { return nil }
func (c *yieldingConn) RemoteAddr() net.Addr             { return nil }
func (c *yieldingConn) SetDeadline(time.Time) error      { return nil }
func (c *yieldingConn) SetReadDeadline(time.Time) error  { return nil }
func (c *yieldingConn) SetWriteDeadline(time.Time) error { return nil }

// TestConcurrentWritesDoNotInterleave fails without the write mutex. A frame is
// a header write followed by a payload write, so two goroutines writing at once
// can emit header A, header B, payload A, payload B and desynchronise the peer
// permanently. The heartbeat ticker and the invocation answerer are different
// goroutines, so this is reachable in normal operation.
func TestConcurrentWritesDoNotInterleave(t *testing.T) {
	socket := &yieldingConn{}
	conn := newConnection(socket)

	const writers = 8
	const each = 12
	long := make([]byte, 64)
	for index := range long {
		long[index] = 'x'
	}

	var group sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		group.Add(1)
		go func(writer int) {
			defer group.Done()
			for index := 0; index < each; index++ {
				_ = conn.writeFrame([]interface{}{
					wampPublish, uint64(writer), map[string]interface{}{},
					string(long), []interface{}{},
				})
			}
		}(writer)
	}
	group.Wait()

	// Walk the recorded stream as a peer would. Any interleaving shows up as a
	// frame that does not decode or does not carry the payload it was given.
	stream := socket.written
	frames := 0
	for len(stream) > 0 {
		if len(stream) < 4 {
			t.Fatalf("stream ends mid-header after %d frames", frames)
		}
		length := int(stream[1])<<16 | int(stream[2])<<8 | int(stream[3])
		if len(stream) < 4+length {
			t.Fatalf("stream ends mid-payload after %d frames", frames)
		}
		decoded, err := decodeMessagePack(stream[4 : 4+length])
		if err != nil {
			t.Fatalf("frame %d did not decode: %v", frames, err)
		}
		message, ok := decoded.([]interface{})
		if !ok || len(message) != 5 {
			t.Fatalf("frame %d has the wrong shape", frames)
		}
		if topic, ok := message[3].(string); !ok || len(topic) != 64 {
			t.Fatalf("frame %d lost its payload", frames)
		}
		stream = stream[4+length:]
		frames++
	}
	if frames != writers*each {
		t.Fatalf("recovered %d frames, expected %d", frames, writers*each)
	}
}

// TestCallDoesNotStealInvocations fails against the previous implementation,
// which looped on readFrame inside the call and discarded anything that was
// not its own reply. An invocation that arrived during a call was dropped
// silently, so the caller waited forever for a response that was never sent.
func TestCallDoesNotStealInvocations(t *testing.T) {
	ours, theirs := net.Pipe()
	defer ours.Close()
	defer theirs.Close()
	conn := newConnection(ours)

	invocations := make(chan []interface{}, 4)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			message, err := conn.readFrame()
			if err != nil {
				return
			}
			if conn.deliver(message) {
				continue
			}
			invocations <- message
		}
	}()

	result := make(chan error, 1)
	go func() {
		_, err := conn.callProcedure("com.harman.bluetooth.stop", nil, 3*time.Second)
		result <- err
	}()

	call := readOneFrame(t, theirs)
	if messageType(call) != wampCall {
		t.Fatalf("expected CALL, got %v", call)
	}
	request, _ := unsigned(call[1])

	// Arrives while the call is outstanding. The old code ate this.
	writeOneFrame(t, theirs, []interface{}{
		wampInvocation, uint64(9001), uint64(1), map[string]interface{}{},
	})
	writeOneFrame(t, theirs, []interface{}{
		wampResult, request, map[string]interface{}{}, []interface{}{},
	})

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("call failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("call never completed")
	}
	select {
	case message := <-invocations:
		if id, _ := unsigned(message[1]); id != 9001 {
			t.Fatalf("wrong invocation surfaced: %v", message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("invocation arriving during a call was discarded")
	}
}

// TestCallLeavesNoDeadline fails against the previous implementation, which set
// a socket deadline for the call and never cleared it. The reader goroutine
// then inherited an expired deadline and died, taking the session with it, the
// first time one source displaced another.
func TestCallLeavesNoDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	peer := make(chan net.Conn, 1)
	go func() {
		accepted, err := listener.Accept()
		if err == nil {
			peer <- accepted
		}
	}()
	ours, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ours.Close()
	theirs := <-peer
	defer theirs.Close()

	conn := newConnection(ours)
	replies := make(chan []interface{}, 1)
	go func() {
		for {
			message, err := conn.readFrame()
			if err != nil {
				replies <- nil
				return
			}
			if conn.deliver(message) {
				continue
			}
			replies <- message
		}
	}()

	go func() {
		call := readOneFrame(t, theirs)
		request, _ := unsigned(call[1])
		writeOneFrame(t, theirs, []interface{}{
			wampResult, request, map[string]interface{}{}, []interface{}{},
		})
	}()
	if _, err := conn.callProcedure("com.harman.bluetooth.stop", nil, time.Second); err != nil {
		t.Fatalf("call failed: %v", err)
	}

	// Longer than the call timeout: if a deadline survived the call, the
	// reader is already dead and this invocation never arrives.
	time.Sleep(1500 * time.Millisecond)
	writeOneFrame(t, theirs, []interface{}{
		wampInvocation, uint64(4242), uint64(1), map[string]interface{}{},
	})
	select {
	case message := <-replies:
		if message == nil {
			t.Fatal("reader died after the call: a socket deadline survived it")
		}
		if id, _ := unsigned(message[1]); id != 4242 {
			t.Fatalf("unexpected frame: %v", message)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reader stopped delivering after a call")
	}
}

// TestCallTimesOutWithoutLeaking proves a call that is never answered gives up
// and does not retain its waiter.
func TestCallTimesOutWithoutLeaking(t *testing.T) {
	ours, theirs := net.Pipe()
	defer ours.Close()
	defer theirs.Close()
	conn := newConnection(ours)

	go func() { readOneFrame(t, theirs) }()
	_, err := conn.callProcedure("com.harman.bluetooth.stop", nil, 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout")
	}
	conn.pendingMu.Lock()
	outstanding := len(conn.pending)
	conn.pendingMu.Unlock()
	if outstanding != 0 {
		t.Fatalf("timed-out call left %d waiters behind", outstanding)
	}
}
