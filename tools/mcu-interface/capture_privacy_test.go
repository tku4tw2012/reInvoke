// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type privacyEventLog struct {
	mu     sync.Mutex
	events []string
}

func (log *privacyEventLog) add(event string) {
	log.mu.Lock()
	log.events = append(log.events, event)
	log.mu.Unlock()
}

func (log *privacyEventLog) snapshot() []string {
	log.mu.Lock()
	defer log.mu.Unlock()
	return append([]string(nil), log.events...)
}

func indexOfEvent(events []string, expected string) int {
	for index, event := range events {
		if event == expected {
			return index
		}
	}
	return -1
}

type testCaptureGate struct {
	mu           sync.Mutex
	log          *privacyEventLog
	live         bool
	fence        func() error
	drain        func() error
	allow        func() error
	terminations int
}

func (gate *testCaptureGate) Fence(context.Context) error {
	if gate.log != nil {
		gate.log.add("fence")
	}
	if gate.fence != nil {
		return gate.fence()
	}
	return nil
}

func (gate *testCaptureGate) Drain(context.Context) error {
	if gate.log != nil {
		gate.log.add("drain")
	}
	if gate.drain != nil {
		return gate.drain()
	}
	return nil
}

func (gate *testCaptureGate) State(_ context.Context, muted bool) error {
	if gate.log != nil {
		state := "unmuted"
		if muted {
			state = "muted"
		}
		gate.log.add("state:" + state)
	}
	return nil
}

func (gate *testCaptureGate) Allow(context.Context) error {
	if gate.log != nil {
		gate.log.add("allow")
	}
	if gate.allow != nil {
		return gate.allow()
	}
	return nil
}

func (gate *testCaptureGate) Live() bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.live
}

func (gate *testCaptureGate) setLive(live bool) {
	gate.mu.Lock()
	gate.live = live
	gate.mu.Unlock()
}

func (gate *testCaptureGate) TerminateVerified() error {
	gate.mu.Lock()
	gate.terminations++
	gate.mu.Unlock()
	if gate.log != nil {
		gate.log.add("terminate")
	}
	return nil
}

func (gate *testCaptureGate) terminationCount() int {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return gate.terminations
}

func startRecordingMicControl(
	t *testing.T,
	count int,
	log *privacyEventLog,
	beforeReply func(string),
) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dsp-mic.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for index := 0; index < count; index++ {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			request := make([]byte, 2)
			if _, readErr := io.ReadFull(connection, request); readErr == nil {
				value := string(request)
				log.add("dsp:" + strings.TrimSpace(value))
				if beforeReply != nil {
					beforeReply(value)
				}
				_, _ = connection.Write([]byte("OK\n"))
			}
			_ = connection.Close()
		}
	}()
	return path
}

func newTestPrivacyController(
	t *testing.T,
	muted bool,
	controlPath string,
) *microphonePrivacyController {
	t.Helper()
	return newMicrophonePrivacyController(
		muted,
		filepath.Join(t.TempDir(), "microphone-state"),
		controlPath,
		nil,
		nil,
	)
}

