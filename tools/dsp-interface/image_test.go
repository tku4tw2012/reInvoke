// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReverseBitsMatchesCapturedLoaderBytes(t *testing.T) {
	if got := reverseBits(0xa7); got != 0xe5 {
		t.Fatalf("reverseBits(0xa7) = 0x%02x, want 0xe5", got)
	}
	if got := reverseBits(0xc0); got != 0x03 {
		t.Fatalf("reverseBits(0xc0) = 0x%02x, want 0x03", got)
	}
}

func TestRecoveredBootImageMatchesWireCapture(t *testing.T) {
	path := os.Getenv("REINVOKE_DSP_IMAGE")
	if path == "" {
		t.Skip("REINVOKE_DSP_IMAGE is not set")
	}
	image, err := loadBootImage(path)
	if err != nil {
		t.Fatal(err)
	}
	if !image.Matches() {
		t.Fatalf("recovered image did not match: %#v", image)
	}
}

func TestLoadBootImageRejectsPartialTransfer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.ldr")
	if err := os.WriteFile(path, []byte{1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadBootImage(path); err == nil {
		t.Fatal("partial four-byte transfer was accepted")
	}
}
