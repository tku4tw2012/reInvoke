// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeHeartbeatWriter struct {
	frames [][6]byte
	err    error
	cancel context.CancelFunc
}

func (writer *fakeHeartbeatWriter) WriteMCUCommand(frame [6]byte) error {
	writer.frames = append(writer.frames, frame)
	if len(writer.frames) == 2 && writer.cancel != nil {
		writer.cancel()
	}
	return writer.err
}

func TestMCUHeartbeatUsesRecoveredFiveSecondFrame(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	writer := &fakeHeartbeatWriter{cancel: cancel}

	if err := runMCUHeartbeat(ctx, writer, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(writer.frames) != 2 {
		t.Fatalf("heartbeat count = %d, want 2", len(writer.frames))
	}
	for _, frame := range writer.frames {
		if frame != [6]byte{0x24} {
			t.Fatalf("heartbeat frame = %x, want 240000000000", frame)
		}
	}
}

func TestMCUHeartbeatPropagatesWriteFailure(t *testing.T) {
	writer := &fakeHeartbeatWriter{err: errors.New("write failed")}

	err := runMCUHeartbeat(context.Background(), writer, time.Second)
	if err == nil || err.Error() != "send MCU heartbeat: write failed" {
		t.Fatalf("unexpected error: %v", err)
	}
}
