// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"
)

func checkTestVoiceCall(message []interface{}, state string) error {
	expected := []interface{}{uint64(48), uint64(1), map[string]interface{}{},
		"com.harman.extStateUpdate", []interface{}{"voice"}, map[string]interface{}{"state": state}}
	if !reflect.DeepEqual(message, expected) {
		return errors.New("voice phase did not use the original MCU extStateUpdate contract")
	}
	return nil
}

func acceptTestActionSubscription(connection net.Conn) error {
	if err := acceptTestWAMPHelloRole(connection, "default", "subscriber"); err != nil {
		return err
	}
	if err := welcomeTestWAMP(connection); err != nil {
		return err
	}
	message, err := readTestWAMP(connection)
	if err != nil {
		return err
	}
	want := []interface{}{uint64(32), uint64(1), map[string]interface{}{}, voiceActionTopic}
	if !reflect.DeepEqual(message, want) {
		return errors.New("endpoint must subscribe only to resolved com.harman.vui.action")
	}
	return writeWAMP(connection, maxControlFrame, []interface{}{33, uint64(1), uint64(901)})
}

func startIdleActionRouter(t *testing.T) string {
	t.Helper()
	address, _ := startTestRouter(t, 1, func(_ int, connection net.Conn) error {
		if err := acceptTestActionSubscription(connection); err != nil {
			return err
		}
		_, err := io.Copy(io.Discard, connection)
		return err
	})
	return address
}

func actionMessage(subscription uint64, action, name string) []interface{} {
	return []interface{}{uint64(36), subscription, uint64(44), map[string]interface{}{},
		[]interface{}{action, name}}
}

func TestWAMPResolvedActionsDispatchExactlyOnce(t *testing.T) {
	address, server := startTestRouter(t, 1, func(_ int, connection net.Conn) error {
		if err := acceptTestActionSubscription(connection); err != nil {
			return err
		}
		for _, message := range [][]interface{}{
			actionMessage(902, "voice-trigger", "action-long"),
			actionMessage(901, "action-long", ""),
			actionMessage(901, "music-pause", "action"),
			actionMessage(901, "voice-trigger", "action"),
			actionMessage(901, "volumeup", "volumeup"),
			actionMessage(901, "voice-trigger", "action-long"),
			actionMessage(901, "voice-cancel", "action"),
			actionMessage(901, "micmute", "micmute"),
		} {
			if err := writeWAMP(connection, maxControlFrame, message); err != nil {
				return err
			}
		}
		_, err := io.Copy(io.Discard, connection)
		return err
	})
	controls, err := connectWorkerControls(context.Background(), address, "default")
	if err != nil {
		t.Fatal(err)
	}
	defer controls.Close()
	for _, want := range []syscall.Signal{syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGUSR2} {
		select {
		case got := <-controls.actions:
			if got != want {
				t.Fatalf("button = %v, want %v", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("resolved button was not delivered")
		}
	}
	select {
	case extra := <-controls.actions:
		t.Fatalf("raw or unrelated input dispatched a second action: %v", extra)
	case <-time.After(20 * time.Millisecond):
	}
	controls.Close()
	if err := <-server; err != nil {
		t.Fatal(err)
	}
}

func TestVoiceActionRejectsMalformedAndUnrelatedEvents(t *testing.T) {
	for _, message := range [][]interface{}{
		nil, {}, {uint64(36)}, {uint64(36), uint64(901), uint64(44), "bad-details", []interface{}{"voice-trigger", "action-long"}},
		{uint64(36), uint64(901), uint64(0), map[string]interface{}{}, []interface{}{"voice-trigger", "action-long"}},
		{uint64(36), uint64(901), uint64(44), map[string]interface{}{}, []interface{}{"voice-trigger"}},
		{uint64(36), uint64(901), uint64(44), map[string]interface{}{}, []interface{}{"voice-trigger", "action-long"}, false},
		actionMessage(902, "voice-trigger", "action-long"),
		actionMessage(901, "bluetooth-pair", "bluetooth"),
	} {
		if got := voiceAction(message, 901); got != 0 {
			t.Fatalf("invalid event dispatched %v: %v", got, message)
		}
	}
}

func TestWAMPActionSubscriberCancellationAndDisconnect(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "disconnect"}[disconnect], func(t *testing.T) {
			address, _ := startTestRouter(t, 1, func(_ int, connection net.Conn) error {
				if err := acceptTestActionSubscription(connection); err != nil {
					return err
				}
				if !disconnect {
					_, _ = io.Copy(io.Discard, connection)
				}
				return nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			controls, err := connectWorkerControls(ctx, address, "default")
			if err != nil {
				t.Fatal(err)
			}
			defer controls.Close()
			if !disconnect {
				cancel()
			}
			select {
			case err := <-controls.errors:
				if err == nil {
					t.Fatal("lost action session reported success")
				}
			case <-time.After(time.Second):
				t.Fatal("blocked action subscriber did not shut down")
			}
		})
	}
}

func TestFeedbackRenewsLeaseWithoutReplayingQueuedPhases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "microphone-state")
	writeMicrophoneState(t, path, "unmuted\n")
	calls := make(chan string, 8)
	feedback := newFeedback(context.Background(), path, log.New(io.Discard, "", 0),
		func(_ context.Context, _ string, _ []interface{}, kwargs map[string]interface{}) error {
			calls <- kwargs["state"].(string)
			return nil
		})
	defer feedback.close()
	feedback.publish(phaseThinking)
	for i := 0; i < 2; i++ {
		select {
		case state := <-calls:
			if state != "thinking" {
				t.Fatalf("lease renewed an unrelated phase: %q", state)
			}
		case <-time.After(phaseRefresh + time.Second):
			t.Fatal("active phase lease was not renewed")
		}
	}
	feedback.close()
	if state := <-calls; state != "" {
		t.Fatalf("session cleanup = %q", state)
	}
}

