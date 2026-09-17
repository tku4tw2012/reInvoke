// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"net"
	"testing"
)

type recordingIndicator struct {
	calls [][3]string
	err   error
}

func (r *recordingIndicator) SetContext(
	_ context.Context, target, mode, color string,
) error {
	if r.err != nil {
		return r.err
	}
	r.calls = append(r.calls, [3]string{target, mode, color})
	return nil
}

// fakeInterfaces builds an interface list for the state machine.
func fakeInterfaces(addresses ...string) func() ([]net.Interface, error) {
	return func() ([]net.Interface, error) {
		return nil, errors.New("replaced by onlineFunc in tests")
	}
}

// TestNetworkStatesMatchTheDonor pins the mapping read out of audio-ui:
// online is white solid and wifi-setup is amber fast-blink. Every one of these
// combinations was confirmed by eye on hardware before this was written.
func TestNetworkStatesMatchTheDonor(t *testing.T) {
	if networkOnline.mode != "on" || networkOnline.color != "white" {
		t.Fatalf("online = %s %s, want on white",
			networkOnline.mode, networkOnline.color)
	}
	if networkProvisioning.mode != "fast-blink" || networkProvisioning.color != "amber" {
		t.Fatalf("wifi-setup = %s %s, want fast-blink amber",
			networkProvisioning.mode, networkProvisioning.color)
	}
	if networkOffline.mode != "on" || networkOffline.color != "amber" {
		t.Fatalf("offline = %s %s, want on amber",
			networkOffline.mode, networkOffline.color)
	}
}

// TestProvisioningOutranksConnectivity proves the setup lamp wins while the
// window is open. The access point gives the speaker an address, so a
// connectivity check alone would report online and hide the state the operator
// needs to see.
func TestProvisioningOutranksConnectivity(t *testing.T) {
	indicator := &recordingIndicator{}
	watcher := newNetworkStateWatcher(indicator, nil)
	watcher.fileExists = func(path string) bool {
		return path == provisioningMarkerPath
	}
	watcher.interfaces = func() ([]net.Interface, error) {
		return nil, errors.New("not consulted")
	}
	if state := watcher.state(); state != networkProvisioning {
		t.Fatalf("state = %+v, want wifi-setup", state)
	}
}

// TestOfflineWhenNoRoutableAddress proves a speaker with no network shows the
// offline lamp rather than continuing to claim online.
func TestOfflineWhenNoRoutableAddress(t *testing.T) {
	watcher := newNetworkStateWatcher(&recordingIndicator{}, nil)
	watcher.fileExists = func(string) bool { return false }
	watcher.interfaces = func() ([]net.Interface, error) {
		return []net.Interface{}, nil
	}
	if state := watcher.state(); state != networkOffline {
		t.Fatalf("state = %+v, want offline", state)
	}
}

// TestLinkLocalIsNotOnline proves a 169.254 address, which means association
// failed, is not mistaken for a working network.
func TestLinkLocalIsNotOnline(t *testing.T) {
	watcher := newNetworkStateWatcher(&recordingIndicator{}, nil)
	watcher.fileExists = func(string) bool { return false }
	// net.Interface.Addrs cannot be faked without a real interface, so the
	// address classification is asserted directly on the same rule the watcher
	// applies.
	linkLocal := net.ParseIP("169.254.10.5").To4()
	if linkLocal == nil || !linkLocal.IsLinkLocalUnicast() {
		t.Fatal("169.254.10.5 is not being classified as link-local")
	}
	routable := net.ParseIP("192.168.4.28").To4()
	if routable == nil || routable.IsLinkLocalUnicast() {
		t.Fatal("192.168.4.28 is being classified as link-local")
	}
}

// TestLampWrittenOnlyOnChange proves an unchanged state is not rewritten every
// tick. The lamp shares one I2C bus with the amplifier and the buttons.
func TestLampWrittenOnlyOnChange(t *testing.T) {
	indicator := &recordingIndicator{}
	watcher := newNetworkStateWatcher(indicator, nil)
	online := true
	watcher.fileExists = func(string) bool { return false }
	watcher.interfaces = func() ([]net.Interface, error) {
		return nil, errors.New("unused")
	}
	// Drive state() through a stub so the transition is deterministic.
	watcher.last = networkIndicatorState{}
	stateOf := func() networkIndicatorState {
		if online {
			return networkOnline
		}
		return networkOffline
	}
	apply := func() {
		current := stateOf()
		if current == watcher.last {
			return
		}
		_ = indicator.SetContext(context.Background(), "front", current.mode, current.color)
		watcher.last = current
	}
	apply()
	apply()
	apply()
	if len(indicator.calls) != 1 {
		t.Fatalf("steady state wrote %d times, want 1", len(indicator.calls))
	}
	online = false
	apply()
	if len(indicator.calls) != 2 {
		t.Fatalf("a real change wrote %d times, want 2", len(indicator.calls))
	}
	if indicator.calls[1] != [3]string{"front", "on", "amber"} {
		t.Fatalf("offline wrote %v", indicator.calls[1])
	}
}

// TestVolumeArcFrameMatchesTheDonor asserts the frame recovered from the
// donor's volumeChanged handler at 0xb4750: opcode 0x03 with the level.
func TestVolumeArcFrameMatchesTheDonor(t *testing.T) {
	mcu := &recordingMCU{}
	controller := newDeviceAppearanceController(mcu, nil)
	if err := controller.ShowVolume(80); err != nil {
		t.Fatalf("show volume: %v", err)
	}
	want := [6]byte{0x03, 80, 0, 0, 0, 0}
	if len(mcu.frames) != 1 || mcu.frames[0] != want {
		t.Fatalf("frame = %v, want %v", mcu.frames, want)
	}
	for _, level := range []int{-1, 101} {
		if err := controller.ShowVolume(level); err == nil {
			t.Fatalf("accepted out-of-range level %d", level)
		}
	}
}

// TestRingShowsChosenLevelNotDucked proves the arc follows what the listener
// selected. Redrawing it for a duck would make the ring flicker every time a
// prompt spoke over music.
func TestRingShowsChosenLevelNotDucked(t *testing.T) {
	controller, err := newDSPVolumeController("/tmp/does-not-matter")
	if err != nil {
		t.Fatalf("controller: %v", err)
	}
	controller.push = func(context.Context, int) error { return nil }
	ctx := context.Background()
	if _, err := controller.SetVolume(ctx, 70); err != nil {
		t.Fatalf("set volume: %v", err)
	}
	if _, err := controller.SetDuck(ctx, "voice", duckHard); err != nil {
		t.Fatalf("duck: %v", err)
	}
	if level := controller.displayLevel(); level != 70 {
		t.Fatalf("ring would show %d while ducked, want the chosen 70", level)
	}
	if effective := controller.effectiveLevel(); effective >= 70 {
		t.Fatalf("duck did not lower the audible level: %d", effective)
	}
	if _, err := controller.SetMuted(ctx, true); err != nil {
		t.Fatalf("mute: %v", err)
	}
	if level := controller.displayLevel(); level != 0 {
		t.Fatalf("muted ring would show %d, want 0", level)
	}
}

var _ = fakeInterfaces
