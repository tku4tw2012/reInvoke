// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const playbackPolicyInterval = 100 * time.Millisecond

type playbackMuteController interface {
	setPlaybackActive(bool) error
}

func playbackIsRunning(status []byte) bool {
	return bytes.Contains(status, []byte("state: RUNNING"))
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
	logf func(string, ...interface{}),
) error {
	if interval <= 0 {
		return fmt.Errorf("playback policy interval must be positive")
	}
	active := false
	check := func() error {
		status, err := os.ReadFile(statusPath)
		running := err == nil && playbackIsRunning(status)
		if running {
			lease, leaseErr := os.ReadFile(leasePath)
			ownerPID, ownerOK := playbackOwnerPID(status)
			leasePID, leaseOK := playbackLeasePID(lease)
			running = leaseErr == nil && ownerOK && leaseOK &&
				leasePID == ownerPID
			if running && ownerExecutable != "" {
				actual, linkErr := os.Readlink(
					"/proc/" + strconv.Itoa(ownerPID) + "/exe",
				)
				running = linkErr == nil && actual == ownerExecutable
			}
		}
		if running == active {
			return nil
		}
		if err := controller.setPlaybackActive(running); err != nil {
			if running {
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
		active = running
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
