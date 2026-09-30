// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/tku4tw2012/reinvoke/tools/control/msgpack"
)

func startTestRouter(t *testing.T, sessions int, handler func(int, net.Conn) error) (string, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result, finished := make(chan error, 1), make(chan struct{})
	go func() {
		defer close(finished)
		for session := 0; session < sessions; session++ {
			connection, err := listener.Accept()
			if err != nil {
				result <- err
				return
			}
			stop := closeOnCancel(ctx, connection)
			err = connection.SetDeadline(time.Now().Add(3 * time.Second))
			if err == nil {
				err = handler(session, connection)
			}
			connection.Close()
			stop()
			if err != nil {
				result <- err
				return
			}
		}
		result <- nil
	}()
	t.Cleanup(func() {
		cancel()
		listener.Close()
		select {
		case <-finished:
		case <-time.After(4 * time.Second):
			t.Error("test router did not stop")
		}
	})
	return listener.Addr().String(), result
}

func readTestFrame(connection net.Conn) (byte, []byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(connection, header[:]); err != nil {
		return 0, nil, err
	}
	length := int(header[1])<<16 | int(header[2])<<8 | int(header[3])
	if length > maxControlFrame {
		return 0, nil, errors.New("client emitted an oversized control frame")
	}
	payload := make([]byte, length)
	_, err := io.ReadFull(connection, payload)
	return header[0], payload, err
}

func readTestWAMP(connection net.Conn) ([]interface{}, error) {
	kind, payload, err := readTestFrame(connection)
	if err != nil {
		return nil, err
	}
	if kind != 0 {
		return nil, errors.New("expected WAMP data frame")
	}
	value, err := msgpack.Decode(payload)
	if err != nil {
		return nil, err
	}
	message, ok := value.([]interface{})
	if !ok {
		return nil, errors.New("expected WAMP array")
	}
	return message, nil
}

func acceptTestWAMPHello(connection net.Conn, realm string) error {
	return acceptTestWAMPHelloRole(connection, realm, "caller")
}

func acceptTestWAMPHelloRole(connection net.Conn, realm, role string) error {
	var handshake [4]byte
	if _, err := io.ReadFull(connection, handshake[:]); err != nil {
		return err
	}
	if handshake != [4]byte{0x7f, 0xf2, 0, 0} {
		return errors.New("client did not use the specified RawSocket handshake")
	}
	if err := writeAll(connection, handshake[:]); err != nil {
		return err
	}
	message, err := readTestWAMP(connection)
	if err != nil {
		return err
	}
	expected := []interface{}{
		uint64(1), realm,
		map[string]interface{}{"roles": map[string]interface{}{role: map[string]interface{}{}}},
	}
	if !reflect.DeepEqual(message, expected) {
		return errors.New("WAMP HELLO did not advertise exactly the expected role and realm")
	}
	return nil
}

func welcomeTestWAMP(connection net.Conn) error {
	return writeWAMP(connection, maxControlFrame, []interface{}{2, uint64(77), map[string]interface{}{}})
}

func checkTestLEDCall(message []interface{}, procedure string, args []interface{}) error {
	if len(message) != 5 || controlNumber(message[0]) != 48 || controlNumber(message[1]) != 1 {
		return errors.New("unexpected WAMP CALL shape or request ID")
	}
	if options, ok := message[2].(map[string]interface{}); !ok || len(options) != 0 {
		return errors.New("unexpected WAMP CALL options")
	}
	if message[3] != procedure || !reflect.DeepEqual(message[4], args) {
		return errors.New("LED procedure or positional arguments differ from the MCU contract")
	}
	return nil
}

