// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// dspVolumeController owns the speaker's volume and mute state.
//
// This replaces a controller that shelled out to bluealsa-cli. BlueALSA is the
// BlueZ ALSA bridge, and this runtime replaced BlueZ with the donor Bluedroid
// stack, so that binary is not shipped and every volume request failed with
// "fork/exec /opt/reinvoke/bin/bluealsa-cli: no such file or directory".
//
// Candidate 05.8.6 held the level here and reported it to callers but never
// applied it, so the speaker played at whatever the DSP happened to boot with
// and the rotary control moved a number that reached no hardware. Verified on
// hardware during a looped playback: calling com.harman.dsp.volumeSet with 10,
// then 90, then 5 produced clearly audible quiet, loud and quiet again. That
// procedure is a plain WAMP registration of the DSP service, so it needs
// neither the microphone control socket extended nor the DSP binary rebuilt,
// which is what previously blocked this.
//
// The DSP takes a single byte. Percent is passed straight through: the scale
// is NOT established as linear, and the only measured points are 5 and 10
// (comfortable) against 90 (loud), so values are kept in that low range rather
// than scaled up to fill the byte.
type dspVolumeController struct {
	socket string

	// caller invokes a WAMP procedure; it is the same fixed caller the top-tap
	// pause uses. Left empty the controller keeps working and simply does not
	// reach the hardware, which is the 05.8.6 behaviour.
	caller, host, realm, dspProcedure string
	port                              int

	// pushes carries the latest level to the worker. It holds one entry so a
	// fast rotary sweep collapses to the most recent value instead of queuing
	// a process per detent.
	pushes chan int
	logf   func(string, ...interface{})

	mu     sync.Mutex
	volume int
	muted  bool

	// musicStatePath, when set, remembers the chosen level across restarts.
	musicStatePath string
	savedVolume    int
	hasSavedVolume bool
}

type volumeSnapshot struct {
	Volume int
	Muted  bool
}

// defaultVolume is where the speaker starts before anything sets a level. The
// DSP scale is not percent. Measured on hardware through a Bluetooth stream:
// unattenuated was "too loud", 5 was "a bit on the louder side" and 3 was
// "soft but clearly audible". Percent is passed to the DSP unscaled, so this
// starts at the level that was actually judged comfortable.
const defaultVolume = 3

func newDSPVolumeController(socket string) (*dspVolumeController, error) {
	if socket == "" {
		return nil, errors.New("DSP control socket is required for volume")
	}
	return &dspVolumeController{
		socket: socket,
		volume: defaultVolume,
		pushes: make(chan int, 1),
	}, nil
}

// Apply handles the physical rotary control.
func (controller *dspVolumeController) Apply(
	ctx context.Context,
	event inputEvent,
) error {
	if event.Name != "volumeup" && event.Name != "volumedown" {
		return nil
	}
	delta, err := strconv.Atoi(event.Step)
	if err != nil || delta < 1 || delta > 100 {
		return errors.New("invalid rotary step")
	}
	if event.Name == "volumedown" {
		delta = -delta
	}
	_, err = controller.AdjustVolume(ctx, delta)
	return err
}

func clampVolume(percent int) int {
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
}

// applyLocked records the effective level and asks the worker to push it.
func (controller *dspVolumeController) applyLocked(ctx context.Context) error {
	// Mute is held in its own field rather than by zeroing the level, so the
	// chosen volume is always what gets remembered and unmuting restores it.
	err := controller.rememberMusicVolume(controller.volume)
	controller.requestPushLocked()
	return err
}

// requestPushLocked queues the effective level. The channel holds one entry and
// the oldest is dropped, so a fast sweep of the rotary control settles on the
// level the user stopped at rather than replaying every step.
func (controller *dspVolumeController) requestPushLocked() {
	if controller.pushes == nil {
		return
	}
	level := controller.volume
	if controller.muted {
		level = 0
	}
	for {
		select {
		case controller.pushes <- level:
			return
		default:
		}
		select {
		case <-controller.pushes:
		default:
			return
		}
	}
}

// Run applies queued levels to the DSP until the context is cancelled.
func (controller *dspVolumeController) Run(ctx context.Context) {
	if controller.pushes == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case level := <-controller.pushes:
			if ctx.Err() != nil {
				return
			}
			request, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := controller.pushVolume(request, level)
			cancel()
			if err != nil && ctx.Err() == nil && controller.logf != nil {
				controller.logf("apply DSP volume %d: %v", level, err)
			}
		}
	}
}

// pushVolume hands one level to the DSP service. The DSP accepts a single
// byte, so the level is range checked here rather than relying on the callee.
func (controller *dspVolumeController) pushVolume(
	ctx context.Context,
	level int,
) error {
	if controller.caller == "" || controller.dspProcedure == "" {
		return nil
	}
	if level < 0 || level > 0xff {
		return fmt.Errorf("level %d is outside the DSP byte range", level)
	}
	command := exec.CommandContext(ctx, controller.caller,
		"--router-host", controller.host,
		"--router-port", strconv.Itoa(controller.port),
		"--realm", controller.realm,
		"--call", controller.dspProcedure,
		"--call-args", "["+strconv.Itoa(level)+"]")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", controller.dspProcedure, err, output)
	}
	return nil
}

func (controller *dspVolumeController) snapshotLocked() volumeSnapshot {
	return volumeSnapshot{Volume: controller.volume, Muted: controller.muted}
}

func (controller *dspVolumeController) Snapshot(
	ctx context.Context,
) (volumeSnapshot, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.snapshotLocked(), nil
}

func (controller *dspVolumeController) SetVolume(
	ctx context.Context,
	percent int,
) (volumeSnapshot, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.volume = clampVolume(percent)
	if err := controller.applyLocked(ctx); err != nil {
		return controller.snapshotLocked(), err
	}
	return controller.snapshotLocked(), nil
}

func (controller *dspVolumeController) AdjustVolume(
	ctx context.Context,
	delta int,
) (volumeSnapshot, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.volume = clampVolume(controller.volume + delta)
	if err := controller.applyLocked(ctx); err != nil {
		return controller.snapshotLocked(), err
	}
	return controller.snapshotLocked(), nil
}

func (controller *dspVolumeController) SetMuted(
	ctx context.Context,
	muted bool,
) (volumeSnapshot, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.muted = muted
	if err := controller.applyLocked(ctx); err != nil {
		return controller.snapshotLocked(), err
	}
	return controller.snapshotLocked(), nil
}

func (controller *dspVolumeController) ToggleMuted(
	ctx context.Context,
) (volumeSnapshot, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.muted = !controller.muted
	if err := controller.applyLocked(ctx); err != nil {
		return controller.snapshotLocked(), err
	}
	return controller.snapshotLocked(), nil
}
