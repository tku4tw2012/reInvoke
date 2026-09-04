// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestSelectBlueALSAPCMUsesAllowlistedPeer(t *testing.T) {
	output := "/org/bluealsa/hci0/dev_AA_BB_CC_DD_EE_FF/a2dpsnk/source\n" +
		"/org/bluealsa/hci0/dev_AA_BB_CC_11_22_33/a2dpsnk/source\n"
	path, err := selectBlueALSAPCM(output, "aa:bb:cc:11:22:33")
	if err != nil {
		t.Fatal(err)
	}
	want := "/org/bluealsa/hci0/dev_AA_BB_CC_11_22_33/a2dpsnk/source"
	if path != want {
		t.Fatalf("PCM path = %q, want %q", path, want)
	}
}

func TestParseBlueALSAVolumeRequiresSynchronizedChannels(t *testing.T) {
	volume, err := parseBlueALSAVolume("Volume: L: 64 R: 64\n")
	if err != nil || volume != 64 {
		t.Fatalf("volume = %d, error = %v", volume, err)
	}
	if _, err := parseBlueALSAVolume("Volume: L: 64 R: 65\n"); err == nil {
		t.Fatal("mismatched channel volumes were accepted")
	}
}

func TestBlueALSAControllerAppliesRotaryStep(t *testing.T) {
	var calls [][]string
	run := func(ctx context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string(nil), args...))
		switch args[0] {
		case "list-pcms":
			return []byte(
				"/org/bluealsa/hci0/dev_AA_BB_CC_11_22_33/a2dpsnk/source\n",
			), nil
		case "info":
			return []byte("Volume: L: 64 R: 64\nMuted: L: N R: N\n"), nil
		case "volume":
			return nil, nil
		default:
			return nil, errors.New("unexpected command")
		}
	}
	controller, err := newBlueALSAController(
		"bluealsa-cli",
		"aa:bb:cc:11:22:33",
		run,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Apply(
		context.Background(),
		inputEvent{Name: "volumeup", Step: "3"},
	); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"volume",
		"/org/bluealsa/hci0/dev_AA_BB_CC_11_22_33/a2dpsnk/source",
		"67",
		"67",
	}
	if !reflect.DeepEqual(calls[len(calls)-1], want) {
		t.Fatalf("volume call = %v, want %v", calls[len(calls)-1], want)
	}
}

func TestParseBlueALSAMutedRequiresSynchronizedChannels(t *testing.T) {
	muted, err := parseBlueALSAMuted("Muted: L: Y R: Y\n")
	if err != nil || !muted {
		t.Fatalf("muted = %t, error = %v", muted, err)
	}
	if _, err := parseBlueALSAMuted("Muted: L: Y R: N\n"); err == nil {
		t.Fatal("mismatched channel mute state was accepted")
	}
}
