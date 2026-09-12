// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMusicVolumeStateRoundTripAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "music-volume")
	if value, err := readMusicVolume(path); err != nil || value != defaultConnectCeiling {
		t.Fatal("missing preference did not preserve safe default")
	}
	controller := &blueALSAController{musicStatePath: path}
	if err := controller.rememberMusicVolume(7); err != nil {
		t.Fatal(err)
	}
	if value, err := readMusicVolume(path); err != nil || value != 7 {
		t.Fatal("music preference not retained")
	}
	for _, invalid := range []string{"101\n", "-1\n", "7", "07\n", "invalid"} {
		if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readMusicVolume(path); err == nil {
			t.Fatal("invalid music preference accepted")
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(filepath.Dir(path), "other"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := readMusicVolume(path); err == nil {
		t.Fatal("music preference symlink accepted")
	}
}

func TestMusicVolumeSavedOnlyAfterSuccessfulControl(t *testing.T) {
	path := filepath.Join(t.TempDir(), "music-volume")
	controller := &blueALSAController{
		musicStatePath: path, cachedValid: true, cachedVolume: 7, cachedPath: "synthetic-pcm",
		run: func(context.Context, ...string) ([]byte, error) { return nil, nil },
	}
	if _, err := controller.SetVolume(context.Background(), 9); err != nil {
		t.Fatal(err)
	}
	if value, err := readMusicVolume(path); err != nil || value != 9 {
		t.Fatal("successful control not retained")
	}
	controller.run = func(context.Context, ...string) ([]byte, error) { return nil, errors.New("synthetic failure") }
	if _, err := controller.SetVolume(context.Background(), 20); err == nil {
		t.Fatal("failed volume control accepted")
	}
	if value, err := readMusicVolume(path); err != nil || value != 9 {
		t.Fatal("failed volume control overwrote preference")
	}
}

func TestSavedMusicVolumeNeverBypassesSafeCeiling(t *testing.T) {
	for _, saved := range []int{0, 7, 90} {
		var written string
		controller := &blueALSAController{
			peer: "02:00:00:00:00:02", connectCeiling: defaultConnectCeiling,
			hasSavedVolume: true, savedVolume: saved,
			run: func(_ context.Context, args ...string) ([]byte, error) {
				switch args[0] {
				case "list-pcms":
					return []byte("/org/bluealsa/hci0/dev_02_00_00_00_00_02/a2dpsnk/source\n"), nil
				case "info":
					return []byte("Volume: 127\nMuted: N\n"), nil
				case "volume":
					written = strings.Join(args[2:], ",")
					return nil, nil
				}
				return nil, errors.New("unexpected command")
			},
		}
		snapshot, lowered, err := controller.EnforceConnectCeiling(context.Background())
		if err != nil || !lowered || written == "" || snapshot.Muted || snapshot.Volume > defaultConnectCeiling ||
			(saved <= defaultConnectCeiling && snapshot.Volume != saved) {
			t.Fatal("saved preference bypassed safe ceiling or lost silence")
		}
	}
}

func TestUserVolumeZeroIsDistinctFromTransportMute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "music-volume")
	controller := &blueALSAController{
		musicStatePath: path, cachedValid: true, cachedVolume: 7, cachedPath: "synthetic-pcm",
		run: func(context.Context, ...string) ([]byte, error) { return nil, nil },
	}
	zero, err := controller.SetVolume(context.Background(), 0)
	if err != nil || zero.Volume != 0 || zero.Muted {
		t.Fatal("explicit user zero was conflated with mute")
	}
	for _, muted := range []bool{true, false} {
		state, err := controller.SetMuted(context.Background(), muted)
		if err != nil || state.Volume != 0 || state.Muted != muted {
			t.Fatal("mute toggling changed the user's zero volume")
		}
		if saved, err := readMusicVolume(path); err != nil || saved != 0 {
			t.Fatal("mute toggling overwrote the saved volume preference")
		}
	}
}
