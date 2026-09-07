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

func TestAuthorityEpochValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "epoch")
	valid := "00112233445566778899aabbccddeeff\n"
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readAuthorityEpoch(path); err != nil ||
		got != "00112233445566778899aabbccddeeff" {
		t.Fatalf("epoch=%q err=%v", got, err)
	}
	for _, invalid := range []string{
		"short\n",
		"00112233445566778899AABBCCDDEEFF\n",
		"00112233445566778899aabbccddeefg\n",
	} {
		if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readAuthorityEpoch(path); err == nil {
			t.Fatalf("accepted invalid epoch %q", invalid)
		}
	}
}
