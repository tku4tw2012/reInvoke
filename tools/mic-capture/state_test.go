// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMicrophoneStateIsFailClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "microphone-state")
	if muted, err := readMicrophoneMuted(path); err == nil || !muted {
		t.Fatalf("missing state muted=%v err=%v", muted, err)
	}
	if err := os.WriteFile(path, []byte("unknown\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if muted, err := readMicrophoneMuted(path); err == nil || !muted {
		t.Fatalf("invalid state muted=%v err=%v", muted, err)
	}
	if err := os.WriteFile(path, []byte("muted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if muted, err := readMicrophoneMuted(path); err != nil || !muted {
		t.Fatalf("muted state muted=%v err=%v", muted, err)
	}
	if err := os.WriteFile(path, []byte("unmuted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if muted, err := readMicrophoneMuted(path); err != nil || muted {
		t.Fatalf("unmuted state muted=%v err=%v", muted, err)
	}
}