func TestWAMPCallerHandshakePingAndResponseCorrelation(t *testing.T) {
	args := []interface{}{"L_101_c_listening", uint64(1)}
	address, server := startTestRouter(t, 1, func(_ int, connection net.Conn) error {
		if err := acceptTestWAMPHello(connection, "default"); err != nil {
			return err
		}
		ping := []byte{0, 255, 7}
		if err := writeControlFrame(connection, 1, ping, maxControlFrame); err != nil {
			return err
		}
		kind, pong, err := readTestFrame(connection)
		if err != nil || kind != 2 || !bytes.Equal(ping, pong) {
			return errors.New("RawSocket ping was not answered with its exact payload")
		}
		if err := welcomeTestWAMP(connection); err != nil {
			return err
		}
		call, err := readTestWAMP(connection)
		if err != nil {
			return err
		}
		if err := checkTestLEDCall(call, "com.harman.ledAnimate", args); err != nil {
			return err
		}
		for _, response := range [][]interface{}{
			{50, uint64(2), map[string]interface{}{}},
			{8, 64, uint64(1), map[string]interface{}{}, "wamp.error.unrelated_request_type"},
			{8, 48, uint64(2), map[string]interface{}{}, "wamp.error.unrelated_request_id"},
			{50, uint64(1), map[string]interface{}{"progress": false}, []interface{}{}},
		} {
			if err := writeWAMP(connection, maxControlFrame, response); err != nil {
				return err
			}
		}
		return nil
	})
	if err := callWAMP(context.Background(), address, "default", "com.harman.ledAnimate", args); err != nil {
		t.Fatal(err)
	}
	if err := <-server; err != nil {
		t.Fatal(err)
	}
}

func TestWAMPRejectsErrorsMalformedResultsAndUnrelatedFlood(t *testing.T) {
	for name, replies := range map[string][][]interface{}{
		"matched-error":     {{8, 48, uint64(1), map[string]interface{}{}, "wamp.error.no_such_procedure"}},
		"wrong-result-id":   {{50, uint64(2), map[string]interface{}{}}},
		"short-result":      {{50, uint64(1)}},
		"bad-details":       {{50, uint64(1), "not a map"}},
		"progressive":       {{50, uint64(1), map[string]interface{}{"progress": true}}},
		"bad-args":          {{50, uint64(1), map[string]interface{}{}, "not an array"}},
		"bad-kwargs":        {{50, uint64(1), map[string]interface{}{}, []interface{}{}, "not a map"}},
		"goodbye":           {{6, map[string]interface{}{}, "wamp.close.normal"}},
		"short-error":       {{8, 48, uint64(1)}},
		"bad-error-details": {{8, 48, uint64(1), "not a map", "wamp.error.failure"}},
		"bad-error-uri":     {{8, 48, uint64(1), map[string]interface{}{}, false}},
		"bad-error-args":    {{8, 48, uint64(1), map[string]interface{}{}, "com.harman.error", "not an array"}},
		"bad-error-kwargs": {{8, 48, uint64(1), map[string]interface{}{}, "com.harman.error",
			[]interface{}{"invalid argument format"}, "not a map"}},
		"unrelated-flood": repeatControlMessage([]interface{}{50, uint64(2), map[string]interface{}{}}, maxControlSkip),
	} {
		t.Run(name, func(t *testing.T) {
			address, _ := startTestRouter(t, 1, func(_ int, connection net.Conn) error {
				if err := acceptTestWAMPHello(connection, "default"); err != nil {
					return err
				}
				if err := welcomeTestWAMP(connection); err != nil {
					return err
				}
				if _, err := readTestWAMP(connection); err != nil {
					return err
				}
				for _, reply := range replies {
					if err := writeWAMP(connection, maxControlFrame, reply); err != nil {
						return err
					}
				}
				return nil
			})
			if err := callWAMP(context.Background(), address, "default", "com.harman.ledOff", []interface{}{}); err == nil {
				t.Fatal("invalid or unrelated WAMP response became success")
			}
		})
	}
}

func repeatControlMessage(message []interface{}, count int) [][]interface{} {
	messages := make([][]interface{}, count)
	for i := range messages {
		messages[i] = message
	}
	return messages
}