func TestWorkerControlsRefuseOtherProcessesAndUnsupportedSignals(t *testing.T) {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}

	worker := ownedWorker{process: process}
	if err := worker.signal(context.Background(), make(chan struct{}), syscall.SIGUSR1); err == nil {
		t.Fatal("current process was accepted as an owned child")
	}
	if err := worker.signal(context.Background(), make(chan struct{}), syscall.SIGTERM); err == nil {
		t.Fatal("private control path accepted a lifecycle signal")
	}
	done := make(chan struct{})
	close(done)
	if err := worker.signal(context.Background(), done, syscall.SIGUSR2); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("retired child accepted a control signal: %v", err)
	}
}

func TestWorkerSignalsAndControlLossCleanup(t *testing.T) {
	controls := &workerControls{actions: make(chan syscall.Signal, 8), errors: make(chan error, 1)}
	phases := make(chan voicePhase, 8)
	cmd := helperCommand(t, "controls")
	harness := startBridge(t, cmd, false, func(phase voicePhase) { phases <- phase }, controls)
	read := func(want string) {
		t.Helper()
		data := make([]byte, len(want))
		if _, err := io.ReadFull(harness.host, data); err != nil || string(data) != want {
			t.Fatalf("control altered worker I/O: %q, want %q: %v", data, want, err)
		}
	}
	read("READY\n")
	controls.actions <- syscall.SIGUSR1
	read("TRIGGER\n")
	controls.actions <- syscall.SIGUSR2
	read("CANCEL\n")
	for _, want := range []voicePhase{phaseListening, phaseIdle} {
		select {
		case phase := <-phases:
			if phase != want {
				t.Fatalf("worker phase = %q, want %q", phase, want)
			}
		case <-time.After(time.Second):
			t.Fatal("signal-driven worker phase was not drained")
		}
	}
	controls.errors <- errors.New("test action session lost")
	if err := harness.wait(t); err == nil {
		t.Fatal("control disconnect did not terminate the isolated session")
	}
	assertReaped(t, cmd.Process.Pid)
	done := make(chan struct{})
	close(done)
	if err := (ownedWorker{process: cmd.Process}).signal(context.Background(), done, syscall.SIGUSR1); !errors.Is(err, os.ErrProcessDone) {
		t.Fatal("retired worker accepted a late trigger")
	}

	nextControls := &workerControls{actions: make(chan syscall.Signal, 8), errors: make(chan error, 1)}
	next := startBridge(t, helperCommand(t, "controls"), false, nil, nextControls)
	data := make([]byte, len("READY\n"))
	if _, err := io.ReadFull(next.host, data); err != nil || string(data) != "READY\n" {
		t.Fatal("replacement worker did not start")
	}
	controls.actions <- syscall.SIGUSR1
	nextControls.actions <- syscall.SIGUSR2
	data = make([]byte, len("CANCEL\n"))
	if _, err := io.ReadFull(next.host, data); err != nil || string(data) != "CANCEL\n" {
		t.Fatal("stale session control reached the replacement child")
	}
	next.cancel()
	if err := next.wait(t); !errors.Is(err, context.Canceled) {
		t.Fatal("replacement worker cleanup did not preserve cancellation")
	}
	assertReaped(t, next.cmd.Process.Pid)
}
