// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

// Front indicator: network state.
//
// The donor drove this from audio-ui. Its own state strings, and the ledSet
// arguments beside them, give the mapping directly:
//
//	online       front white  solid
//	wifi-setup   front amber  fast-blink
//	not online   front amber  solid
//
// The donor had a fourth state, downloading, as front white fast-blink. This
// runtime fetches no updates, so that lamp state could never occur and is not
// implemented rather than being wired to a condition that never fires.
//
// Every one of those four combinations was confirmed by eye on this unit
// before this was written, so the values here are observed rather than
// assumed. What was missing was anything driving them: the front lamp sat at
// whatever the microcontroller lit at power-up, which happens to look like
// "online" whether or not the speaker has a network.

package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"
)

type frontIndicatorController interface {
	SetContext(ctx context.Context, target, mode, color string) error
}

// networkIndicatorState is one of the donor's four front-lamp states.
type networkIndicatorState struct {
	name  string
	mode  string
	color string
}

var (
	networkOnline       = networkIndicatorState{"online", "on", "white"}
	networkProvisioning = networkIndicatorState{"wifi-setup", "fast-blink", "amber"}
	networkOffline      = networkIndicatorState{"offline", "on", "amber"}
)

// provisioningMarkerPath exists while the setup access point is open. The
// provisioning controller already maintains it.
var provisioningMarkerPath = "/run/reinvoke/provisioning-window"

type networkStateWatcher struct {
	indicator frontIndicatorController
	interval  time.Duration
	logf      func(string, ...interface{})

	// interfaces is injectable so the state machine can be tested without a
	// network.
	interfaces func() ([]net.Interface, error)
	fileExists func(string) bool

	last networkIndicatorState
}

func newNetworkStateWatcher(
	indicator frontIndicatorController,
	logf func(string, ...interface{}),
) *networkStateWatcher {
	return &networkStateWatcher{
		indicator:  indicator,
		interval:   2 * time.Second,
		logf:       logf,
		interfaces: net.Interfaces,
		fileExists: func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		},
	}
}

// state reports which of the donor's four states applies right now.
//
// Provisioning outranks connectivity: the setup access point gives the speaker
// an address, so a connectivity check alone would call it online and hide the
// one state the operator actually needs to see.
func (watcher *networkStateWatcher) state() networkIndicatorState {
	if watcher.fileExists(provisioningMarkerPath) {
		return networkProvisioning
	}
	if watcher.online() {
		return networkOnline
	}
	return networkOffline
}

// online reports whether any non-loopback interface holds a routable IPv4
// address. A link-local 169.254 address means association failed, so it is
// treated as offline rather than as a network.
func (watcher *networkStateWatcher) online() bool {
	interfaces, err := watcher.interfaces()
	if err != nil {
		return false
	}
	for _, candidate := range interfaces {
		if candidate.Flags&net.FlagLoopback != 0 ||
			candidate.Flags&net.FlagUp == 0 {
			continue
		}
		addresses, err := candidate.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			network, ok := address.(*net.IPNet)
			if !ok {
				continue
			}
			value := network.IP.To4()
			if value == nil || value.IsLinkLocalUnicast() {
				continue
			}
			return true
		}
	}
	return false
}

// apply writes the lamp only when the state changed. The lamp shares one I2C
// bus with the amplifier and the buttons, so rewriting an unchanged value
// every tick would be traffic for nothing.
func (watcher *networkStateWatcher) apply(ctx context.Context) error {
	current := watcher.state()
	if current == watcher.last {
		return nil
	}
	if err := watcher.indicator.SetContext(
		ctx, "front", current.mode, current.color,
	); err != nil {
		return fmt.Errorf("set front indicator to %s: %w", current.name, err)
	}
	watcher.last = current
	if watcher.logf != nil {
		watcher.logf("FRONT_INDICATOR %s (%s %s)",
			current.name, current.color, current.mode)
	}
	return nil
}

func (watcher *networkStateWatcher) Run(ctx context.Context) error {
	interval := watcher.interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// Assert the lamp immediately rather than waiting a tick: at startup it
	// holds whatever the microcontroller lit at power-up.
	if err := watcher.apply(ctx); err != nil && watcher.logf != nil {
		watcher.logf("FRONT_INDICATOR_FAILED: %v", err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := watcher.apply(ctx); err != nil && watcher.logf != nil {
				watcher.logf("FRONT_INDICATOR_FAILED: %v", err)
			}
		}
	}
}
