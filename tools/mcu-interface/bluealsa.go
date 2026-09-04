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

type commandRunner func(context.Context, ...string) ([]byte, error)

type blueALSAController struct {
	command string
	peer    string
	run     commandRunner
	mu      sync.Mutex
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
	controller := &blueALSAController{command: command, peer: normalized, run: run}
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
	if event.Name == "micmute" {
		_, err := controller.ToggleMuted(ctx)
		return err
	}
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
	return snapshot, nil
}

func (controller *blueALSAController) Snapshot(
	ctx context.Context,
) (blueALSASnapshot, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.snapshotLocked(ctx)
}

func (controller *blueALSAController) SetVolume(
	ctx context.Context,
	percent int,
) (blueALSASnapshot, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if percent < 0 || percent > 100 {
		return blueALSASnapshot{}, errors.New("volume must be from 0 through 100")
	}
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
	snapshot.Volume = percent
	return snapshot, nil
}

func (controller *blueALSAController) AdjustVolume(
	ctx context.Context,
	delta int,
) (blueALSASnapshot, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	pcmPath, snapshot, err := controller.pcmSnapshotLocked(ctx)
	if err != nil {
		return blueALSASnapshot{}, err
	}
	percent := snapshot.Volume + delta
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
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
	snapshot.Volume = percent
	return snapshot, nil
}

func (controller *blueALSAController) SetMuted(
	ctx context.Context,
	muted bool,
) (blueALSASnapshot, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
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
	snapshot.Muted = muted
	return snapshot, nil
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
