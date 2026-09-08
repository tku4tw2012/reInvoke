// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
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
