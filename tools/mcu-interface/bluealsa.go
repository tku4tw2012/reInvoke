// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

var errBlueALSAPCMUnavailable = errors.New("BlueALSA PCM is unavailable")

const blueALSACommandTimeout = 3 * time.Second

// defaultConnectCeiling keeps a newly connected peer at a comfortable level.
// The scale is linear in amplitude rather than perceptual, so this is much
// lower than an intuitive "percent" reading would suggest.
const defaultConnectCeiling = 12

// BlueALSA publishes a transport's PCM some time after the peer connects, and
// the rear-indicator state masks connection while pairing, so the ceiling is
// driven by polling for the PCM itself.
const connectCeilingRetryInterval = 250 * time.Millisecond

// runConnectCeilingWatcher polls for a new transport and caps it. Polling is
// used instead of the rear-indicator state because that state reports
// "pairing" while a freshly paired phone is already streaming, which is exactly
// the case that must not play at maximum volume.
func runConnectCeilingWatcher(
	ctx context.Context,
	enforce func(context.Context) (blueALSASnapshot, bool, error),
	sleep func(context.Context, time.Duration) error,
	logf func(string, ...interface{}),
) error {
	var lastFailure string
	for {
		snapshot, lowered, err := enforce(ctx)
		switch {
		case err == nil:
			lastFailure = ""
			if lowered && logf != nil {
				logf("lowered new transport volume to %d", snapshot.Volume)
			}
		case errors.Is(err, context.Canceled),
			errors.Is(err, context.DeadlineExceeded):
			return nil
		case errors.Is(err, errBlueALSAPCMUnavailable):
			// No peer connected. This is the normal idle state.
			lastFailure = ""
		default:
			if logf != nil && err.Error() != lastFailure {
				logf("connect volume ceiling: %v", err)
				lastFailure = err.Error()
			}
		}
		if sleepErr := sleep(ctx, connectCeilingRetryInterval); sleepErr != nil {
			return nil
		}
	}
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type commandRunner func(context.Context, ...string) ([]byte, error)

type blueALSAController struct {
	command string
	peer    string
	run     commandRunner
	mu      sync.Mutex

	cachedPath   string
	cachedVolume int
	cachedMuted  bool
	cachedValid  bool

	// connectCeiling is the highest volume a freshly acquired transport may
	// keep. BlueALSA starts a new PCM at maximum, so without this a phone that
	// simply connects plays at full output.
	connectCeiling int
	// ceilingPath is the PCM the ceiling was last successfully applied to. It
	// identifies the transport generation so the same one is not re-lowered
	// while the operator raises the knob.
	ceilingPath string
}

type blueALSASnapshot struct {
	Volume int
	Muted  bool
}

func newBlueALSAController(
	command, peer string,
	run commandRunner,
) (*blueALSAController, error) {
	normalized := strings.ToUpper(peer)
	parts := strings.Split(normalized, ":")
	if len(parts) != 6 {
		return nil, errors.New("BlueALSA peer must be a Bluetooth address")
	}
	for _, part := range parts {
		if len(part) != 2 {
			return nil, errors.New("BlueALSA peer must be a Bluetooth address")
		}
		if _, err := strconv.ParseUint(part, 16, 8); err != nil {
			return nil, errors.New("BlueALSA peer must be a Bluetooth address")
		}
	}
	controller := &blueALSAController{
		command:        command,
		peer:           normalized,
		run:            run,
		connectCeiling: defaultConnectCeiling,
	}
	if controller.run == nil {
		controller.run = controller.runCommand
	}
	return controller, nil
}

func (controller *blueALSAController) runCommand(
	ctx context.Context,
	args ...string,
) ([]byte, error) {
	commandContext, cancel := context.WithTimeout(ctx, blueALSACommandTimeout)
	defer cancel()
	command := exec.CommandContext(commandContext, controller.command, args...)
	command.Env = os.Environ()
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf(
			"bluealsa-cli %s: %w: %s",
			args[0],
			err,
			strings.TrimSpace(string(output)),
		)
	}
	return output, nil
}

