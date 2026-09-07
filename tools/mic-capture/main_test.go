// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func writeCurrentPID(t *testing.T, path string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		path,
		[]byte(strconv.Itoa(os.Getpid())+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	return executable
}

func TestAuthorityStateRequiresMatchingEpochAndUnmutedState(t *testing.T) {
	directory := t.TempDir()
	epochPath := filepath.Join(directory, "epoch")
	statePath := filepath.Join(directory, "state")
	const epoch = "00112233445566778899aabbccddeeff"
	if err := os.WriteFile(epochPath, []byte(epoch+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("unmuted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hub := newClientHub(1)
	config := serviceConfig{
		authorityEpoch: epochPath,
		privacyState:   statePath,
	}
	var recordedEpoch string
	event := authorityEvent{
		kind:  "state",
		epoch: epoch,
	}
	if err := handleAuthorityEvent(
		context.Background(),
		config,
		hub,
		&recordedEpoch,
		event,
	); err != nil {
		t.Fatalf("state event: %v", err)
	}
	enabled, generation, _ := hub.state()
	if enabled || generation != 0 || recordedEpoch != epoch {
		t.Fatalf(
			"enabled=%v generation=%d epoch=%q",
			enabled,
			generation,
			recordedEpoch,
		)
	}
	event = authorityEvent{kind: "allow", epoch: epoch}
	if err := handleAuthorityEvent(
		context.Background(),
		config,
		hub,
		&recordedEpoch,
		event,
	); err != nil {
		t.Fatalf("allow event: %v", err)
	}
	enabled, generation, _ = hub.state()
	if !enabled || generation == 0 {
		t.Fatalf("enabled=%v generation=%d", enabled, generation)
	}
	firstGeneration := generation
	if err := handleAuthorityEvent(
		context.Background(),
		config,
		hub,
		&recordedEpoch,
		authorityEvent{kind: "block"},
	); err != nil {
		t.Fatalf("second block: %v", err)
	}
	if err := handleAuthorityEvent(
		context.Background(),
		config,
		hub,
		&recordedEpoch,
		authorityEvent{kind: "allow", epoch: epoch},
	); err != nil {
		t.Fatalf("second allow: %v", err)
	}
	enabled, generation, _ = hub.state()
	if !enabled || generation == 0 || generation == firstGeneration {
		t.Fatalf(
			"second authorization enabled=%v generation=%d first=%d",
			enabled,
			generation,
			firstGeneration,
		)
	}

	if err := os.WriteFile(statePath, []byte("muted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	event = authorityEvent{kind: "allow", epoch: epoch}
	if err := handleAuthorityEvent(
		context.Background(),
		config,
		hub,
		&recordedEpoch,
		event,
	); err == nil {
		t.Fatal("allow was accepted while muted")
	}
	enabled, _, _ = hub.state()
	if enabled {
		t.Fatal("hub remained enabled while muted")
	}
}

func TestAuthorityBlockWaitsForBackloggedWriter(t *testing.T) {
	hub := newClientHub(4)
	hub.enable(4)
	server, client := net.Pipe()
	defer client.Close()
	hub.add(server)
	for index := 0; index < 3; index++ {
		hub.broadcast(make([]byte, recordSize))
	}
	event := authorityEvent{kind: "block"}
	if err := handleAuthorityEvent(
		context.Background(),
		serviceConfig{},
		hub,
		new(string),
		event,
	); err != nil {
		t.Fatalf("block: %v", err)
	}
	enabled, _, clients := hub.state()
	if enabled || clients != 0 {
		t.Fatalf("enabled=%v clients=%d", enabled, clients)
	}
}

func TestProcessIdentityIncludesStartTime(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "pid")
	executable := writeCurrentPID(t, pidPath)
	identity, err := readProcessIdentity(pidPath, executable)
	if err != nil {
		t.Fatalf("read process identity: %v", err)
	}
	if identity.pid != os.Getpid() || identity.startTime == "" {
		t.Fatalf("identity = %+v", identity)
	}
}

func TestDSPGenerationIncludesSocketIdentity(t *testing.T) {
	directory := t.TempDir()
	pidPath := filepath.Join(directory, "pid")
	executable := writeCurrentPID(t, pidPath)
	socketPath := filepath.Join(directory, "dsp.sock")
	listener, err := net.ListenUnix(
		"unix",
		&net.UnixAddr{Name: socketPath, Net: "unix"},
	)
	if err != nil {
		t.Fatal(err)
	}
	generation, err := readDSPGeneration(pidPath, executable, socketPath)
	listener.Close()
	if err != nil {
		t.Fatalf("read DSP generation: %v", err)
	}
	if generation.pid != os.Getpid() ||
		generation.startTime == "" ||
		generation.socketIno == 0 {
		t.Fatalf("generation = %+v", generation)
	}
}

func TestLifecycleLockRejectsSecondOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "capture.lock")
	first, err := acquireLifecycleLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := acquireLifecycleLock(path); err == nil {
		t.Fatal("second owner acquired the lifecycle lock")
	}
}

func TestWriteAllRejectsAStalledConnection(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	start := time.Now()
	if err := writeAll(server, make([]byte, 4096), 20*time.Millisecond); err == nil {
		t.Fatal("stalled connection was accepted")
	}
	if time.Since(start) > time.Second {
		t.Fatal("stalled write exceeded its bound")
	}
}

func TestUnixPeerUID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer.sock")
	listener, err := net.ListenUnix(
		"unix",
		&net.UnixAddr{Name: path, Net: "unix"},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialUnix(
		"unix",
		nil,
		&net.UnixAddr{Name: path, Net: "unix"},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	uid, err := unixPeerUID(server)
	if err != nil {
		t.Fatal(err)
	}
	if uid != uint32(os.Getuid()) {
		t.Fatalf("uid = %d, want %d", uid, os.Getuid())
	}
}

func TestReadDSPGenerationRejectsRegularFile(t *testing.T) {
	directory := t.TempDir()
	pidPath := filepath.Join(directory, "pid")
	executable := writeCurrentPID(t, pidPath)
	socketPath := filepath.Join(directory, "not-a-socket")
	if err := os.WriteFile(socketPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readDSPGeneration(
		pidPath,
		executable,
		socketPath,
	); err == nil {
		t.Fatal("regular file accepted as DSP socket")
	}
}

func TestProcessIdentityRejectsWrongExecutable(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "pid")
	if err := os.WriteFile(
		pidPath,
		[]byte(strconv.Itoa(os.Getpid())+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := readProcessIdentity(pidPath, "/definitely/not/this/test"); err == nil {
		t.Fatal("wrong executable was accepted")
	}
}

func TestCaptureGenerationWaitsForAllow(t *testing.T) {
	directory := t.TempDir()
	fakeSource := filepath.Join(directory, "fake-loader")
	if err := os.WriteFile(
		fakeSource,
		[]byte(
			"#!/bin/sh\n"+
				"while :; do dd if=/dev/zero bs=2048 count=1 2>/dev/null; done\n",
		),
		0o700,
	); err != nil {
		t.Fatal(err)
	}
	dspPID := filepath.Join(directory, "dsp.pid")
	mcuPID := filepath.Join(directory, "mcu.pid")
	executable := writeCurrentPID(t, dspPID)
	_ = writeCurrentPID(t, mcuPID)
	dspSocket := filepath.Join(directory, "dsp.sock")
	dspListener, err := net.ListenUnix(
		"unix",
		&net.UnixAddr{Name: dspSocket, Net: "unix"},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer dspListener.Close()
	authoritySocket := filepath.Join(directory, "authority.sock")
	authorityListener, err := net.Listen(
		"unix",
		authoritySocket,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer authorityListener.Close()
	epochPath := filepath.Join(directory, "epoch")
	statePath := filepath.Join(directory, "state")
	const epoch = "00112233445566778899aabbccddeeff"
	if err := os.WriteFile(epochPath, []byte(epoch+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("unmuted\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	allow := make(chan struct{})
	stateSent := make(chan struct{})
	releaseServer := make(chan struct{})
	serverDone := make(chan error, 1)
	go func() {
		connection, err := authorityListener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer connection.Close()
		reader := bufio.NewReader(connection)
		if _, err := reader.ReadString('\n'); err != nil {
			serverDone <- err
			return
		}
		if _, err := connection.Write([]byte("BLOCK\n")); err != nil {
			serverDone <- err
			return
		}
		if reply, err := reader.ReadString('\n'); err != nil ||
			reply != "BLOCKED\n" {
			serverDone <- fmt.Errorf("block reply = %q err=%v", reply, err)
			return
		}
		if _, err := connection.Write([]byte("DRAIN\n")); err != nil {
			serverDone <- err
			return
		}
		if reply, err := reader.ReadString('\n'); err != nil ||
			reply != "DRAINED\n" {
			serverDone <- fmt.Errorf("drain reply = %q err=%v", reply, err)
			return
		}
		if _, err := connection.Write(
			[]byte("STATE " + epoch + " UNMUTED\n"),
		); err != nil {
			serverDone <- err
			return
		}

		close(stateSent)
		<-allow
		if _, err := connection.Write([]byte("ALLOW " + epoch + "\n")); err != nil {
			serverDone <- err
			return
		}
		if reply, err := reader.ReadString('\n'); err != nil ||
			reply != "ALLOWED\n" {
			serverDone <- fmt.Errorf("allow reply = %q err=%v", reply, err)
			return
		}
		<-releaseServer
		serverDone <- nil
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := newClientHub(1)
	runDone := make(chan error, 1)
	config := serviceConfig{
		authoritySocket: authoritySocket,
		authorityEpoch:  epochPath,
		privacyState:    statePath,
		mcuPID:          mcuPID,
		mcuExecutable:   executable,
		dspPID:          dspPID,
		dspExecutable:   executable,
		dspControl:      dspSocket,
		source: sourceConfig{
			loader:      fakeSource,
			libraryPath: "/unused",
			arecord:     "/unused",
			device:      "unused",
		},
	}
	go func() {
		runDone <- runCaptureGeneration(
			ctx,
			config,
			hub,
			make(chan error),
		)
	}()

	select {
	case <-stateSent:
	case <-time.After(2 * time.Second):
		t.Fatal("authority never sent STATE")
	}
	time.Sleep(20 * time.Millisecond)
	enabled, _, _ := hub.state()
	if enabled {
		t.Fatal("STATE authorized delivery before ALLOW")
	}
	close(allow)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		enabled, _, _ = hub.state()
		if enabled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	enabled, _, _ = hub.state()
	if !enabled {
		select {
		case err := <-runDone:
			t.Fatalf("ALLOW did not authorize delivery; generation=%v", err)
		case err := <-serverDone:
			t.Fatalf("ALLOW did not authorize delivery; authority=%v", err)
		default:
			t.Fatal("ALLOW did not authorize delivery")
		}
	}
	if err := os.WriteFile(statePath, []byte("invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-runDone:
		if err == nil || !strings.Contains(err.Error(), "privacy state became invalid") {
			t.Fatalf("generation stopped with %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("generation did not stop")
	}
	close(releaseServer)
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestPeriodIsZero(t *testing.T) {
	if !periodIsZero(make([]byte, nativePeriodBytes)) {
		t.Fatal("zero period was not recognized")
	}
	period := make([]byte, nativePeriodBytes)
	period[len(period)-1] = 1
	if periodIsZero(period) {
		t.Fatal("nonzero period was accepted")
	}
}

func TestPrivacyDrainRequiresConsecutiveZeroPeriods(t *testing.T) {
	zero := make([]byte, nativePeriodBytes)
	nonzero := make([]byte, nativePeriodBytes)
	nonzero[10] = 1
	remaining := privacyDrainPeriods
	for index := 0; index < privacyDrainPeriods-1; index++ {
		remaining = advancePrivacyDrain(remaining, zero)
	}
	if remaining != 1 {
		t.Fatalf("remaining = %d, want 1", remaining)
	}
	remaining = advancePrivacyDrain(remaining, nonzero)
	if remaining != privacyDrainPeriods {
		t.Fatalf(
			"nonzero period left %d, want reset to %d",
			remaining,
			privacyDrainPeriods,
		)
	}
	for remaining > 0 {
		remaining = advancePrivacyDrain(remaining, zero)
	}
	if remaining != 0 {
		t.Fatalf("drain never completed: %d", remaining)
	}
}
