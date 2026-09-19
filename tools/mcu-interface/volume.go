// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
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

	// ring draws the volume arc on the LED ring. The microcontroller renders
	// it from the level; nothing here draws segments.
	ring interface{ ShowVolume(int) error }

	// softvol is the ALSA control that actually carries the user volume. The
	// DSP keeps whatever gain it booted with; attenuating there instead cost a
	// forked process per rotary detent and stuttered during playback.
	softvol softvolWriter

	mu     sync.Mutex
	volume int
	muted  bool
	// duck holds the strongest attenuation any caller has asked for. The donor
	// kept a map of named duck requests so that a voice prompt and an alert
	// could overlap without either one restoring full volume while the other
	// was still speaking; the same applies here even with one caller.
	ducks map[string]duckState

	// applied closes once the DSP has accepted a level. A cue rendered before
	// that plays into whatever gain the DSP powered up with, which on this
	// unit was inaudible: the amplifier and DAC were open, the samples were
	// scaled correctly, and nothing came out.
	appliedOnce sync.Once
	applied     chan struct{}

	// musicStatePath, when set, remembers the chosen level across restarts.
	musicStatePath string
	savedVolume    int
	hasSavedVolume bool
}

type volumeSnapshot struct {
	Volume int
	Muted  bool
	// Duck reports the attenuation in force, for callers that want to know why
	// the level they set is not the level being played.
	Duck string
}

// duckState is the donor's aui::DuckState. The donor attenuated rather than
// muted, and distinguished a partial duck from a near-silent one, which is why
// a voice prompt left music faintly audible underneath.
type duckState int

const (
	duckNone duckState = iota
	duckSoft
	duckHard
)

// duckScale is the proportion of the chosen level that survives each duck.
// The donor's exact ratios are not recorded in any file recovered from this
// unit, so these are this project's values and are labelled as such rather
// than presented as the vendor's.
var duckScale = map[duckState]int{
	duckNone: 100,
	duckSoft: 40,
	duckHard: 10,
}

func parseDuckState(name string) (duckState, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "none", "off":
		return duckNone, nil
	case "soft":
		return duckSoft, nil
	case "hard":
		return duckHard, nil
	}
	return duckNone, fmt.Errorf("unknown duck state %q", name)
}

func (state duckState) String() string {
	switch state {
	case duckSoft:
		return "soft"
	case duckHard:
		return "hard"
	}
	return "none"
}

// defaultVolume is the vendor's own starting level, recovered from its
// settings store rather than guessed: caldata/FENV.bin id 0x26, named
// current_volume in LibreEnv's table, holds 80. Earlier candidates guessed
// here because the level was being applied to DSP gain, where the comfortable
// point was around 3; on the softvol control this is a percentage of the
// vendor's own scale.
// defaultVolume is the level a unit starts at with nothing stored.
//
// Measured on this unit, 2026-09-19, by playing an unattenuated cue through
// the DSP and asking the listener: gain 3 was slightly quiet and gain 5 was
// right. That is the level music plays at, because music reaches the DSP
// without the attenuation the cue player applies to its own files.
//
// It was 80 for several releases, which was correct only for a control that
// no longer exists. Candidate d75dccf adopted the vendor's own current_volume
// of 80 while volume rode an ALSA softvol control, where the number is a
// percentage of the vendor's scale. That commit said so plainly: "the
// comfortable point was near 3" when the level went to DSP gain instead.
// Softvol was later found to be absent on this runtime and disabled, which
// put the level back on DSP gain without anyone moving the number back.
const defaultVolume = 5

const (
	// volumePushTimeout bounds one call to the DSP service.
	volumePushTimeout = 3 * time.Second
	// volumeRetryDelay spaces retries while the DSP service is still coming up.
	volumeRetryDelay = 5 * time.Second

	// The donor faded its softvol on a periodic tick rather than jumping to a
	// new level, which is why its volume changes were smooth. A softvol write
	// is an ioctl on an open descriptor, so a tick this short is cheap; the
	// step is in control units, not percent, because the control is 0..255.
	volumeFadeInterval = 20 * time.Millisecond
	volumeFadeStep     = 6
)