func (controller *blueALSAController) Apply(
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

func (controller *blueALSAController) ToggleMuted(
	ctx context.Context,
) (blueALSASnapshot, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return blueALSASnapshot{}, err
	}
	if controller.cachedValid {
		newMuted := !controller.cachedMuted
		value := "n"
		if newMuted {
			value = "y"
		}
		if _, err := controller.run(
			ctx,
			"mute",
			controller.cachedPath,
			value,
			value,
		); err != nil {
			controller.cachedValid = false
			return controller.toggleMutedSlowLocked(ctx)
		}
		controller.cachedMuted = newMuted
		return blueALSASnapshot{Volume: controller.cachedVolume, Muted: newMuted}, nil
	}
	return controller.toggleMutedSlowLocked(ctx)
}

func (controller *blueALSAController) toggleMutedSlowLocked(
	ctx context.Context,
) (blueALSASnapshot, error) {
	pcmPath, snapshot, err := controller.pcmSnapshotLocked(ctx)
	if err != nil {
		return blueALSASnapshot{}, err
	}
	snapshot.Muted = !snapshot.Muted
	value := "n"
	if snapshot.Muted {
		value = "y"
	}
	if _, err := controller.run(
		ctx,
		"mute",
		pcmPath,
		value,
		value,
	); err != nil {
		return blueALSASnapshot{}, err
	}
	controller.cachedPath = pcmPath
	controller.cachedVolume = snapshot.Volume
	controller.cachedMuted = snapshot.Muted
	controller.cachedValid = true
	return snapshot, nil
}

func (controller *blueALSAController) Snapshot(
	ctx context.Context,
) (blueALSASnapshot, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return blueALSASnapshot{}, err
	}
	return controller.snapshotLocked(ctx)
}

func (controller *blueALSAController) SetVolume(
	ctx context.Context,
	percent int,
) (blueALSASnapshot, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return blueALSASnapshot{}, err
	}
	percent = clampVolume(percent)
	if controller.cachedValid {
		rawVolume := (percent*127 + 50) / 100
		value := strconv.Itoa(rawVolume)
		if _, err := controller.run(
			ctx,
			"volume",
			controller.cachedPath,
			value,
			value,
		); err != nil {
			controller.cachedValid = false
			return controller.setVolumeSlowLocked(ctx, percent)
		}
		controller.cachedVolume = percent
		return blueALSASnapshot{Volume: percent, Muted: controller.cachedMuted}, nil
	}
	return controller.setVolumeSlowLocked(ctx, percent)
}

func (controller *blueALSAController) setVolumeSlowLocked(
	ctx context.Context,
	percent int,
) (blueALSASnapshot, error) {
	pcmPath, snapshot, err := controller.pcmSnapshotLocked(ctx)
	if err != nil {
		return blueALSASnapshot{}, err
	}
	rawVolume := (percent*127 + 50) / 100
	value := strconv.Itoa(rawVolume)
	if _, err := controller.run(
		ctx,
		"volume",
		pcmPath,
		value,
		value,
	); err != nil {
		return blueALSASnapshot{}, err
	}
	controller.cachedPath = pcmPath
	controller.cachedVolume = percent
	controller.cachedMuted = snapshot.Muted
	controller.cachedValid = true
	snapshot.Volume = percent
	return snapshot, nil
}

func (controller *blueALSAController) AdjustVolume(
	ctx context.Context,
	delta int,
) (blueALSASnapshot, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return blueALSASnapshot{}, err
	}
	if controller.cachedValid {
		percent := adjustVolume(controller.cachedVolume, delta)
		if percent == controller.cachedVolume {
			return blueALSASnapshot{Volume: percent, Muted: controller.cachedMuted}, nil
		}
		rawVolume := (percent*127 + 50) / 100
		value := strconv.Itoa(rawVolume)
		if _, err := controller.run(
			ctx,
			"volume",
			controller.cachedPath,
			value,
			value,
		); err != nil {
			controller.cachedValid = false
			return controller.adjustVolumeSlowLocked(ctx, delta)
		}
		controller.cachedVolume = percent
		return blueALSASnapshot{Volume: percent, Muted: controller.cachedMuted}, nil
	}
	return controller.adjustVolumeSlowLocked(ctx, delta)
}

