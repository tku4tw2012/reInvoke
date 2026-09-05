// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMicrophoneStateInitializesUnmuted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "microphone-state")
	muted, err := loadOrInitializeMicrophoneState(path)
	if err != nil {
		t.Fatal(err)
	}
	if muted {
		t.Fatal("new microphone state initialized muted")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != microphoneUnmutedState {
		t.Fatalf("state = %q, want %q", content, microphoneUnmutedState)
	}
}

func TestMicrophoneStatePersistsMute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "microphone-state")
	if err := persistMicrophoneState(path, true); err != nil {
		t.Fatal(err)
	}
	muted, err := loadOrInitializeMicrophoneState(path)
	if err != nil {
		t.Fatal(err)
	}
	if !muted {
		t.Fatal("persisted microphone mute was not loaded")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode = %o, want 600", info.Mode().Perm())
	}
}

func TestMicrophoneStateRejectsInvalidContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "microphone-state")
	if err := os.WriteFile(path, []byte("unknown\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrInitializeMicrophoneState(path); err == nil {
		t.Fatal("invalid microphone state was accepted")
	}
}
