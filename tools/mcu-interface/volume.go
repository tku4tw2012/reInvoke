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

	// notify wakes the worker. It holds one entry, so a fast rotary sweep
	// collapses to a single push of wherever the dial stopped instead of one
	// subprocess per detent.
	notify chan struct{}
	logf   func(string, ...interface{})
	// push sends one level to the DSP. Tests replace it; production leaves it
	// nil and the subprocess caller is used.
	push func(context.Context, int) error

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

const (
	// volumePushTimeout bounds one call to the DSP service.
	volumePushTimeout = 3 * time.Second
	// volumeRetryDelay spaces retries while the DSP service is still coming up.
	volumeRetryDelay = 5 * time.Second
)

func newDSPVolumeController(socket string) (*dspVolumeController, error) {
	if socket == "" {
		return nil, errors.New("DSP control socket is required for volume")
	}
	return &dspVolumeController{
		socket: socket,
		volume: defaultVolume,
		notify: make(chan struct{}, 1),
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

// requestPushLocked wakes the worker. The channel carries a bare notification
// rather than a level, so a pending wake always means "state changed" and the
// worker reads whatever the level is when it gets there. An earlier version
// queued the level itself and could strand the newest one: a send that found
// the channel full, then lost the drain race to the worker, returned without
// queueing anything and left the DSP on the previous value, including a
// stranded unmute that would have held it at zero.
func (controller *dspVolumeController) requestPushLocked() {
	if controller.notify == nil {
		return
	}
	select {
	case controller.notify <- struct{}{}:
	default:
	}
}

// RequestPush asks the worker to reassert the current level.
func (controller *dspVolumeController) RequestPush() {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.requestPushLocked()
}

// effectiveLevel is what the DSP should be playing at right now.
func (controller *dspVolumeController) effectiveLevel() int {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.muted {
		return 0
	}
	return controller.volume
}

// Run keeps the DSP at the level this controller holds, until the context is
// cancelled.
func (controller *dspVolumeController) Run(ctx context.Context) {
	if controller.notify == nil {
		return
	}
	// The DSP powers up at its own gain and nothing else corrects it, so the
	// restored or default level has to be asserted rather than waited for.
	// Without this the speaker played its first stream at full output however
	// low the configured level was.
	controller.RequestPush()

	var retry <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-controller.notify:
		case <-retry:
		}
		if ctx.Err() != nil {
			return
		}
		retry = nil

		level := controller.effectiveLevel()
		send := controller.push
		if send == nil {
			send = controller.pushVolume
		}
		request, cancel := context.WithTimeout(ctx, volumePushTimeout)
		err := send(request, level)
		cancel()
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if controller.logf != nil {
			controller.logf("apply DSP volume %d: %v", level, err)
		}
		// The DSP service registers its procedures after this one starts, so
		// the first assertion can lose a race nobody can hear. Keep trying;
		// each attempt re-reads the level, so a retry cannot apply a stale one.
		retry = time.After(volumeRetryDelay)
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
