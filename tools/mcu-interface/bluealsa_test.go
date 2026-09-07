// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
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

// newCeilingFixture builds a controller whose BlueALSA reports pcmPath at the
// given raw volume, recording every volume write.
func newCeilingFixture(
	t *testing.T,
	pcmPath func() string,
	rawVolume func() int,
	writes *[]string,
	infoCalls *int,
) *blueALSAController {
	t.Helper()
	controller, err := newBlueALSAController(
		"bluealsa-cli",
		"00:00:5E:00:53:01",
		func(_ context.Context, args ...string) ([]byte, error) {
			switch args[0] {
			case "list-pcms":
				return []byte(pcmPath() + "\n"), nil
			case "info":
				*infoCalls++
				return []byte(fmt.Sprintf(
					"Volume: L: %d R: %d\nMuted: L: N R: N\n",
					rawVolume(),
					rawVolume(),
				)), nil
			case "volume":
				*writes = append(*writes, args[2])
				return nil, nil
			}
			return nil, errors.New("unexpected command")
		},
	)
	if err != nil {
		t.Fatalf("new controller: %v", err)
	}
	return controller
}

// Connecting a phone must not play at maximum volume.
func TestEnforceConnectCeilingLowersANewTransport(t *testing.T) {
	var writes []string
	infoCalls := 0
	raw := 127
	path := "/org/bluealsa/hci0/dev_00_00_5E_00_53_01/a2dpsnk/sink"
	controller := newCeilingFixture(t,
		func() string { return path },
		func() int { return raw },
		&writes, &infoCalls)

	snapshot, lowered, err := controller.EnforceConnectCeiling(context.Background())
	if err != nil {
		t.Fatalf("enforce: %v", err)
	}
	if !lowered || snapshot.Volume != defaultConnectCeiling {
		t.Fatalf("snapshot = %+v lowered = %v", snapshot, lowered)
	}
	// 12 percent of the 0-127 BlueALSA scale, written once from a single read.
	if len(writes) != 1 || writes[0] != "15" {
		t.Fatalf("writes = %v, want one write of 15", writes)
	}
	if infoCalls != 1 {
		t.Fatalf("info calls = %d, want 1; a second read can raise volume",
			infoCalls)
	}
}

// The knob must win after the ceiling has been applied to a transport.
func TestEnforceConnectCeilingDoesNotFightTheOperator(t *testing.T) {
	var writes []string
	infoCalls := 0
	raw := 127
	path := "/org/bluealsa/hci0/dev_00_00_5E_00_53_01/a2dpsnk/sink"
	controller := newCeilingFixture(t,
		func() string { return path },
		func() int { return raw },
		&writes, &infoCalls)

	if _, _, err := controller.EnforceConnectCeiling(context.Background()); err != nil {
		t.Fatalf("first enforce: %v", err)
	}
	raw = 90 // operator turned it up afterwards
	_, lowered, err := controller.EnforceConnectCeiling(context.Background())
	if err != nil {
		t.Fatalf("second enforce: %v", err)
	}
	if lowered {
		t.Fatal("an already-capped transport must not be lowered again")
	}
	if len(writes) != 1 {
		t.Fatalf("writes = %v, want exactly one", writes)
	}
}

// A new transport that is already quiet must be recorded, never raised.
func TestEnforceConnectCeilingNeverRaisesAQuietTransport(t *testing.T) {
	var writes []string
	infoCalls := 0
	controller := newCeilingFixture(t,
		func() string { return "/org/bluealsa/hci0/dev_00_00_5E_00_53_01/a2dpsnk/sink" },
		func() int { return 6 },
		&writes, &infoCalls)

	snapshot, lowered, err := controller.EnforceConnectCeiling(context.Background())
	if err != nil {
		t.Fatalf("enforce: %v", err)
	}
	if lowered || len(writes) != 0 {
		t.Fatalf("quiet transport was written: lowered=%v writes=%v", lowered, writes)
	}
	if snapshot.Volume > defaultConnectCeiling {
		t.Fatalf("volume = %d", snapshot.Volume)
	}
}

// A reconnect creates a different PCM, which must be capped again even though
// the rear indicator may still be reporting "pairing".
func TestEnforceConnectCeilingCapsEachNewTransport(t *testing.T) {
	var writes []string
	infoCalls := 0
	raw := 127
	path := "/org/bluealsa/hci0/dev_00_00_5E_00_53_01/a2dpsnk/sink"
	controller := newCeilingFixture(t,
		func() string { return path },
		func() int { return raw },
		&writes, &infoCalls)

	if _, _, err := controller.EnforceConnectCeiling(context.Background()); err != nil {
		t.Fatalf("first enforce: %v", err)
	}
	path = "/org/bluealsa/hci0/dev_00_00_5E_00_53_01/a2dpsnk/sink2"
	raw = 127
	_, lowered, err := controller.EnforceConnectCeiling(context.Background())
	if err != nil {
		t.Fatalf("second enforce: %v", err)
	}
	if !lowered || len(writes) != 2 {
		t.Fatalf("new transport not capped: lowered=%v writes=%v", lowered, writes)
	}
}

func TestConnectCeilingWatcherToleratesIdleAndStopsOnCancellation(t *testing.T) {
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())
	var logged []string
	err := runConnectCeilingWatcher(
		ctx,
		func(context.Context) (blueALSASnapshot, bool, error) {
			calls++
			if calls >= 3 {
				cancel()
			}
			return blueALSASnapshot{}, false, errBlueALSAPCMUnavailable
		},
		func(c context.Context, _ time.Duration) error { return c.Err() },
		func(format string, args ...interface{}) {
			logged = append(logged, fmt.Sprintf(format, args...))
		},
	)
	if err != nil {
		t.Fatalf("watcher returned %v", err)
	}
	if calls < 3 {
		t.Fatalf("calls = %d, want at least 3", calls)
	}
	// An idle speaker must not fill the log.
	if len(logged) != 0 {
		t.Fatalf("idle watcher logged %v", logged)
	}
}

func TestConnectCeilingWatcherLogsRealFailuresOnce(t *testing.T) {
	calls := 0
	var logged []string
	ctx, cancel := context.WithCancel(context.Background())
	_ = runConnectCeilingWatcher(
		ctx,
		func(context.Context) (blueALSASnapshot, bool, error) {
			calls++
			if calls >= 4 {
				cancel()
			}
			return blueALSASnapshot{}, false, errors.New("bluealsa exploded")
		},
		func(c context.Context, _ time.Duration) error { return c.Err() },
		func(format string, args ...interface{}) {
			logged = append(logged, fmt.Sprintf(format, args...))
		},
	)
	if len(logged) != 1 {
		t.Fatalf("repeated failure logged %d times, want 1", len(logged))
	}
}
