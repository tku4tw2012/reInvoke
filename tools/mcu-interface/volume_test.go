// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTestVolumeController(t *testing.T) *dspVolumeController {
	t.Helper()
	controller, err := newDSPVolumeController("/run/reinvoke/test.sock")
	if err != nil {
		t.Fatalf("newDSPVolumeController: %v", err)
	}
	return controller
}

// The DSP powers up at its own gain, so the configured level has to be
// asserted rather than waited for. Candidate 05.8.8 only pushed on a later
// change, so the first stream played at full output however low the configured
// level was, and the speaker was reported as far too loud.
func TestRunAssertsConfiguredLevelWithoutAnyChange(t *testing.T) {
	controller := newTestVolumeController(t)
	levels := make(chan int, 4)
	controller.push = func(_ context.Context, level int) error {
		levels <- level
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go controller.Run(ctx)

	select {
	case level := <-levels:
		if level != defaultVolume {
			t.Errorf("startup level = %d, want %d", level, defaultVolume)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run never asserted the configured level")
	}
}

// A failed assertion must be retried: the DSP service registers its procedures
// after this one starts, so the first attempt can lose a race nobody can hear.
func TestRunRetriesAFailedAssertion(t *testing.T) {
	controller := newTestVolumeController(t)
	attempts := make(chan int, 8)
	controller.push = func(_ context.Context, level int) error {
		attempts <- level
		if len(attempts) < 2 {
			return errors.New("DSP procedure not registered yet")
		}
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go controller.Run(ctx)

	for i := 0; i < 2; i++ {
		select {
		case <-attempts:
		case <-time.After(volumeRetryDelay + 3*time.Second):
			t.Fatalf("attempt %d never happened", i+1)
		}
	}
}

// Each push reads the level when it runs, so a burst of rotary steps settles
// on where the dial stopped and never applies a stale one. The queue-the-level
// design this replaced could strand the newest value when a send found the
// channel full and then lost the drain race to the worker.
func TestPushUsesTheLevelAtSendTime(t *testing.T) {
	controller := newTestVolumeController(t)
	release := make(chan struct{})
	levels := make(chan int, 8)
	controller.push = func(_ context.Context, level int) error {
		<-release
		levels <- level
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go controller.Run(ctx)

	// Let the startup assertion enter the push and block there.
	time.Sleep(100 * time.Millisecond)
	for _, level := range []int{10, 20, 30} {
		if _, err := controller.SetVolume(ctx, level); err != nil {
			t.Fatalf("SetVolume(%d): %v", level, err)
		}
	}
	close(release)

	deadline := time.After(3 * time.Second)
	for {
		select {
		case level := <-levels:
			if level == 30 {
				return
			}
			if level != defaultVolume && level != 10 && level != 20 {
				t.Fatalf("unexpected level %d", level)
			}
		case <-deadline:
			t.Fatal("the final level was never applied")
		}
	}
}

// Mute holds the chosen level and applies zero; unmuting restores it.
func TestMuteAppliesZeroAndRestores(t *testing.T) {
	controller := newTestVolumeController(t)
	ctx := context.Background()
	if _, err := controller.SetVolume(ctx, 40); err != nil {
		t.Fatalf("SetVolume: %v", err)
	}
	if _, err := controller.SetMuted(ctx, true); err != nil {
		t.Fatalf("SetMuted(true): %v", err)
	}
	if level := controller.effectiveLevel(); level != 0 {
		t.Errorf("muted level = %d, want 0", level)
	}
	if _, err := controller.SetMuted(ctx, false); err != nil {
		t.Fatalf("SetMuted(false): %v", err)
	}
	if level := controller.effectiveLevel(); level != 40 {
		t.Errorf("restored level = %d, want 40", level)
	}
}