func TestCaptureSyncStableUnmutedFencesReassertsAndAllows(t *testing.T) {
	log := &privacyEventLog{}
	controlPath := startRecordingMicControl(t, 2, log, nil)
	controller := newTestPrivacyController(t, false, controlPath)
	gate := &testCaptureGate{log: log, live: true}

	muted, err := controller.Synchronize(context.Background(), gate)
	if err != nil {
		t.Fatal(err)
	}
	if muted {
		t.Fatal("stable unmuted policy was not restored")
	}
	want := []string{
		"fence", "dsp:1", "drain", "dsp:0", "state:unmuted", "allow",
	}
	if got := log.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
	content, err := os.ReadFile(controller.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != microphoneUnmutedState {
		t.Fatalf("final state = %q, want unmuted", content)
	}
}

func TestCaptureSyncStableMutedNeverRestoresUnmute(t *testing.T) {
	log := &privacyEventLog{}
	controlPath := startRecordingMicControl(t, 1, log, nil)
	controller := newTestPrivacyController(t, true, controlPath)
	gate := &testCaptureGate{log: log, live: true}

	muted, err := controller.Synchronize(context.Background(), gate)
	if err != nil {
		t.Fatal(err)
	}
	if !muted {
		t.Fatal("stable muted policy was reversed")
	}
	want := []string{"fence", "dsp:1", "drain", "state:muted"}
	if got := log.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
}

func TestCaptureSyncPendingOrUnknownPolicyNeverUnmutes(t *testing.T) {
	tests := []struct {
		name    string
		desired bool
		unknown bool
	}{
		{name: "desired mute", desired: true},
		{name: "unknown hardware", unknown: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := &privacyEventLog{}
			controlPath := startRecordingMicControl(t, 1, log, nil)
			controller := newTestPrivacyController(t, false, controlPath)
			controller.desired = test.desired
			controller.unknown = test.unknown
			gate := &testCaptureGate{log: log, live: true}

			muted, err := controller.Synchronize(context.Background(), gate)
			if err != nil {
				t.Fatal(err)
			}
			if !muted {
				t.Fatal("pending privacy policy restored unmute")
			}
			if controller.desired != test.desired ||
				controller.unknown != test.unknown {
				t.Fatalf(
					"policy changed to desired=%t unknown=%t",
					controller.desired,
					controller.unknown,
				)
			}
			want := []string{"fence", "dsp:1", "drain", "state:muted"}
			if got := log.snapshot(); !reflect.DeepEqual(got, want) {
				t.Fatalf("events = %#v, want %#v", got, want)
			}
		})
	}
}

