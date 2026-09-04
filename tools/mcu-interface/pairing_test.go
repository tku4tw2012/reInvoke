// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

func TestPairingControllerIgnoresUnrelatedInput(t *testing.T) {
	controller := pairingSignalController{
		pidPath:    "/missing",
		executable: "/missing",
	}
	if err := controller.Apply(
		context.Background(),
		inputEvent{Name: "volumeup", Step: "1"},
	); err != nil {
		t.Fatal(err)
	}
}

func TestPairingControllerRejectsWrongExecutable(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "pairing.pid")
	if err := os.WriteFile(
		pidPath,
		[]byte(strconv.Itoa(os.Getpid())),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	controller := pairingSignalController{
		pidPath:    pidPath,
		executable: "/not-this-test",
	}
	if err := controller.Apply(
		context.Background(),
		inputEvent{Name: "bluetooth-long"},
	); err == nil {
		t.Fatal("wrong executable was accepted")
	}
}

func TestPairingControllerSignalsVerifiedAgent(t *testing.T) {
	var signaledPID int
	var signaledSignal syscall.Signal
	controller := pairingSignalController{
		pidPath:    "/run/reinvoke/pairing-agent.pid",
		executable: "/opt/reinvoke/bin/bluez-pairing-agent",
		readFile: func(string) ([]byte, error) {
			return []byte("42\n"), nil
		},
		readlink: func(string) (string, error) {
			return "/opt/reinvoke/bin/bluez-pairing-agent", nil
		},
		signal: func(pid int, signal syscall.Signal) error {
			signaledPID = pid
			signaledSignal = signal
			return nil
		},
	}
	if err := controller.Apply(
		context.Background(),
		inputEvent{Name: "bluetooth-long"},
	); err != nil {
		t.Fatal(err)
	}
	if signaledPID != 42 || signaledSignal != syscall.SIGUSR1 {
		t.Fatalf("signal = (%d, %v)", signaledPID, signaledSignal)
	}
}
