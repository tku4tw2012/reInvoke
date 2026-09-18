// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"sync"
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

// The hardware powers up at its own level, so the configured one has to be
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

// fakeSoftvol records every value written so a test can assert the path taken,
// not just the destination.
type fakeSoftvol struct {
	value   int
	written []int
}

func (f *fakeSoftvol) Read() (int, error) { return f.value, nil }
func (f *fakeSoftvol) Write(v int) error {
	f.value = v
	f.written = append(f.written, v)
	return nil
}

// The donor faded its softvol on a tick rather than jumping. Jumping is what a
// listener heard as a stutter during a rotary sweep, so the intermediate steps
// are the behaviour under test, not an implementation detail.
func TestFadeWalksToTargetInSteps(t *testing.T) {
	controller := newTestVolumeController(t)
	fake := &fakeSoftvol{value: 0}
	controller.softvol = fake

	if err := controller.fadeToTarget(context.Background(), 60); err != nil {
		t.Fatalf("fadeToTarget: %v", err)
	}
	if len(fake.written) < 2 {
		t.Fatalf("expected a fade, got a jump: %v", fake.written)
	}
	if got := fake.written[len(fake.written)-1]; got != 60 {
		t.Errorf("final value = %d, want 60", got)
	}
	for i, v := range fake.written {
		if i > 0 && v-fake.written[i-1] > volumeFadeStep {
			t.Errorf("step %d -> %d exceeds %d", fake.written[i-1], v, volumeFadeStep)
		}
	}
}

func TestFadeDescendsAndStopsExactly(t *testing.T) {
	controller := newTestVolumeController(t)
	fake := &fakeSoftvol{value: 200}
	controller.softvol = fake

	if err := controller.fadeToTarget(context.Background(), 13); err != nil {
		t.Fatalf("fadeToTarget: %v", err)
	}
	if got := fake.value; got != 13 {
		t.Errorf("final value = %d, want 13", got)
	}
	for _, v := range fake.written {
		if v < 13 {
			t.Errorf("fade undershot to %d", v)
		}
	}
}

// Percent maps onto the control's own 0..255 range; the ends must be exact so
// zero is silent and full is full.
func TestSoftvolPercentMapping(t *testing.T) {
	for _, c := range []struct{ percent, want int }{
		{0, 0}, {100, softvolMax}, {50, 127}, {-5, 0}, {150, softvolMax},
	} {
		if got := softvolForPercent(c.percent); got != c.want {
			t.Errorf("softvolForPercent(%d) = %d, want %d", c.percent, got, c.want)
		}
	}
}

// countingRing records every arc the controller asks the microcontroller to
// draw.
type countingRing struct {
	mu     sync.Mutex
	levels []int
}

func (ring *countingRing) ShowVolume(level int) error {
	ring.mu.Lock()
	defer ring.mu.Unlock()
	ring.levels = append(ring.levels, level)
	return nil
}

func (ring *countingRing) count() int {
	ring.mu.Lock()
	defer ring.mu.Unlock()
	return len(ring.levels)
}

// TestRingIsDrawnOnlyForAVolumeChange proves the ring follows changes, as
// the donor did, and not this runtime's apply attempts.
//
// The ring used to be written on every attempt. The DSP registers its
// procedures seconds after this service starts, so each lost race lit the ring
// again and the number of illuminations at boot was simply how slow the DSP
// had been. Confirmed on hardware at one illumination per apply.
func TestRingIsDrawnOnlyForAVolumeChange(t *testing.T) {
	controller := newTestVolumeController(t)
	ring := &countingRing{}
	controller.ring = ring

	attempts := make(chan int, 8)
	var mu sync.Mutex
	fail := true
	controller.push = func(_ context.Context, level int) error {
		attempts <- level
		mu.Lock()
		defer mu.Unlock()
		if fail {
			return errors.New("no_such_procedure")
		}
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go controller.Run(ctx)

	select {
	case <-attempts:
	case <-time.After(3 * time.Second):
		t.Fatal("Run never attempted the configured level")
	}
	if drawn := ring.count(); drawn != 0 {
		t.Fatalf("a failed apply drew the ring %d times", drawn)
	}

	mu.Lock()
	fail = false
	mu.Unlock()

	// The startup assertion is not a change, so it must not draw either. The
	// donor only ever drew on com.harman.volumeChanged.
	select {
	case <-attempts:
	case <-time.After(volumeRetryDelay + 5*time.Second):
		t.Fatal("Run never retried the assertion")
	}
	time.Sleep(200 * time.Millisecond)
	if drawn := ring.count(); drawn != 0 {
		t.Fatalf("the startup assertion drew the ring %d times", drawn)
	}

	// An actual change must draw.
	if _, err := controller.SetVolume(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for ring.count() == 0 {
		select {
		case <-deadline:
			t.Fatal("a volume change never drew the ring")
		case <-time.After(50 * time.Millisecond):
		}
	}
	if drawn := ring.count(); drawn != 1 {
		t.Fatalf("one change drew the ring %d times, want 1", drawn)
	}
}