func (controller *blueALSAController) adjustVolumeSlowLocked(
	ctx context.Context,
	delta int,
) (blueALSASnapshot, error) {
	pcmPath, snapshot, err := controller.pcmSnapshotLocked(ctx)
	if err != nil {
		return blueALSASnapshot{}, err
	}
	percent := adjustVolume(snapshot.Volume, delta)
	rawVolume := (percent*127 + 50) / 100
	value := strconv.Itoa(rawVolume)
	if _, err := controller.run(
		ctx,
		"volume",
		pcmPath,
		value,
		value,
	); err != nil {
		return blueALSASnapshot{}, err
	}
	controller.cachedPath = pcmPath
	controller.cachedVolume = percent
	controller.cachedMuted = snapshot.Muted
	controller.cachedValid = true
	snapshot.Volume = percent
	return snapshot, nil
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

func adjustVolume(current int, delta int) int {
	current = clampVolume(current)
	if delta > 0 && delta >= 100-current {
		return 100
	}
	if delta < 0 && delta <= -current {
		return 0
	}
	return current + delta
}

func (controller *blueALSAController) SetMuted(
	ctx context.Context,
	muted bool,
) (blueALSASnapshot, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return blueALSASnapshot{}, err
	}
	if controller.cachedValid {
		value := "n"
		if muted {
			value = "y"
		}
		if _, err := controller.run(
			ctx,
			"mute",
			controller.cachedPath,
			value,
			value,
		); err != nil {
			controller.cachedValid = false
			return controller.setMutedSlowLocked(ctx, muted)
		}
		controller.cachedMuted = muted
		return blueALSASnapshot{Volume: controller.cachedVolume, Muted: muted}, nil
	}
	return controller.setMutedSlowLocked(ctx, muted)
}

func (controller *blueALSAController) setMutedSlowLocked(
	ctx context.Context,
	muted bool,
) (blueALSASnapshot, error) {
	pcmPath, snapshot, err := controller.pcmSnapshotLocked(ctx)
	if err != nil {
		return blueALSASnapshot{}, err
	}
	value := "n"
	if muted {
		value = "y"
	}
	if _, err := controller.run(
		ctx,
		"mute",
		pcmPath,
		value,
		value,
	); err != nil {
		return blueALSASnapshot{}, err
	}
	controller.cachedPath = pcmPath
	controller.cachedVolume = snapshot.Volume
	controller.cachedMuted = muted
	controller.cachedValid = true
	snapshot.Muted = muted
	return snapshot, nil
}

// EnforceConnectCeiling lowers a newly appeared transport to the safe ceiling.
//
// It keys off the BlueALSA PCM path rather than the rear-indicator state,
// because the indicator reports "pairing" while a freshly paired phone is
// already streaming, and because a restart or a missed poll must not lose the
// edge. Enforcement is recorded only after a successful write, so a transport
// that appears before BlueALSA publishes its PCM is retried rather than lost.
//
// It never raises a quieter peer, so a deliberate low level is preserved.
func (controller *blueALSAController) EnforceConnectCeiling(
	ctx context.Context,
) (blueALSASnapshot, bool, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return blueALSASnapshot{}, false, err
	}
	ceiling := controller.connectCeiling
	if ceiling <= 0 || ceiling > 100 {
		ceiling = defaultConnectCeiling
	}
	pcmPath, snapshot, err := controller.pcmSnapshotLocked(ctx)
	if err != nil {
		return blueALSASnapshot{}, false, err
	}
	if pcmPath == controller.ceilingPath {
		return snapshot, false, nil
	}
	if snapshot.Volume <= ceiling {
		controller.ceilingPath = pcmPath
		return snapshot, false, nil
	}
	// Write against the path just observed instead of taking a second
	// snapshot, so an external change cannot be read as a reason to raise.
	lowered, err := controller.writeVolumeLocked(ctx, pcmPath, ceiling)
	if err != nil {
		return blueALSASnapshot{}, false, err
	}
	controller.ceilingPath = pcmPath
	return lowered, true, nil
}

