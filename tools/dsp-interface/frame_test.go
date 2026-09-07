// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"testing"
)

func TestBuildAndDecodeCapturedBootFrame(t *testing.T) {
	encoded, err := buildFrame(messageIDBoot, []byte{0x04})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x00, 0x01, 0x00, 0x01, 0x06, 0x04, 0x00, 0x00}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("encoded frame = %x, want %x", encoded, want)
	}

	decoded, err := decodeDeviceFrame(encoded[:5], encoded[5:])
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ID != messageIDBoot ||
		!bytes.Equal(decoded.Payload, []byte{0x04}) {
		t.Fatalf("decoded frame = %#v", decoded)
	}
}

func TestDecodeDeviceFrameRejectsCorruptChecksum(t *testing.T) {
	header := []byte{0x00, 0x01, 0x00, 0x01, 0x07}
	if _, err := decodeDeviceFrame(header, []byte{0x04, 0x00, 0x00}); err == nil {
		t.Fatal("corrupt checksum was accepted")
	}
}