func TestOrdinaryMuteFencesBeforeStateAndDSP(t *testing.T) {
	log := &privacyEventLog{}
	stateDirectory := t.TempDir()
	statePath := filepath.Join(stateDirectory, "microphone-state")
	stateAtDSP := make(chan string, 1)
	controlPath := startRecordingMicControl(
		t,
		1,
		log,
		func(string) {
			content, _ := os.ReadFile(statePath)
			stateAtDSP <- string(content)
		},
	)
	controller := newMicrophonePrivacyController(
		false,
		statePath,
		controlPath,
		nil,
		nil,
	)
	controller.capture = &testCaptureGate{log: log, live: true}

	if err := controller.Set(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if got := log.snapshot(); !reflect.DeepEqual(
		got,
		[]string{"fence", "dsp:1"},
	) {
		t.Fatalf("events = %#v", got)
	}
	if state := <-stateAtDSP; state != microphoneMutedState {
		t.Fatalf("state at DSP mute = %q, want muted", state)
	}
}

func TestOrdinaryUnmuteAllowsOnlyAfterConfirmationAndPersistence(t *testing.T) {
	log := &privacyEventLog{}
	statePath := filepath.Join(t.TempDir(), "microphone-state")
	if err := persistMicrophoneState(statePath, true); err != nil {
		t.Fatal(err)
	}
	releaseDSP := make(chan struct{})
	controlPath := startRecordingMicControl(
		t,
		1,
		log,
		func(request string) {
			if request == "0\n" {
				<-releaseDSP
			}
		},
	)
	controller := newMicrophonePrivacyController(
		true,
		statePath,
		controlPath,
		nil,
		nil,
	)
	gate := &testCaptureGate{log: log, live: true}
	gate.allow = func() error {
		content, err := os.ReadFile(statePath)
		if err != nil {
			return err
		}
		if string(content) != microphoneUnmutedState {
			return errors.New("capture allowed before unmuted state persisted")
		}
		return nil
	}
	controller.capture = gate
	done := make(chan error, 1)
	go func() { done <- controller.Set(context.Background(), false) }()

	deadline := time.After(time.Second)
	for {
		events := log.snapshot()
		if len(events) > 0 {
			if !reflect.DeepEqual(events, []string{"dsp:0"}) {
				t.Fatalf("events before confirmation = %#v", events)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("DSP unmute was not requested")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	content, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != microphoneMutedState {
		t.Fatalf("state before confirmation = %q", content)
	}
	close(releaseDSP)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := log.snapshot(); !reflect.DeepEqual(
		got,
		[]string{"dsp:0", "allow"},
	) {
		t.Fatalf("events = %#v", got)
	}
}

func TestFenceFailureTerminatesAndStillMutes(t *testing.T) {
	log := &privacyEventLog{}
	controlPath := startRecordingMicControl(t, 1, log, nil)
	controller := newTestPrivacyController(t, false, controlPath)
	gate := &testCaptureGate{
		log:  log,
		live: true,
		fence: func() error {
			return errors.New("capture owner wedged")
		},
	}
	controller.capture = gate

	if err := controller.Set(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if gate.terminationCount() != 1 {
		t.Fatalf("termination count = %d, want 1", gate.terminationCount())
	}
	want := []string{"fence", "terminate", "dsp:1"}
	if got := log.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
	if !controller.muted {
		t.Fatal("capture failure prevented physical privacy mute")
	}
}

func TestDrainFailureLeavesMutedAndTerminatesOwner(t *testing.T) {
	log := &privacyEventLog{}
	controlPath := startRecordingMicControl(t, 1, log, nil)
	controller := newTestPrivacyController(t, false, controlPath)
	gate := &testCaptureGate{
		log:  log,
		live: true,
		drain: func() error {
			return errors.New("capture backlog did not drain")
		},
	}

	muted, err := controller.Synchronize(context.Background(), gate)
	if err == nil {
		t.Fatal("drain failure was accepted")
	}
	if !muted || !controller.muted {
		t.Fatal("drain failure restored unmute")
	}
	if gate.terminationCount() != 1 {
		t.Fatalf("termination count = %d, want 1", gate.terminationCount())
	}
	want := []string{"fence", "dsp:1", "drain", "terminate"}
	if got := log.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
}

func TestTerminateProcessGenerationRefusesStaleOrMismatchedOwner(t *testing.T) {
	expected := processGeneration{pid: 41, startTime: 900}
	tests := []struct {
		name    string
		current processGeneration
		err     error
	}{
		{
			name:    "stale PID generation",
			current: processGeneration{pid: 41, startTime: 901},
		},
		{
			name: "mismatched executable",
			err:  errors.New("capture-owner PID belongs to another executable"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			signaled := false
			err := terminateProcessGeneration(
				expected,
				func() (processGeneration, error) {
					return test.current, test.err
				},
				func(int, syscall.Signal) error {
					signaled = true
					return nil
				},
			)
			if err == nil {
				t.Fatal("unverified generation was accepted")
			}
			if signaled {
				t.Fatal("unverified capture owner was killed")
			}
		})
	}
}

func TestCaptureAuthorityDisconnectRevokesAllow(t *testing.T) {
	log := &privacyEventLog{}
	var gate *testCaptureGate
	controlPath := startRecordingMicControl(
		t,
		2,
		log,
		func(request string) {
			if request == "0\n" {
				gate.setLive(false)
			}
		},
	)
	controller := newTestPrivacyController(t, false, controlPath)
	gate = &testCaptureGate{log: log, live: true}

	muted, err := controller.Synchronize(context.Background(), gate)
	if err == nil {
		t.Fatal("authority loss before allow was accepted")
	}
	if muted {
		t.Fatal("confirmed unmute state was reported incorrectly")
	}
	if got := log.snapshot(); !reflect.DeepEqual(
		got,
		[]string{"fence", "dsp:1", "drain", "dsp:0"},
	) {
		t.Fatalf("events = %#v", got)
	}
	if controller.capture != nil {
		t.Fatal("lost authority remained attached")
	}
}

func TestCapturePrivacySessionRejectsUnexpectedResponse(t *testing.T) {
	server, client := net.Pipe()
	session := newCapturePrivacySession(
		server,
		"generation-1",
		strings.Repeat("a", 64),
		100*time.Millisecond,
		nil,
	)
	defer session.Close()
	go func() {
		reader := bufio.NewReader(client)
		_, _ = reader.ReadString('\n')
		_, _ = io.WriteString(client, "BLOCKED wrong-generation\n")
		_ = client.Close()
	}()
	if err := session.Fence(context.Background()); err == nil {
		t.Fatal("unexpected fence response was accepted")
	}
	if session.Live() {
		t.Fatal("protocol violation did not revoke authority")
	}
}

func TestCapturePrivacyHelloProtocol(t *testing.T) {
	log := &privacyEventLog{}
	controlPath := startRecordingMicControl(t, 2, log, nil)
	controller := newTestPrivacyController(t, false, controlPath)
	server, client := net.Pipe()
	epoch := strings.Repeat("d", privacyEpochBytes*2)
	session := newCapturePrivacySession(
		server,
		"0000000000001122",
		epoch,
		time.Second,
		nil,
	)
	defer session.Close()
	peerResult := make(chan error, 1)
	releasePeer := make(chan struct{})
	go func() {
		defer client.Close()
		reader := bufio.NewReader(client)
		for _, exchange := range []struct {
			command string
			reply   string
		}{
			{"BLOCK\n", "BLOCKED\n"},
			{"DRAIN\n", "DRAINED\n"},
			{"STATE " + epoch + " UNMUTED\n", ""},
			{"ALLOW " + epoch + "\n", "ALLOWED\n"},
		} {
			command, err := reader.ReadString('\n')
			if err != nil {
				peerResult <- err
				return
			}
			if command != exchange.command {
				peerResult <- errors.New("unexpected authority command")
				return
			}
			if exchange.reply != "" {
				if _, err := io.WriteString(client, exchange.reply); err != nil {
					peerResult <- err
					return
				}
			}
		}
		peerResult <- nil
		<-releasePeer
	}()
	muted, err := controller.Synchronize(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	if muted {
		t.Fatal("authority synchronization returned muted")
	}
	if err := <-peerResult; err != nil {
		t.Fatal(err)
	}
	close(releasePeer)
}

func TestCapturePrivacyDisconnectAndSingleOwner(t *testing.T) {
	firstServer, firstClient := net.Pipe()
	first := newCapturePrivacySession(
		firstServer,
		"0000000000000001",
		strings.Repeat("c", privacyEpochBytes*2),
		time.Second,
		nil,
	)
	secondServer, secondClient := net.Pipe()
	second := newCapturePrivacySession(
		secondServer,
		"0000000000000002",
		strings.Repeat("c", privacyEpochBytes*2),
		time.Second,
		nil,
	)
	server := &capturePrivacyServer{}
	if !server.reserve(first) {
		t.Fatal("first capture owner was rejected")
	}
	if server.reserve(second) {
		t.Fatal("second live capture owner was accepted")
	}
	_ = firstClient.Close()
	select {
	case <-first.done:
	case <-time.After(time.Second):
		t.Fatal("authority disconnect was not detected")
	}
	server.release(first)
	if !server.reserve(second) {
		t.Fatal("replacement owner was rejected after disconnect")
	}
	second.Close()
	_ = secondClient.Close()
}

func TestCapturePrivacyProtocolBoundsAndPeerChecks(t *testing.T) {
	reader := bufio.NewReaderSize(
		strings.NewReader(strings.Repeat("x", maxCapturePrivacyLine+1)+"\n"),
		maxCapturePrivacyLine+1,
	)
	if _, err := readBoundedPrivacyLine(reader); err == nil {
		t.Fatal("oversized privacy record was accepted")
	}

	called := false
	verify := func(pid int) (processGeneration, error) {
		called = true
		return processGeneration{pid: pid, startTime: 1}, nil
	}
	if _, err := authenticateCapturePeer(
		&syscall.Ucred{Uid: 1000, Pid: 12},
		verify,
	); err == nil {
		t.Fatal("non-root capture peer was accepted")
	}
	if called {
		t.Fatal("non-root peer reached process verification")
	}
	generation, err := authenticateCapturePeer(
		&syscall.Ucred{Uid: 0, Pid: 12},
		verify,
	)
	if err != nil {
		t.Fatal(err)
	}
	if generation.pid != 12 || !called {
		t.Fatalf("verified generation = %#v", generation)
	}
	if _, err := authenticateCapturePeer(
		&syscall.Ucred{Uid: 0, Pid: 12},
		func(int) (processGeneration, error) {
			return processGeneration{}, errors.New("executable mismatch")
		},
	); err == nil {
		t.Fatal("root peer with mismatched executable was accepted")
	}

	for _, invalid := range []string{
		"HELLO",
		"HELLO 0000000000001122 extra",
		"HELLO  0000000000001122",
		"HELLO\t0000000000001122",
		"HELLO 000000000000112G",
		"HELLO 000000000000112",
		"HELLO 000000000000112A",
		"SYNC 0000000000001122",
	} {
		if _, err := parseCaptureHello(invalid); err == nil {
			t.Fatalf("invalid request %q was accepted", invalid)
		}
	}
	if value, err := parseCaptureHello(
		"HELLO 0000000000001122",
	); err != nil || value != "0000000000001122" {
		t.Fatalf("valid request parsed as %q, %v", value, err)
	}
}

func TestCapturePrivacySocketModeRecoveryAndEpochCleanup(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(directory, "privacy.sock")
	listener, identity, lock, err := listenPrivateUnix(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %o, want 600", info.Mode().Perm())
	}
	if _, _, _, err := listenPrivateUnix(socketPath); err == nil {
		t.Fatal("second live capture privacy owner was accepted")
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	_ = lock.Close()

	recovered, recoveredIdentity, recoveredLock, err := listenPrivateUnix(
		socketPath,
	)
	if err != nil {
		t.Fatalf("recover stale socket: %v", err)
	}
	_ = recovered.Close()
	removeSocketIfSame(socketPath, recoveredIdentity)
	_ = syscall.Flock(int(recoveredLock.Fd()), syscall.LOCK_UN)
	_ = recoveredLock.Close()
	removeSocketIfSame(socketPath, identity)

	epochPath := filepath.Join(directory, "authority-epoch")
	epoch, err := createPrivacyAuthorityEpoch(epochPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(epoch) != privacyEpochBytes*2 {
		t.Fatalf("epoch length = %d", len(epoch))
	}
	epochInfo, err := os.Stat(epochPath)
	if err != nil {
		t.Fatal(err)
	}
	if epochInfo.Mode().Perm() != 0o600 {
		t.Fatalf("epoch mode = %o, want 600", epochInfo.Mode().Perm())
	}
	replacement, err := createPrivacyAuthorityEpoch(epochPath)
	if err != nil {
		t.Fatal(err)
	}
	if replacement == epoch {
		t.Fatal("privacy authority epoch was reused")
	}

	serverSocket := filepath.Join(directory, "server.sock")
	serverEpoch := filepath.Join(directory, "server-epoch")
	controller := newMicrophonePrivacyController(
		false,
		filepath.Join(directory, "microphone-state"),
		filepath.Join(directory, "dsp.sock"),
		nil,
		nil,
	)
	server, err := openCapturePrivacyServer(
		serverSocket,
		serverEpoch,
		filepath.Join(directory, "capture.pid"),
		"/unused",
		controller,
	)
	if err != nil {
		t.Fatal(err)
	}
	firstEpoch, err := os.ReadFile(serverEpoch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openCapturePrivacyServer(
		serverSocket,
		serverEpoch,
		filepath.Join(directory, "capture.pid"),
		"/unused",
		controller,
	); err == nil {
		t.Fatal("duplicate privacy server was opened")
	}
	currentEpoch, err := os.ReadFile(serverEpoch)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(currentEpoch, firstEpoch) {
		t.Fatal("failed duplicate startup replaced the live authority epoch")
	}
	server.Close()
	if _, err := os.Stat(serverSocket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("privacy socket survived clean close: %v", err)
	}
	if _, err := os.Stat(serverEpoch); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("authority epoch survived clean close: %v", err)
	}
}

func TestButtonRaceWithCaptureSyncCannotReverseMute(t *testing.T) {
	log := &privacyEventLog{}
	muteStarted := make(chan struct{})
	releaseMute := make(chan struct{})
	controlPath := startRecordingMicControl(
		t,
		1,
		log,
		func(request string) {
			if request == "1\n" {
				close(muteStarted)
				<-releaseMute
			}
		},
	)
	controller := newTestPrivacyController(t, false, controlPath)
	gate := &testCaptureGate{log: log, live: true}
	syncDone := make(chan error, 1)
	go func() {
		_, err := controller.Synchronize(context.Background(), gate)
		syncDone <- err
	}()
	<-muteStarted
	buttonDone := make(chan error, 1)
	go func() {
		buttonDone <- controller.Apply(
			context.Background(),
			inputEvent{Name: "micmute"},
		)
	}()
	deadline := time.After(time.Second)
	for {
		requestedMuted, version := controller.requestedPolicy()
		if requestedMuted && version > 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("physical mute did not update requested policy")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	close(releaseMute)
	if err := <-syncDone; err != nil {
		t.Fatal(err)
	}
	if err := <-buttonDone; err != nil {
		t.Fatal(err)
	}
	if !controller.muted || !controller.desired || controller.unknown {
		t.Fatalf(
			"final policy muted=%t desired=%t unknown=%t",
			controller.muted,
			controller.desired,
			controller.unknown,
		)
	}
	want := []string{
		"fence", "dsp:1", "drain", "state:muted",
	}
	if got := log.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
}

func TestCancelledUnmuteDoesNotCorruptNextPhysicalToggle(t *testing.T) {
	log := &privacyEventLog{}
	controlPath := startRecordingMicControl(t, 1, log, nil)
	controller := newTestPrivacyController(t, false, controlPath)
	controller.capture = &testCaptureGate{live: true, log: log}

	ctx, cancel := context.WithCancel(context.Background())
	controller.mu.Lock()
	done := make(chan error, 1)
	go func() {
		done <- controller.Set(ctx, false)
	}()
	deadline := time.After(time.Second)
	for {
		requestedMuted, version := controller.requestedPolicy()
		if !requestedMuted && version > 1 {
			break
		}

		select {
		case <-deadline:
			controller.mu.Unlock()
			t.Fatal("unmute request did not record its logical policy")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	controller.mu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled unmute error = %v", err)
	}
	requestedMuted, _ := controller.requestedPolicy()
	if requestedMuted {
		t.Fatal("cancelled unmute changed logical policy to muted")
	}

	if err := controller.Apply(
		context.Background(),
		inputEvent{Name: "micmute"},
	); err != nil {
		t.Fatalf("physical mute after cancellation: %v", err)
	}
	if !controller.muted {
		t.Fatal("next physical Mic-Mute press did not request mute")
	}
	events := log.snapshot()
	fence := indexOfEvent(events, "fence")
	dspMute := indexOfEvent(events, "dsp:1")
	if fence < 0 || dspMute < 0 || fence > dspMute {
		t.Fatalf("events = %v, want fence before DSP mute", events)
	}
	if indexOfEvent(events, "dsp:0") >= 0 {
		t.Fatalf("cancelled unmute caused a later unmute: %v", events)
	}
}

func TestMuteCannotLandBetweenFinalPolicyCheckAndAllow(t *testing.T) {
	log := &privacyEventLog{}
	controlPath := startRecordingMicControl(t, 3, log, nil)
	controller := newTestPrivacyController(t, false, controlPath)
	allowStarted := make(chan struct{})
	releaseAllow := make(chan struct{})
	gate := &testCaptureGate{
		live: true,
		log:  log,
		allow: func() error {
			close(allowStarted)
			<-releaseAllow
			return nil
		},
	}
	syncDone := make(chan error, 1)
	go func() {
		_, err := controller.Synchronize(context.Background(), gate)
		syncDone <- err
	}()
	<-allowStarted

	buttonDone := make(chan error, 1)
	go func() {
		buttonDone <- controller.Apply(
			context.Background(),
			inputEvent{Name: "micmute"},
		)
	}()
	// Synchronize holds policyMu through ALLOW. The button must remain blocked
	// at that linearization point rather than racing authorization.
	select {
	case err := <-buttonDone:
		t.Fatalf("physical mute crossed the ALLOW boundary early: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseAllow)
	if err := <-syncDone; err != nil {
		t.Fatal(err)
	}
	if err := <-buttonDone; err != nil {
		t.Fatal(err)
	}
	if !controller.muted {
		t.Fatal("queued physical mute did not win after ALLOW")
	}
	events := log.snapshot()
	allow := indexOfEvent(events, "allow")
	fence := -1
	for index := allow + 1; index < len(events); index++ {
		if events[index] == "fence" {
			fence = index
			break
		}
	}
	if allow < 0 || fence < 0 || allow > fence {
		t.Fatalf("events = %v, want ALLOW linearized before next fence", events)
	}
}

func TestNewerValidUnmuteSupersedesOlderMute(t *testing.T) {
	log := &privacyEventLog{}
	controlPath := startRecordingMicControl(t, 1, log, nil)
	controller := newTestPrivacyController(t, false, controlPath)
	oldVersion := controller.recordRequestedPolicy(true)
	newVersion := controller.recordRequestedPolicy(false)

	if err := controller.applyRequestedPolicy(
		context.Background(),
		true,
		oldVersion,
	); !errors.Is(err, errMicrophoneRequestSuperseded) {
		t.Fatalf("old request error = %v", err)
	}
	if events := log.snapshot(); len(events) != 0 {
		t.Fatalf("superseded request touched hardware: %v", events)
	}
	if err := controller.applyRequestedPolicy(
		context.Background(),
		false,
		newVersion,
	); err != nil {
		t.Fatalf("new request: %v", err)
	}
	if events := log.snapshot(); indexOfEvent(events, "dsp:1") >= 0 {
		t.Fatalf("older mute won over newer unmute: %v", events)
	}
}

func TestCancelledUnmuteRestoresSupersededMuteIntent(t *testing.T) {
	log := &privacyEventLog{}
	controlPath := startRecordingMicControl(t, 1, log, nil)
	controller := newTestPrivacyController(t, false, controlPath)
	controller.capture = &testCaptureGate{live: true, log: log}

	muteVersion := controller.recordRequestedPolicy(true)
	unmuteVersion := controller.recordRequestedPolicy(false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := controller.applyRequestedPolicy(
		ctx,
		false,
		unmuteVersion,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled unmute error = %v", err)
	}
	requestedMuted, rollbackVersion := controller.requestedPolicy()
	if !requestedMuted || rollbackVersion <= unmuteVersion {
		t.Fatalf(
			"rollback policy muted=%v version=%d, prior mute version=%d",
			requestedMuted,
			rollbackVersion,
			muteVersion,
		)
	}
	if events := log.snapshot(); indexOfEvent(events, "fence") < 0 {
		t.Fatalf("cancelled unmute did not fence capture: %v", events)
	}

	if err := controller.applyRequestedPolicy(
		context.Background(),
		true,
		muteVersion,
	); err != nil {
		t.Fatalf("restored pending mute error = %v", err)
	}
	if !controller.muted {
		t.Fatal("superseded mute intent was lost after cancelled unmute")
	}
	if events := log.snapshot(); indexOfEvent(events, "dsp:0") >= 0 {
		t.Fatalf("cancelled unmute reached hardware: %v", events)
	}
}

func TestCancelledUnmutesKeepMute(t *testing.T) {
	tests := []struct {
		name  string
		order []int
	}{
		{name: "old-first", order: []int{0, 1}},
		{name: "new-first", order: []int{1, 0}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := &privacyEventLog{}
			controlPath := startRecordingMicControl(t, 1, log, nil)
			controller := newTestPrivacyController(t, false, controlPath)
			controller.capture = &testCaptureGate{live: true, log: log}

			muteVersion := controller.recordRequestedPolicy(true)
			unmuteVersions := []uint64{
				controller.recordRequestedPolicy(false),
				controller.recordRequestedPolicy(false),
			}
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			for _, index := range test.order {
				err := controller.applyRequestedPolicy(
					cancelled,
					false,
					unmuteVersions[index],
				)
				if !errors.Is(err, context.Canceled) {
					t.Fatalf(
						"cancel unmute %d: %v",
						unmuteVersions[index],
						err,
					)
				}
			}

			requestedMuted, _ := controller.requestedPolicy()
			if !requestedMuted {
				t.Fatal("cancelled unmutes erased the pending mute")
			}
			select {
			case <-controller.reconcile:
			default:
				t.Fatal("restored mute did not schedule reconciliation")
			}
			if err := controller.applyRequestedPolicy(
				context.Background(),
				true,
				muteVersion,
			); err != nil {
				t.Fatalf("apply surviving mute: %v", err)
			}
			if !controller.muted {
				t.Fatal("surviving mute was not applied")
			}
			events := log.snapshot()
			if indexOfEvent(events, "dsp:0") >= 0 {
				t.Fatalf("cancelled unmute reached hardware: %v", events)
			}
			if indexOfEvent(events, "fence") < 0 ||
				indexOfEvent(events, "dsp:1") < 0 {
				t.Fatalf("mute was not fenced and applied: %v", events)
			}
		})
	}
}

func TestCancelledMuteRemainsFailClosed(t *testing.T) {
	log := &privacyEventLog{}
	controlPath := startRecordingMicControl(t, 1, log, nil)
	controller := newTestPrivacyController(t, false, controlPath)
	controller.capture = &testCaptureGate{live: true, log: log}
	version := controller.recordRequestedPolicy(true)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	if err := controller.applyRequestedPolicy(
		cancelled,
		true,
		version,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled mute error = %v", err)
	}
	requestedMuted, _ := controller.requestedPolicy()
	if !requestedMuted {
		t.Fatal("cancelled mute request was discarded")
	}
	if events := log.snapshot(); indexOfEvent(events, "fence") < 0 {
		t.Fatalf("cancelled mute did not fence capture: %v", events)
	}
	select {
	case <-controller.reconcile:
	default:
		t.Fatal("cancelled mute did not schedule reconciliation")
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile cancelled mute: %v", err)
	}
	if !controller.muted {
		t.Fatal("cancelled mute was not eventually applied")
	}
}

func TestAppliedOlderRequestCannotMutateHardware(t *testing.T) {
	log := &privacyEventLog{}
	controlPath := startRecordingMicControl(t, 1, log, nil)
	controller := newTestPrivacyController(t, false, controlPath)
	older := controller.recordRequestedPolicy(true)
	newer := controller.recordRequestedPolicy(true)

	if err := controller.applyRequestedPolicy(
		context.Background(),
		true,
		newer,
	); err != nil {
		t.Fatalf("apply newer mute: %v", err)
	}
	eventCount := len(log.snapshot())
	if err := controller.applyRequestedPolicy(
		context.Background(),
		true,
		older,
	); err != nil {
		t.Fatalf("already-applied older mute: %v", err)
	}
	if events := log.snapshot(); len(events) != eventCount {
		t.Fatalf("older applied request touched hardware: %v", events)
	}
}

func TestSupersededUnmuteCannotBecomePolicyAfterNewerCancellation(t *testing.T) {
	controller := newTestPrivacyController(t, true, "/unused")
	olderUnmute := controller.recordRequestedPolicy(false)
	newerUnmute := controller.recordRequestedPolicy(false)

	if err := controller.applyRequestedPolicy(
		context.Background(),
		false,
		olderUnmute,
	); !errors.Is(err, errMicrophoneRequestSuperseded) {
		t.Fatalf("older unmute error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := controller.applyRequestedPolicy(
		cancelled,
		false,
		newerUnmute,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("newer cancelled unmute error = %v", err)
	}
	requestedMuted, _ := controller.requestedPolicy()
	if !requestedMuted {
		t.Fatal("orphaned unmute replaced the applied mute policy")
	}
}
