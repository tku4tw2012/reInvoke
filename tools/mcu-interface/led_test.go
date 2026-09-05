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

func TestClearLEDsUsesRecoveredOffContract(t *testing.T) {
	writer := &recordingLEDWriter{}
	if err := clearLEDs(writer); err != nil {
		t.Fatal(err)
	}
	if len(writer.packets) != 1 {
		t.Fatalf("packet count = %d, want 1", len(writer.packets))
	}
	packet := writer.packets[0]
	if len(packet) != 2+3*ledFrameBytes {
		t.Fatalf("packet length = %d, want 41", len(packet))
	}
	if packet[0] != ledAnimationCode || packet[1] != ledFirstChunkFlag {
		t.Fatalf("packet header = %x, want 0e01", packet[:2])
	}
	for index, value := range packet[2:] {
		if value != 0 {
			t.Fatalf("packet byte %d = %02x, want 00", index+2, value)
		}
	}

}

func TestPrivacyIndicatorBlocksTransientAnimationsAndLEDOff(t *testing.T) {
	writer := &recordingLEDWriter{}
	player := &ledPlayer{
		writer:       writer,
		privacyMuted: true,
	}
	if err := player.Apply(
		context.Background(),
		inputEvent{Name: "action"},
	); err != nil {
		t.Fatal(err)
	}
	if err := player.Stop(); err != nil {
		t.Fatal(err)
	}
	if len(writer.packets) != 0 {
		t.Fatalf("privacy indicator was replaced: %d packets", len(writer.packets))
	}

	if err := player.SetPrivacyMuted(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if player.privacyMuted {
		t.Fatal("privacy indicator remained locked after unmute")
	}
	if len(writer.packets) != 1 || len(writer.packets[0]) != 41 {
		t.Fatalf("privacy clear packets = %#v", writer.packets)
	}
}