func (controller *blueALSAController) writeVolumeLocked(
	ctx context.Context,
	pcmPath string,
	percent int,
) (blueALSASnapshot, error) {
	rawVolume := (percent*127 + 50) / 100
	value := strconv.Itoa(rawVolume)
	if _, err := controller.run(ctx, "volume", pcmPath, value, value); err != nil {
		controller.cachedValid = false
		return blueALSASnapshot{}, err
	}
	controller.cachedPath = pcmPath
	controller.cachedVolume = percent
	controller.cachedValid = true
	return blueALSASnapshot{Volume: percent, Muted: controller.cachedMuted}, nil
}

func (controller *blueALSAController) snapshotLocked(
	ctx context.Context,
) (blueALSASnapshot, error) {
	_, snapshot, err := controller.pcmSnapshotLocked(ctx)
	return snapshot, err
}

func (controller *blueALSAController) pcmSnapshotLocked(
	ctx context.Context,
) (string, blueALSASnapshot, error) {
	output, err := controller.run(ctx, "list-pcms")
	if err != nil {
		return "", blueALSASnapshot{}, err
	}
	pcmPath, err := selectBlueALSAPCM(string(output), controller.peer)
	if err != nil {
		return "", blueALSASnapshot{}, err
	}
	output, err = controller.run(ctx, "info", pcmPath)
	if err != nil {
		return "", blueALSASnapshot{}, err
	}
	rawVolume, err := parseBlueALSAVolume(string(output))
	if err != nil {
		return "", blueALSASnapshot{}, err
	}
	percent := (rawVolume*100 + 63) / 127
	muted, err := parseBlueALSAMuted(string(output))
	if err != nil {
		return "", blueALSASnapshot{}, err
	}
	controller.cachedPath = pcmPath
	controller.cachedVolume = percent
	controller.cachedMuted = muted
	controller.cachedValid = true
	return pcmPath, blueALSASnapshot{Volume: percent, Muted: muted}, nil
}

func selectBlueALSAPCM(output, peer string) (string, error) {
	peerToken := "DEV_" + strings.ReplaceAll(strings.ToUpper(peer), ":", "_")
	for _, line := range strings.Split(output, "\n") {
		path := strings.TrimSpace(line)
		if strings.HasPrefix(path, "/org/bluealsa/") &&
			strings.Contains(strings.ToUpper(path), peerToken) {
			return path, nil
		}
	}
	return "", errBlueALSAPCMUnavailable
}

func parseBlueALSAVolume(output string) (int, error) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 5 &&
			fields[0] == "Volume:" &&
			fields[1] == "L:" &&
			fields[3] == "R:" {
			left, leftErr := strconv.Atoi(fields[2])
			right, rightErr := strconv.Atoi(fields[4])
			if leftErr != nil || rightErr != nil ||
				left != right || left < 0 || left > 127 {
				return 0, errors.New("invalid BlueALSA stereo volume")
			}
			return left, nil
		}
		if len(fields) == 2 && fields[0] == "Volume:" {
			value, err := strconv.Atoi(fields[1])
			if err != nil || value < 0 || value > 127 {
				return 0, errors.New("invalid BlueALSA volume")
			}
			return value, nil
		}
	}
	return 0, errors.New("BlueALSA volume is missing")
}

func parseBlueALSAMuted(output string) (bool, error) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 5 &&
			fields[0] == "Muted:" &&
			fields[1] == "L:" &&
			fields[3] == "R:" {
			if fields[2] != fields[4] ||
				(fields[2] != "Y" && fields[2] != "N") {
				return false, errors.New("invalid BlueALSA stereo mute")
			}
			return fields[2] == "Y", nil
		}
		if len(fields) == 2 && fields[0] == "Muted:" {
			if fields[1] != "Y" && fields[1] != "N" {
				return false, errors.New("invalid BlueALSA mute")
			}
			return fields[1] == "Y", nil
		}
	}
	return false, errors.New("BlueALSA mute is missing")
}
