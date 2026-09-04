// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"testing"
	"time"
)

type recordingInputController struct {
	events chan inputEvent
}

func (controller recordingInputController) Apply(
	ctx context.Context,
	event inputEvent,
) error {
	select {
	case controller.events <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestEventRelayAppliesControlWithoutWAMPConsumer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := make(chan inputEvent, 1)
	output := make(chan inputEvent, 1)
	applied := make(chan inputEvent, 1)
	go runEventRelay(
		ctx,
		input,
		recordingInputController{events: applied},
		output,
		nil,
	)

	want := inputEvent{Name: "volumeup", Step: "2"}
	input <- want
	select {
	case got := <-applied:
		if got != want {
			t.Fatalf("applied event = %#v, want %#v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("input control was not applied")
	}
}

func TestVolumeQueueCoalescesPendingDeltas(t *testing.T) {
	deltas := make(chan int, 1)
	if !queueVolumeDelta(deltas, 3) {
		t.Fatal("initial delta was not queued")
	}
	if !queueVolumeDelta(deltas, -1) {
		t.Fatal("second delta was not coalesced")
	}
	if got := <-deltas; got != 2 {
		t.Fatalf("coalesced delta = %d, want 2", got)
	}
}
