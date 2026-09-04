// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"testing"
)

type recordingLEDWriter struct {
	packets [][]byte
}

func (writer *recordingLEDWriter) WriteMCUData(data []byte) error {
	writer.packets = append(writer.packets, append([]byte(nil), data...))
	return nil
}

func TestLEDAnimationUsesRecoveredChunkContract(t *testing.T) {
	writer := &recordingLEDWriter{}
	data := make([]byte, ledChunkBytes+ledFrameBytes)
	if err := runLEDAnimation(
		context.Background(),
		writer,
		data,
		false,
	); err != nil {
		t.Fatal(err)
	}
	if len(writer.packets) != 2 {
		t.Fatalf("packet count = %d, want 2", len(writer.packets))
	}
	if len(writer.packets[0]) != 392 ||
		writer.packets[0][0] != 0x0e ||
		writer.packets[0][1] != 0x01 {
		t.Fatalf("first packet = %x", writer.packets[0][:2])
	}
	if len(writer.packets[1]) != 15 ||
		writer.packets[1][0] != 0x0e ||
		writer.packets[1][1] != 0x00 {
		t.Fatalf("second packet = %x", writer.packets[1][:2])
	}
}

func TestLEDNameRejectsPathTraversal(t *testing.T) {
	for _, name := range []string{"", "../pattern", "a/b", "pattern.bin"} {
		if validLEDName(name) {
			t.Fatalf("invalid LED name accepted: %q", name)
		}
	}
}
