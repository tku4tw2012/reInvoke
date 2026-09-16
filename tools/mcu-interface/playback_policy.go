// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	playbackPolicyInterval = 100 * time.Millisecond
	playbackPolicyHoldoff  = 1500 * time.Millisecond
)

type playbackMuteController interface {
	setPlaybackActive(bool) error
}

func playbackState(status []byte) (string, bool) {
	line := strings.TrimSpace(strings.SplitN(string(status), "\n", 2)[0])
	if line == "closed" {
		return line, true
	}
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != "state:" {
		return "", false
	}
	switch fields[1] {
	case "OPEN", "SETUP", "PREPARED", "RUNNING", "XRUN", "DRAINING", "PAUSED", "SUSPENDED", "DISCONNECTED":
		return fields[1], true
	default:
		return "", false
	}
}

func playbackIsRunning(status []byte) bool {
	state, valid := playbackState(status)
	return valid && state == "RUNNING"
}

func playbackOwnerPID(status []byte) (int, bool) {
	for _, line := range strings.Split(string(status), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "owner_pid" && fields[1] == ":" {
			pid, err := strconv.Atoi(fields[2])
			return pid, err == nil && pid > 1
		}
	}
	return 0, false
}

func playbackLeasePID(lease []byte) (int, bool) {
	pid, err := strconv.Atoi(strings.TrimSpace(string(lease)))
	return pid, err == nil && pid > 1
}

func runPlaybackPolicy(
	ctx context.Context,
	statusPath string,
	leasePath string,
	ownerExecutable string,
	controller playbackMuteController,
	interval time.Duration,
	holdoff time.Duration,
	logf func(string, ...interface{}),
) error {
	if interval <= 0 {
		return fmt.Errorf("playback policy interval must be positive")
	}
	if holdoff < 0 {
		return fmt.Errorf("playback policy holdoff must be non-negative")
	}
	active := false
	var lastRunning time.Time
	check := func() error {
		status, err := os.ReadFile(statusPath)
		running := err == nil && playbackIsRunning(status)
		if running && ownerExecutable != "" {
			// Confirm the renderer is the expected program before energising
			// the amplifier. The lease is optional: the donor Bluedroid stack
			// renders in-process through BtSocketHandler::OpenAlsa and knows
			// nothing about a lease file, so requiring one left the amplifier
			// muted for every source this runtime actually has.
			ownerPID, ownerOK := playbackOwnerPID(status)
			running = ownerOK
			if running {
				actual, linkErr := os.Readlink(
					"/proc/" + strconv.Itoa(ownerPID) + "/exe",
				)
				running = linkErr == nil && actual == ownerExecutable
			}
			if running && leasePath != "" {
				if lease, leaseErr := os.ReadFile(leasePath); leaseErr == nil {
					if leasePID, leaseOK := playbackLeasePID(lease); leaseOK {
						running = leasePID == ownerPID
					}
				}
			}
		}
		now := time.Now()
		if running {
			lastRunning = now
		}
		desired := running
		if !desired && active &&
			!lastRunning.IsZero() && now.Sub(lastRunning) < holdoff {
			desired = true
		}
		if desired == active {
			return nil
		}
		if err := controller.setPlaybackActive(desired); err != nil {
			if desired {
				muteErr := controller.setPlaybackActive(false)
				if muteErr != nil {
					return fmt.Errorf(
						"activate playback: %v; reassert mute: %w",
						err,
						muteErr,
					)
				}
				if logf != nil {
					logf("physical playback activation deferred: %v", err)
				}
				return nil
			}
			return fmt.Errorf("apply playback mute policy: %w", err)
		}
		active = desired
		if logf != nil {
			logf("physical playback path active=%t", active)
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return controller.setPlaybackActive(false)
		case <-ticker.C:
			if err := check(); err != nil {
				return err
			}
		}
	}
}
