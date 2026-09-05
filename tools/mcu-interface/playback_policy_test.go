// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"
)

type recordingPlaybackController struct {
	mu     sync.Mutex
	states []bool
}

type failingActivationController struct {
	recordingPlaybackController
}

func (controller *failingActivationController) setPlaybackActive(
	active bool,
) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.states = append(controller.states, active)
	if active {
		return errors.New("activation failed")
	}
	return nil
}

func (controller *recordingPlaybackController) setPlaybackActive(
	active bool,
) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.states = append(controller.states, active)
	return nil
}

func (controller *recordingPlaybackController) snapshot() []bool {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return append([]bool(nil), controller.states...)
}

func TestPlaybackIsRunningRequiresRunningState(t *testing.T) {
	if !playbackIsRunning([]byte("state: RUNNING\nhw_ptr: 256\n")) {
		t.Fatal("RUNNING state was not recognized")
	}
	if playbackIsRunning([]byte("state: PREPARED\n")) {
		t.Fatal("non-running state was accepted")
	}
	pid, ok := playbackOwnerPID([]byte("owner_pid   : 42\n"))
	if !ok || pid != 42 {
		t.Fatalf("owner PID = %d, valid=%t", pid, ok)
	}
	pid, ok = playbackLeasePID([]byte("42\n"))
	if !ok || pid != 42 {
		t.Fatalf("lease PID = %d, valid=%t", pid, ok)
	}
}

func TestPlaybackPolicyTracksTransitionsAndRemutesOnCancel(t *testing.T) {
	tempDir := t.TempDir()
	statusPath := filepath.Join(tempDir, "status")
	leasePath := filepath.Join(tempDir, "lease")
	ownerPID := syscall.Gettid()
	ownerExecutable, err := os.Readlink("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statusPath, []byte("closed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := &recordingPlaybackController{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runPlaybackPolicy(
			ctx,
			statusPath,
			leasePath,
			ownerExecutable,
			controller,
			time.Millisecond,
			20*time.Millisecond,
			nil,
		)
	}()

	if err := os.WriteFile(
		statusPath,
		[]byte("state: RUNNING\nowner_pid   : "+strconv.Itoa(ownerPID)+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if states := controller.snapshot(); len(states) != 0 {
		t.Fatalf("policy activated without a playback lease: %v", states)
	}
	if err := os.WriteFile(
		leasePath,
		[]byte(strconv.Itoa(ownerPID)+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(controller.snapshot()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := os.Remove(leasePath); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for {
		states := controller.snapshot()
		if len(states) >= 2 && !states[len(states)-1] {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("policy did not remute after lease removal: %v", states)
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	states := controller.snapshot()
	if len(states) < 2 || !states[0] || states[len(states)-1] {
		t.Fatalf("policy states = %v, want active then muted", states)
	}
}

func TestPlaybackPolicyHoldsActiveDuringBriefUnderrun(t *testing.T) {
	tempDir := t.TempDir()
	statusPath := filepath.Join(tempDir, "status")
	leasePath := filepath.Join(tempDir, "lease")
	ownerPID := syscall.Gettid()
	ownerExecutable, err := os.Readlink("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		statusPath,
		[]byte("state: RUNNING\nowner_pid   : "+strconv.Itoa(ownerPID)+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		leasePath,
		[]byte(strconv.Itoa(ownerPID)+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	controller := &recordingPlaybackController{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runPlaybackPolicy(
			ctx,
			statusPath,
			leasePath,
			ownerExecutable,
			controller,
			time.Millisecond,
			100*time.Millisecond,
			nil,
		)
	}()

	deadline := time.Now().Add(time.Second)
	for len(controller.snapshot()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if states := controller.snapshot(); len(states) != 1 || !states[0] {
		t.Fatalf("expected initial active state, got %v", states)
	}

	// Simulate a brief 10ms ALSA underrun
	if err := os.WriteFile(statusPath, []byte("state: XRUN\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Millisecond)
	if states := controller.snapshot(); len(states) != 1 {
		t.Fatalf("holdoff failed: muted during brief underrun: %v", states)
	}

	// Audio resumes
	if err := os.WriteFile(
		statusPath,
		[]byte("state: RUNNING\nowner_pid   : "+strconv.Itoa(ownerPID)+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Millisecond)
	if states := controller.snapshot(); len(states) != 1 {
		t.Fatalf("state flapped during brief underrun recovery: %v", states)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPlaybackPolicyDoesNotCreateActivationFromHoldoff(t *testing.T) {
	tempDir := t.TempDir()
	statusPath := filepath.Join(tempDir, "status")
	leasePath := filepath.Join(tempDir, "lease")
	ownerPID := syscall.Gettid()
	ownerExecutable, err := os.Readlink("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		statusPath,
		[]byte("state: RUNNING\nowner_pid   : "+strconv.Itoa(ownerPID)+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		leasePath,
		[]byte(strconv.Itoa(ownerPID)+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	controller := &failingActivationController{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runPlaybackPolicy(
			ctx,
			statusPath,
			leasePath,
			ownerExecutable,
			controller,
			100*time.Millisecond,
			time.Second,
			nil,
		)
	}()

	deadline := time.Now().Add(time.Second)
	for len(controller.snapshot()) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := os.WriteFile(statusPath, []byte("closed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(leasePath); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	activeCalls := 0
	for _, active := range controller.snapshot() {
		if active {
			activeCalls++
		}
	}
	if activeCalls != 1 {
		t.Fatalf("activation calls = %d, want only the failed attempt", activeCalls)
	}
}