func TestWAMPErrorArgumentLogging(t *testing.T) {
	for _, test := range []struct {
		name      string
		args      []interface{}
		want      string
		truncated bool
	}{
		{"mcu-detail", []interface{}{"invalid argument format"}, `args=["invalid argument format"]`, false},
		{"escaped", []interface{}{"invalid\nargument\rformat\t\x00"}, `args=["invalid\nargument\rformat\t\u0000"]`, false},
		{"bounded-ascii", []interface{}{strings.Repeat("x", 2000)}, `args=["xxx`, true},
		{"bounded-utf8", []interface{}{strings.Repeat("\u00e9", 1000)}, `args=["`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			address, server := startTestRouter(t, 1, func(_ int, connection net.Conn) error {
				if err := acceptTestWAMPHello(connection, "default"); err != nil {
					return err
				}
				if err := welcomeTestWAMP(connection); err != nil {
					return err
				}
				if _, err := readTestWAMP(connection); err != nil {
					return err
				}
				return writeWAMP(connection, maxControlFrame, []interface{}{
					8, 48, uint64(1), map[string]interface{}{}, "com.harman.error", test.args,
				})
			})
			path := filepath.Join(t.TempDir(), "microphone-state")
			writeMicrophoneState(t, path, "unmuted\n")
			var output bytes.Buffer
			feedback := &phaseFeedback{
				statePath: path, logger: log.New(&output, "", 0),
				call: func(ctx context.Context, procedure string, args []interface{}, kwargs map[string]interface{}) error {
					return callWAMP(ctx, address, "default", procedure, args, kwargs)
				},
			}
			feedback.apply(context.Background(), phaseListening)
			if err := <-server; err != nil {
				t.Fatal(err)
			}
			text := output.String()
			if !strings.Contains(text, `voice listening failed: WAMP CALL rejected: "com.harman.error"`) ||
				!strings.Contains(text, test.want) || feedback.last != "" {
				t.Fatalf("MCU rejection lost its failure status or detail arguments: %s", text)
			}
			if !utf8.ValidString(text) || strings.Count(text, "\n") != 1 {
				t.Fatal("WAMP error arguments were not kept on one valid UTF-8 log line")
			}
			start := strings.Index(text, " args=")
			detail := strings.TrimSuffix(text[start+len(" args="):], "\n")
			if len(detail) > 512 || strings.HasSuffix(detail, "... (truncated)") != test.truncated {
				t.Fatalf("WAMP error arguments are not bounded to 512 bytes: %d", len(detail))
			}
		})
	}
}

func TestWAMPHandshakeAndWelcomeRejections(t *testing.T) {
	for _, handshake := range [][4]byte{
		{0, 0xf2, 0, 0}, {0x7f, 0xf1, 0, 0}, {0x7f, 0xf2, 1, 0}, {0x7f, 0x10, 0, 0},
	} {
		handshake := handshake
		address, _ := startTestRouter(t, 1, func(_ int, connection net.Conn) error {
			var request [4]byte
			if _, err := io.ReadFull(connection, request[:]); err != nil {
				return err
			}
			return writeAll(connection, handshake[:])
		})
		if err := callWAMP(context.Background(), address, "default", "com.harman.ledOff", []interface{}{}); err == nil {
			t.Fatal("rejected RawSocket handshake became success")
		}
	}
	for _, welcome := range [][]interface{}{
		{3, map[string]interface{}{}, "wamp.error.no_such_realm"},
		{2, uint64(0), map[string]interface{}{}},
		{2, uint64(1), "not a map"},
		{2},
	} {
		welcome := welcome
		address, _ := startTestRouter(t, 1, func(_ int, connection net.Conn) error {
			if err := acceptTestWAMPHello(connection, "default"); err != nil {
				return err
			}
			return writeWAMP(connection, maxControlFrame, welcome)
		})
		if err := callWAMP(context.Background(), address, "default", "com.harman.ledOff", []interface{}{}); err == nil {
			t.Fatal("invalid WAMP WELCOME became success")
		}
	}
}

