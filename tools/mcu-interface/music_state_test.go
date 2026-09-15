// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMusicVolumeStateRoundTripAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "music-volume")
	if value, err := readMusicVolume(path); err != nil || value != defaultVolume {
		t.Fatal("missing preference did not preserve safe default")
	}
	controller := &dspVolumeController{musicStatePath: path}
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
	controller := &dspVolumeController{
		musicStatePath: path, volume: 7,
		socket: newStubDSPSocket(t),
	}
	if _, err := controller.SetVolume(context.Background(), 9); err != nil {
		t.Fatal(err)
	}
	if value, err := readMusicVolume(path); err != nil || value != 9 {
		t.Fatal("successful control not retained")
	}
	// While muted the effective output is zero, and the saved preference must
	// keep tracking what the user chooses so unmuting restores it. Writing the
	// muted value instead would silence the speaker across a restart.
	if _, err := controller.SetMuted(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.SetVolume(context.Background(), 21); err != nil {
		t.Fatal(err)
	}
	state, err := controller.SetMuted(context.Background(), false)
	if err != nil || state.Volume != 21 || state.Muted {
		t.Fatalf("unmute did not restore the chosen level: %+v %v", state, err)
	}
	if value, err := readMusicVolume(path); err != nil || value != 21 {
		t.Fatalf("saved preference is %v, want 21", value)
	}
}

func TestUserVolumeZeroIsDistinctFromTransportMute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "music-volume")
	controller := &dspVolumeController{
		musicStatePath: path, volume: 7,
		socket: newStubDSPSocket(t),
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
