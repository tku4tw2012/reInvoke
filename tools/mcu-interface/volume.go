// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
)

// dspVolumeController owns the speaker's volume and mute state.
//
// This replaces a controller that shelled out to bluealsa-cli. BlueALSA is the
// BlueZ ALSA bridge, and this runtime replaced BlueZ with the donor Bluedroid
// stack, so that binary is not shipped and every volume request failed with
// "fork/exec /opt/reinvoke/bin/bluealsa-cli: no such file or directory".
//
// Volume now goes where the sound actually is: the DSP, over the same control
// socket the microphone mute already uses.
type dspVolumeController struct {
	socket string

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

// defaultVolume is where the speaker starts before anything sets a level.
const defaultVolume = 30

func newDSPVolumeController(socket string) (*dspVolumeController, error) {
	if socket == "" {
		return nil, errors.New("DSP control socket is required for volume")
	}
	return &dspVolumeController{socket: socket, volume: defaultVolume}, nil
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

// applyLocked pushes the effective level to the DSP. Mute is sent as zero
// rather than tracked separately on the hardware, so that unmuting restores
// the level the user had chosen.
func (controller *dspVolumeController) applyLocked(ctx context.Context) error {
	level := controller.volume
	if controller.muted {
		level = 0
	}
	if err := setDSPVolume(ctx, controller.socket, level); err != nil {
		return fmt.Errorf("apply volume: %w", err)
	}
	// The level is remembered only when unmuted, so unmuting restores what the
	// user chose rather than the zero that mute sends to the hardware.
	if controller.muted {
		return nil
	}
	return controller.rememberMusicVolume(controller.volume)
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