func TestWAMPFrameBoundsAndCodecValidation(t *testing.T) {
	for name, frame := range map[string][]byte{
		"oversized": {0, 1, 0, 1}, "unknown-kind": {3, 0, 0, 0}, "empty": {0, 0, 0, 0},
		"not-array": {0, 0, 0, 1, 0xc0}, "trailing-msgpack": {0, 0, 0, 3, 0x91, 0x02, 0xc0},
		"untyped-array": {0, 0, 0, 2, 0x91, 0xc0},
	} {
		t.Run(name, func(t *testing.T) {
			connection, server := net.Pipe()
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer server.Close()
				writeAll(server, frame)
			}()
			_, err := readWAMP(connection, maxControlFrame)
			connection.Close()
			<-done
			if err == nil {
				t.Fatal("invalid RawSocket/MessagePack frame accepted")
			}
		})
	}
	if err := writeControlFrame(io.Discard, 0, make([]byte, maxControlFrame+1), maxControlFrame+1); err == nil {
		t.Fatal("outbound control frame exceeded the local bound")
	}
	if err := writeControlFrame(io.Discard, 0, make([]byte, 513), 512); err == nil {
		t.Fatal("outbound control frame exceeded the negotiated peer bound")
	}
}

func TestWAMPCancellationInterruptsUnresponsiveRouter(t *testing.T) {
	ready := make(chan struct{})
	address, _ := startTestRouter(t, 1, func(_ int, connection net.Conn) error {
		if err := acceptTestWAMPHello(connection, "default"); err != nil {
			return err
		}
		if err := welcomeTestWAMP(connection); err != nil {
			return err
		}
		if _, err := readTestWAMP(connection); err != nil {
			return err
		}
		close(ready)
		var data [1]byte
		_, err := connection.Read(data[:])
		return err
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- callWAMP(ctx, address, "default", "com.harman.ledOff", []interface{}{}) }()
	<-ready
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled WAMP call reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt control I/O")
	}
}

func writeMicrophoneState(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestFeedbackWAMPPhaseOrderingDeduplicationAndNoInitialIdle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "microphone-state")
	writeMicrophoneState(t, path, "unmuted\n")
	expected := []string{"listening", "thinking", "speaking", ""}
	address, server := startTestRouter(t, len(expected), func(index int, connection net.Conn) error {
		if err := acceptTestWAMPHello(connection, "default"); err != nil {
			return err
		}
		if err := welcomeTestWAMP(connection); err != nil {
			return err
		}
		call, err := readTestWAMP(connection)
		if err != nil {
			return err
		}
		if err := checkTestVoiceCall(call, expected[index]); err != nil {
			return fmt.Errorf("phase %d: %w", index, err)
		}
		return writeWAMP(connection, maxControlFrame, []interface{}{50, uint64(1), map[string]interface{}{}})
	})
	completed := make(chan struct{}, 1)
	feedback := newFeedback(context.Background(), path, log.New(io.Discard, "", 0),
		func(ctx context.Context, procedure string, args []interface{}, kwargs map[string]interface{}) error {
			err := callWAMP(ctx, address, "default", procedure, args, kwargs)
			if kwargs["state"] == "" && err == nil {
				completed <- struct{}{}
			}
			return err
		})
	defer feedback.close()
	for _, phase := range []voicePhase{
		phaseIdle, phaseIdle, phaseListening, phaseListening, phaseThinking, phaseSpeaking, phaseIdle, phaseIdle,
	} {
		feedback.publish(phase)
	}
	select {
	case <-completed:
	case <-time.After(3 * time.Second):
		t.Fatal("ordered voice feedback did not complete")
	}
	feedback.close()
	if err := <-server; err != nil {
		t.Fatal(err)
	}
}

