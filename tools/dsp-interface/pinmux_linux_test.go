// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type recordingPinmuxRunner struct {
	value uint32
	calls [][]string
	err   error
}

func (runner *recordingPinmuxRunner) run(
	_ context.Context,
	name string,
	args ...string,
) ([]byte, error) {
	call := append([]string{name}, args...)
	runner.calls = append(runner.calls, call)
	if runner.err != nil {
		return []byte("devmem rejected"), runner.err
	}
	if len(args) == 4 {
		value, err := strconv.ParseUint(args[3], 0, 32)
		if err != nil {
			return nil, err
		}
		runner.value = uint32(value)
		return nil, nil
	}
	return []byte(fmt.Sprintf("0x%08X\n", runner.value)), nil
}

func TestConfigureDSPPinmuxPreservesUnrelatedBits(t *testing.T) {
	runner := &recordingPinmuxRunner{value: 0x0038D249}

	messageValue, err := configureDSPPinmux(
		context.Background(),
		"/bin/busybox",
		filepath.Join(t.TempDir(), "pinmux.lock"),
		true,
		runner.run,
	)
	if err != nil {
		t.Fatal(err)
	}
	if messageValue != 0x0138D249 {
		t.Fatalf("message pinmux = 0x%08X", messageValue)
	}
	if messageValue&0x00200000 == 0 {
		t.Fatal("message pinmux cleared the MCU GPIO3 function bit")
	}

	downloadValue, err := configureDSPPinmux(
		context.Background(),
		"/bin/busybox",
		filepath.Join(t.TempDir(), "pinmux.lock"),
		false,
		runner.run,
	)
	if err != nil {
		t.Fatal(err)
	}
	if downloadValue != 0x0038D249 {
		t.Fatalf("download pinmux = 0x%08X", downloadValue)
	}
	if downloadValue&0x00200000 == 0 {
		t.Fatal("download pinmux cleared the MCU GPIO3 function bit")
	}
}

func TestConfigureDSPPinmuxUsesBusyBoxDevmem(t *testing.T) {
	runner := &recordingPinmuxRunner{value: 0x0038D249}
	if _, err := configureDSPPinmux(
		context.Background(),
		"/bin/busybox",
		filepath.Join(t.TempDir(), "pinmux.lock"),
		true,
		runner.run,
	); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("calls = %v", runner.calls)
	}
	write := strings.Join(runner.calls[1], " ")
	if write != "/bin/busybox devmem 0xF7EA8008 32 0x0138D249" {
		t.Fatalf("write = %q", write)
	}
}

func TestConfigureDSPPinmuxPropagatesFailure(t *testing.T) {
	want := errors.New("devmem failed")
	runner := &recordingPinmuxRunner{err: want}
	_, err := configureDSPPinmux(
		context.Background(),
		"/bin/busybox",
		filepath.Join(t.TempDir(), "pinmux.lock"),
		true,
		runner.run,
	)
	if !errors.Is(err, want) ||
		!strings.Contains(err.Error(), "devmem rejected") {
		t.Fatalf("error = %v, want wrapped command failure", err)
	}
}

func TestConfigureDSPPinmuxRequiresTool(t *testing.T) {
	_, err := configureDSPPinmux(
		context.Background(),
		"",
		filepath.Join(t.TempDir(), "pinmux.lock"),
		true,
		nil,
	)
	if err == nil {
		t.Fatal("empty pinmux tool was accepted")
	}
}

func TestConfigureDSPPinmuxWaitsForSharedLock(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "pinmux.lock")
	holder, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	runner := &recordingPinmuxRunner{value: 0x0038D249}
	done := make(chan error, 1)
	go func() {
		_, err := configureDSPPinmux(
			context.Background(),
			"/bin/busybox",
			lockPath,
			true,
			runner.run,
		)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("pinmux update bypassed held lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("pinmux update did not continue after lock release")
	}
}
