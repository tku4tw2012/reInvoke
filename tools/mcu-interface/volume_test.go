// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
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
// TestSoftvolTrimsWithoutMovingTheCalibration proves the softvol control fills
// in between DSP gain steps and does not become the level itself.
//
// The donor mapped percent straight onto this control. That cannot be done
// here: the level measured as comfortable on this unit is DSP gain 5 with the
// control at 255, so moving the dial onto the control would need the DSP to
// make up 33.6 dB at the default position, which is gain 239 against a
// measured loud point of 90.
func TestSoftvolTrimsWithoutMovingTheCalibration(t *testing.T) {
	// Silence stays silence, and nothing asks for more than the control has.
	if got := softvolForPercent(0); got != 0 {
		t.Fatalf("a dial at zero gave softvol %d", got)
	}
	for percent := 1; percent <= 100; percent++ {
		got := softvolForPercent(percent)
		if got < 0 || got > softvolMax {
			t.Fatalf("dial %d asked for softvol %d", percent, got)
		}
	}

	// The measured anchor must be untouched: at the default dial the DSP byte
	// is exactly what was calibrated, so there is nothing to trim.
	if got := softvolForPercent(defaultVolume); got != softvolMax {
		t.Fatalf("the default dial trims softvol to %d; the calibration moved",
			got)
	}

	// The combination has to rise with the dial. Trimming is only useful if
	// the result is still monotonic.
	previous := math.Inf(-1)
	for percent := 1; percent <= 100; percent++ {
		gain := float64(dspByteForPercent(percent))
		trim := float64(softvolForPercent(percent))
		level := 20*math.Log10(gain/dspMaxByte) +
			(-softvolRangeDB + softvolRangeDB*trim/softvolMax)
		if level < previous-0.01 {
			t.Fatalf("dial %d is quieter than the step below it", percent)
		}
		previous = level
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

// TestVolumeMaxCuePlaysOnArrivalOnly proves the cue for reaching the top of
// the range fires when it is reached and not while sitting there.
//
// Volume_Max ships in the donor's sounds directory and was installed by every
// build without anything ever playing it. No donor binary names it, but none
// names Power_On, BT_Pairing or BT_Connected either, and those are wired from
// their filenames; the evidence for all four is identical.
func TestVolumeMaxCuePlaysOnArrivalOnly(t *testing.T) {
	controller := newTestVolumeController(t)
	controller.push = func(context.Context, int) error { return nil }
	played := 0
	controller.atMax = func() { played++ }
	ctx := context.Background()

	if _, err := controller.SetVolume(ctx, maxVolume-2); err != nil {
		t.Fatal(err)
	}
	if played != 0 {
		t.Fatalf("cue played %d times below maximum", played)
	}

	if _, err := controller.AdjustVolume(ctx, 5); err != nil {
		t.Fatal(err)
	}
	if played != 1 {
		t.Fatalf("cue played %d times on reaching maximum, want 1", played)
	}

	// Still turning up at the top must not repeat it.
	for i := 0; i < 3; i++ {
		if _, err := controller.AdjustVolume(ctx, 5); err != nil {
			t.Fatal(err)
		}
	}
	if played != 1 {
		t.Fatalf("cue repeated while sitting at maximum: %d plays", played)
	}

	// Leaving and returning plays it again.
	if _, err := controller.AdjustVolume(ctx, -10); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AdjustVolume(ctx, 20); err != nil {
		t.Fatal(err)
	}
	if played != 2 {
		t.Fatalf("returning to maximum played %d times, want 2", played)
	}
}

// TestDialFollowsTheDonorCurve proves the dial is logarithmic in amplitude and
// still lands on the two levels measured on this unit.
//
// The donor carried volume on an ALSA softvol control declared with no min_dB
// or max_dB, so it took the plugin defaults and was linear in decibels. This
// runtime has no softvol control, and passing percent straight to DSP gain
// made the dial linear in amplitude instead: the comfortable point sat at 5
// of 100, so almost the whole travel was above it.
func TestDialFollowsTheDonorCurve(t *testing.T) {
	// The anchors. Both were measured by listening, and the curve exists to
	// pass through them.
	if got := dspByteForPercent(defaultVolume); got != 5 {
		t.Fatalf("the default dial produces gain %d, want the measured 5", got)
	}
	if got := dspByteForPercent(100); got != 90 {
		t.Fatalf("a full dial produces gain %d, want the measured 90", got)
	}

	// Monotonic, and never silent while the dial is up.
	previous := 0
	for percent := 1; percent <= 100; percent++ {
		level := dspByteForPercent(percent)
		if level < 1 {
			t.Fatalf("dial %d is silent", percent)
		}
		if level < previous {
			t.Fatalf("dial %d produced %d after %d", percent, level, previous)
		}
		previous = level
	}
	if dspByteForPercent(0) != 0 {
		t.Fatal("a dial at zero should be silent")
	}

	// Logarithmic, not linear: the bottom half of the dial must cover far
	// less than half the gain, which is what makes a knob usable.
	if half := dspByteForPercent(50); half > 90/4 {
		t.Fatalf("half the dial gives gain %d; the curve is too flat", half)
	}
}

// TestPushAppliesTheCurve proves the curve is wired into the path that reaches
// the DSP, not merely available to call.
//
// Testing dspByteForPercent alone passes whether or not anything uses it,
// which is the same shape of mistake as a log line saying the outputs are open
// while they are muted. This runs the real pushVolume against a stand-in
// caller and reads the argument it was given.
func TestPushAppliesTheCurve(t *testing.T) {
	script := filepath.Join(t.TempDir(), "caller.sh")
	record := filepath.Join(t.TempDir(), "args.txt")
	if err := os.WriteFile(script,
		[]byte("#!/bin/sh\nprintf '%s\\n' \"$@\" >>"+record+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	controller := newTestVolumeController(t)
	controller.caller = script
	controller.dspProcedure = "com.harman.dsp.volumeSet"

	if err := controller.pushVolume(context.Background(), defaultVolume); err != nil {
		t.Fatal(err)
	}

	written, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("the caller was never run: %v", err)
	}
	// The measured comfortable gain for the default dial position.
	if !strings.Contains(string(written), "[5]") {
		t.Fatalf("the DSP was called with %q, want the curved gain [5]",
			strings.TrimSpace(string(written)))
	}
}