func newDSPVolumeController(socket string) (*dspVolumeController, error) {
	if socket == "" {
		return nil, errors.New("DSP control socket is required for volume")
	}
	return &dspVolumeController{
		socket:  socket,
		volume:  defaultVolume,
		notify:  make(chan struct{}, 1),
		applied: make(chan struct{}),
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
	return controller.effectiveLevelLocked()
}

// displayLevel is what the ring should show: the chosen level, or nothing when
// muted. It deliberately ignores ducking.
func (controller *dspVolumeController) displayLevel() int {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.muted {
		return 0
	}
	return controller.volume
}

func (controller *dspVolumeController) effectiveLevelLocked() int {
	if controller.muted {
		return 0
	}
	return controller.volume * duckScale[controller.strongestDuckLocked()] / 100
}

// strongestDuckLocked reports the deepest attenuation currently requested, so
// releasing one duck while another is still held does not restore full volume.
func (controller *dspVolumeController) strongestDuckLocked() duckState {
	strongest := duckNone
	for _, state := range controller.ducks {
		if state > strongest {
			strongest = state
		}
	}
	return strongest
}

// SetDuck records or clears one named duck request and returns the resulting
// state. Clearing is asking for "none", which is how the donor released one.
func (controller *dspVolumeController) SetDuck(
	ctx context.Context,
	name string,
	state duckState,
) (volumeSnapshot, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return volumeSnapshot{}, errors.New("duck name is required")
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.ducks == nil {
		controller.ducks = map[string]duckState{}
	}
	if state == duckNone {
		delete(controller.ducks, name)
	} else {
		controller.ducks[name] = state
	}
	if err := controller.applyLocked(ctx); err != nil {
		return controller.snapshotLocked(), err
	}
	return controller.snapshotLocked(), nil
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

	// The ring already shows the startup level as far as the listener is
	// concerned: nothing has changed yet. Seeding it here rather than at the
	// first apply means a dial turn made before the DSP is ready still draws,
	// because that level differs from this one.
	drawn := controller.displayLevel()

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
		// The DSP call carries the level the listener hears.
		//
		// The donor faded a softvol control instead, in
		// aui::VolumeManager::softvol_fading_tick. That control does not exist
		// on this runtime: nothing defines a softvol plugin, and the default
		// this code shipped pointed at card 0, which is the Loopback device.
		// So the fade failed on every change and logged while the DSP call did
		// the work. The fade is left here, disabled, for whoever ships a real
		// softvol plugin; until then it is off rather than failing.
		if err := controller.fadeToTarget(ctx, softvolForPercent(level)); err != nil {
			if ctx.Err() != nil {
				return
			}
			if controller.logf != nil {
				controller.logf("fade softvol to %d: %v", level, err)
			}
		}
		send := controller.push
		if send == nil {
			send = controller.pushVolume
		}
		request, cancel := context.WithTimeout(ctx, volumePushTimeout)
		err := send(request, level)
		cancel()
		// The ring is written only after the DSP call returns. Both devices
		// sit on one I2C bus, and writing while the DSP transaction this call
		// triggered is still in flight cost arbitration losses that the retry
		// loop then had to absorb. Observed on hardware as "lost arbitration"
		// bursts during a volume sweep.
		//
		// The arc shows the level the listener chose, not the ducked one: a
		// duck is a transient from a prompt speaking over music, and redrawing
		// for it would make the ring flicker on every notification.
		//
		// The ring is written once per volume change, matching the donor.
		//
		// The donor drew it once per change: its mcu-interface subscribed to
		// com.harman.volumeChanged, published by audio-ui, and its handler
		// ("Receive volume change notify event: %d!") range-checked the level
		// and wrote opcode 0x03. It never applied volume itself, so it had no
		// retries and no failures to draw.
		//
		// This runtime does apply volume, and the DSP registers its procedures
		// seconds after this service starts, so the opening attempts fail.
		// Writing the arc on those too lit the ring once per lost race, which
		// made the number of illuminations at boot a readout of how slow the
		// DSP had been. Confirmed on this unit at one illumination per apply,
		// four applies to four rings.
		if err == nil {
			if shown := controller.displayLevel(); shown != drawn {
				if controller.ring != nil {
					// Logged on the way out, not only on failure. A line
					// that appears only when the write errors cannot tell
					// a ring that was drawn from one that was skipped, and
					// that difference is the whole behaviour here.
					if ringErr := controller.ring.ShowVolume(
						shown,
					); ringErr != nil {
						if controller.logf != nil {
							controller.logf("show volume on ring: %v", ringErr)
						}
					} else if controller.logf != nil {
						controller.logf("RING_ARC drawn at %d", shown)
					}
				}
				drawn = shown
			}
			controller.markApplied()
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if controller.logf != nil {
			controller.logf("apply DSP volume %d: %v", level, err)
		}
		// The DSP service registers its procedures after this one starts, so
		// the first assertions lose a race nobody can hear. Keep trying; each
		// attempt re-reads the level, so a retry cannot apply a stale one.
		//
		// Retrying until it works, rather than a fixed number of times, is the
		// point. On this unit the DSP finished registering about fifty seconds
		// into a boot while the earlier retry budget ran out at forty-six, so
		// the level was never applied at all: every service reported volume 80
		// while the DSP sat at whatever gain it powered up with, and the boot
		// cue played into it inaudibly.
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
	return volumeSnapshot{
		Volume: controller.volume,
		Muted:  controller.muted,
		Duck:   controller.strongestDuckLocked().String(),
	}
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

// softvolWriter is the ALSA control the user volume rides on.
type softvolWriter interface {
	Read() (int, error)
	Write(int) error
}

// fadeToTarget walks the softvol control toward the requested level instead of
// jumping, matching aui::VolumeManager::softvol_fading_tick. Jumping is what
// made a rotary sweep audible as a stutter: fourteen changes in seven seconds,
// two of which passed through zero and silenced playback outright.
func (controller *dspVolumeController) fadeToTarget(
	ctx context.Context,
	target int,
) error {
	if controller.softvol == nil {
		return nil
	}
	current, err := controller.softvol.Read()
	if err != nil {
		return err
	}
	for current != target {
		if err := ctx.Err(); err != nil {
			return err
		}
		if current < target {
			current += volumeFadeStep
			if current > target {
				current = target
			}
		} else {
			current -= volumeFadeStep
			if current < target {
				current = target
			}
		}
		if err := controller.softvol.Write(current); err != nil {
			return err
		}
		if current == target {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(volumeFadeInterval):
		}
	}
	return nil
}

// markApplied records that the DSP has accepted a level.
func (controller *dspVolumeController) markApplied() {
	controller.appliedOnce.Do(func() {
		if controller.applied != nil {
			close(controller.applied)
		}
	})
}

// WaitApplied blocks until the DSP has accepted a level, or the deadline
// passes. It reports whether the level was applied.
//
// A sound rendered before this returns true goes through a DSP still at its
// power-on gain. On this unit that was silent even with the amplifier and DAC
// open and the samples scaled correctly, and it looked like a working cue in
// every log.
func (controller *dspVolumeController) WaitApplied(
	ctx context.Context,
	timeout time.Duration,
) bool {
	if controller == nil || controller.applied == nil {
		return false
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-controller.applied:
		return true
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}

// dspReadyTimeout bounds how long a startup cue waits for the audio path.
//
// The DSP registered about fifty seconds into a boot on this unit, measured
// from its own log. Ninety seconds leaves room for a slower boot without
// holding a cue for a DSP that is never coming.
const dspReadyTimeout = 90 * time.Second
