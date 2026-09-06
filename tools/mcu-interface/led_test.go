// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type recordingLEDWriter struct {
	packets [][]byte
}

type recoveringLEDWriter struct {
	mu        sync.Mutex
	calls     int
	recovered chan struct{}
	once      sync.Once
}

func (writer *recoveringLEDWriter) WriteMCUData([]byte) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.calls++
	if writer.calls == 2 {
		return errors.New("injected second-chunk failure")
	}
	if writer.calls >= 4 {
		writer.once.Do(func() { close(writer.recovered) })
	}
	return nil
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

func TestPrivacyIndicatorRetriesAfterPostStartFailure(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(directory, micPrivacyLEDName+".bin"),
		make([]byte, ledChunkBytes+ledFrameBytes),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	writer := &recoveringLEDWriter{recovered: make(chan struct{})}
	var logged int
	player := &ledPlayer{
		directory: directory,
		writer:    writer,
		logf: func(string, ...interface{}) {
			logged++
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := player.SetPrivacyMuted(ctx, true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-writer.recovered:
	case <-time.After(2 * time.Second):
		t.Fatal("privacy animation did not recover after second-chunk failure")
	}
	if logged == 0 {
		t.Fatal("post-start privacy animation failure was not logged")
	}
	if err := player.SetPrivacyMuted(ctx, false); err != nil {
		t.Fatal(err)
	}
}

func TestCancelledSessionCannotResumeQueuedAnimation(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(directory, "animation.bin"),
		make([]byte, ledFrameBytes),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	writer := &recordingLEDWriter{}
	player := &ledPlayer{directory: directory, writer: writer}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	player.mu.Lock()
	go func() { done <- player.Start(ctx, "animation", false) }()
	cancel()
	player.mu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("queued animation error = %v, want cancellation", err)
	}
	if len(writer.packets) != 0 {
		t.Fatalf("cancelled session wrote %d LED packets", len(writer.packets))
	}
}