func TestFeedbackMuteGateSuppressesActivePhasesButAlwaysCleansUp(t *testing.T) {
	for _, content := range []string{"muted", "muted\n", "", "unmuted \n", "UNMUTED\n", strings.Repeat("x", 100)} {
		t.Run(fmt.Sprintf("%q", content[:minLength(len(content), 12)]), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "microphone-state")
			writeMicrophoneState(t, path, content)
			var output bytes.Buffer
			calls := 0
			feedback := &phaseFeedback{
				statePath: path, logger: log.New(&output, "", 0), touched: true,
				call: func(_ context.Context, procedure string, _ []interface{}, kwargs map[string]interface{}) error {
					calls++
					if procedure != "com.harman.extStateUpdate" || kwargs["state"] != "" {
						t.Error("muted state allowed active feedback or a direct hardware call")
					}
					return nil
				},
			}
			for _, phase := range []voicePhase{phaseListening, phaseThinking, phaseSpeaking, phaseIdle} {
				feedback.apply(context.Background(), phase)
			}
			if calls != 1 {
				t.Fatal("muted state must send exactly one scoped MCU cleanup")
			}
			if content != "muted" && content != "muted\n" && !strings.Contains(output.String(), "invalid") {
				t.Fatal("invalid microphone state was silently suppressed")
			}
		})
	}
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	writeMicrophoneState(t, target, "unmuted\n")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dir, "missing"), fifo, link, dir} {
		var output bytes.Buffer
		called := false
		feedback := &phaseFeedback{
			statePath: path, logger: log.New(&output, "", 0), touched: true,
			call: func(_ context.Context, _ string, _ []interface{}, kwargs map[string]interface{}) error {
				called = true
				if kwargs["state"] != "" {
					t.Error("unreadable authority allowed active voice feedback")
				}
				return nil
			},
		}
		feedback.apply(context.Background(), phaseListening)
		if !called || !strings.Contains(output.String(), "unreadable") {
			t.Fatal("unreadable authority must suppress active voice but still clean up")
		}
	}
}

func minLength(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestFeedbackRechecksMuteBeforeIdleAndLogsControlFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "microphone-state")
	writeMicrophoneState(t, path, "unmuted\n")
	var output bytes.Buffer
	var states []string
	feedback := &phaseFeedback{
		statePath: path, logger: log.New(&output, "", 0),
		call: func(_ context.Context, procedure string, _ []interface{}, kwargs map[string]interface{}) error {
			if procedure != "com.harman.extStateUpdate" {
				t.Fatal("feedback bypassed the MCU voice owner")
			}
			state := kwargs["state"].(string)
			states = append(states, state)
			if state != "" {
				return errors.New("test router failure")
			}
			return nil
		},
	}
	feedback.apply(context.Background(), phaseIdle)
	if len(states) != 0 {
		t.Fatal("idle cleared another animation before any voice turn")
	}
	feedback.apply(context.Background(), phaseListening)
	if feedback.last != "" || !strings.Contains(output.String(), "voice listening failed") {
		t.Fatal("failed control call was recorded as a successful light change")
	}
	writeMicrophoneState(t, path, "muted\n")
	feedback.apply(context.Background(), phaseIdle)
	if !reflect.DeepEqual(states, []string{"listening", ""}) || feedback.touched {
		t.Fatal("muted cleanup did not release the scoped voice owner")
	}
	writeMicrophoneState(t, path, "unmuted")
	feedback.apply(context.Background(), phaseIdle)
	if len(states) != 2 {
		t.Fatal("unmute repeated an already completed voice cleanup")
	}
	feedback.apply(context.Background(), phaseIdle)
	if len(states) != 2 {
		t.Fatal("successful idle cleanup was not deduplicated")
	}
}

func TestSlowFeedbackCannotBlockPhaseProducerAndCleanupIsJoined(t *testing.T) {
	path := filepath.Join(t.TempDir(), "microphone-state")
	writeMicrophoneState(t, path, "unmuted\n")
	var output bytes.Buffer
	started := make(chan struct{})
	cleaned := make(chan struct{}, 1)
	feedback := newFeedback(context.Background(), path, log.New(&output, "", 0),
		func(ctx context.Context, _ string, _ []interface{}, kwargs map[string]interface{}) error {
			if kwargs["state"] == "" {
				cleaned <- struct{}{}
				return nil
			}
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
	defer feedback.close()
	feedback.publish(phaseListening)
	<-started
	published := make(chan struct{})
	go func() {
		defer close(published)
		for i := 0; i < 1000; i++ {
			feedback.publish(phaseThinking)
			feedback.publish(phaseIdle)
		}
	}()
	select {
	case <-published:
	case <-time.After(time.Second):
		t.Fatal("slow router blocked the phase producer")
	}
	feedback.close()
	select {
	case <-cleaned:
	default:
		t.Fatal("feedback close did not join gated cleanup")
	}
	if !strings.Contains(output.String(), "coalescing") || strings.Contains(output.String(), "failed") {
		t.Fatal("queue overflow was not reported or normal cancellation became a failure")
	}
}
