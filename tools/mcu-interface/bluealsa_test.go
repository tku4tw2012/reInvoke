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

func TestBlueALSAControllerRejectsCancelledSessionBeforeHardware(t *testing.T) {
	var calls int
	controller, err := newBlueALSAController(
		"bluealsa-cli",
		"aa:bb:cc:11:22:33",
		func(context.Context, ...string) ([]byte, error) {
			calls++
			return nil, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := controller.SetVolume(ctx, 50); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("SetVolume error = %v, want context cancellation", err)
	}
	if calls != 0 {
		t.Fatalf("cancelled session made %d hardware calls", calls)
	}
}

func TestBlueALSASetVolumeClampsToRecoveredRange(t *testing.T) {
	var calls [][]string
	controller, err := newBlueALSAController(
		"bluealsa-cli",
		"aa:bb:cc:11:22:33",
		func(_ context.Context, args ...string) ([]byte, error) {
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
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := controller.SetVolume(context.Background(), -1)
	if err != nil || snapshot.Volume != 0 {
		t.Fatalf("negative volume snapshot = %#v, error = %v", snapshot, err)
	}
	if got := calls[len(calls)-1][2]; got != "0" {
		t.Fatalf("negative volume raw value = %q, want 0", got)
	}
	snapshot, err = controller.SetVolume(context.Background(), 101)
	if err != nil || snapshot.Volume != 100 {
		t.Fatalf("high volume snapshot = %#v, error = %v", snapshot, err)
	}
	if got := calls[len(calls)-1][2]; got != "127" {
		t.Fatalf("high volume raw value = %q, want 127", got)
	}
}

func TestAdjustVolumeSaturatesWithoutOverflow(t *testing.T) {
	maximum := int(^uint(0) >> 1)
	minimum := -maximum - 1
	for _, test := range []struct {
		name    string
		current int
		delta   int
		want    int
	}{
		{name: "maximum positive", current: 50, delta: maximum, want: 100},
		{name: "maximum negative", current: 50, delta: minimum, want: 0},
		{name: "ordinary positive", current: 50, delta: 25, want: 75},
		{name: "ordinary negative", current: 50, delta: -25, want: 25},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := adjustVolume(test.current, test.delta); got != test.want {
				t.Fatalf("adjusted volume = %d, want %d", got, test.want)
			}
		})
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

func TestBlueALSAControllerCachesPCMPathForLowLatency(t *testing.T) {
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
	// Initial snapshot primes cache (list-pcms + info)
	if _, err := controller.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	initialCallCount := len(calls)
	if initialCallCount != 2 {
		t.Fatalf("expected 2 calls for initial snapshot, got %d", initialCallCount)
	}
	// Subsequent rotary step should ONLY execute "volume", not "list-pcms" or "info"
	if err := controller.Apply(
		context.Background(),
		inputEvent{Name: "volumeup", Step: "2"},
	); err != nil {
		t.Fatal(err)
	}
	if len(calls) != initialCallCount+1 {
		t.Fatalf("expected exactly 1 call for cached volume step, got %d", len(calls)-initialCallCount)
	}
	if calls[len(calls)-1][0] != "volume" {
		t.Fatalf("expected volume command, got %s", calls[len(calls)-1][0])
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
