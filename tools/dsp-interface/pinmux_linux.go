// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	dspPinmuxRegister = uint32(0xF7EA8008)
	dspPinmuxGPIO5Bit = uint32(0x01000000)
	dspPinmuxLockPath = "/run/reinvoke/pinmux.lock"
	// Restoring message mode must survive the signal that cancels the boot, so
	// it runs on its own deadline instead of the process context.
	dspPinmuxRestoreTimeout = 10 * time.Second
	dspPinmuxLockRetry      = 10 * time.Millisecond
)

// detachedPinmuxContext returns a bounded context that no shutdown signal can
// cancel, so a cleanup write is never skipped by the condition that caused it.
// The caller must call the returned cancel function.
func detachedPinmuxContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(
		context.Background(),
		dspPinmuxRestoreTimeout,
	)
}

type pinmuxCommandRunner func(
	context.Context,
	string,
	...string,
) ([]byte, error)

func configureDSPPinmux(
	ctx context.Context,
	devmemPath string,
	lockPath string,
	messageMode bool,
	run pinmuxCommandRunner,
) (uint32, error) {
	if devmemPath == "" {
		return 0, fmt.Errorf("DSP pinmux tool is required")
	}
	if lockPath == "" {
		return 0, fmt.Errorf("DSP pinmux lock path is required")
	}
	if run == nil {
		run = func(
			ctx context.Context,
			name string,
			args ...string,
		) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		}
	}

	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return 0, fmt.Errorf("open DSP pinmux lock: %w", err)
	}
	defer lockFile.Close()
	if err := lockDSPPinmux(ctx, lockFile); err != nil {
		return 0, fmt.Errorf("lock DSP pinmux: %w", err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	current, err := readDSPPinmux(ctx, devmemPath, run)
	if err != nil {
		return 0, err
	}
	updated := current &^ dspPinmuxGPIO5Bit
	if messageMode {
		updated |= dspPinmuxGPIO5Bit
	}
	if updated != current {
		output, runErr := run(
			ctx,
			devmemPath,
			"devmem",
			fmt.Sprintf("0x%08X", dspPinmuxRegister),
			"32",
			fmt.Sprintf("0x%08X", updated),
		)
		if runErr != nil {
			return 0, fmt.Errorf(
				"write DSP pinmux: %w: %s",
				runErr,
				strings.TrimSpace(string(output)),
			)
		}
	}

	confirmed, err := readDSPPinmux(ctx, devmemPath, run)
	if err != nil {
		return 0, fmt.Errorf("verify DSP pinmux: %w", err)
	}
	if confirmed != updated {
		return 0, fmt.Errorf(
			"DSP pinmux readback 0x%08X, want 0x%08X",
			confirmed,
			updated,
		)
	}
	return confirmed, nil
}

func lockDSPPinmux(ctx context.Context, lockFile *os.File) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syscall.Flock(
			int(lockFile.Fd()),
			syscall.LOCK_EX|syscall.LOCK_NB,
		)
		if err == nil {
			return nil
		}
		if err != syscall.EAGAIN &&
			err != syscall.EWOULDBLOCK &&
			err != syscall.EINTR {
			return err
		}

		timer := time.NewTimer(dspPinmuxLockRetry)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func readDSPPinmux(
	ctx context.Context,
	devmemPath string,
	run pinmuxCommandRunner,
) (uint32, error) {
	output, err := run(
		ctx,
		devmemPath,
		"devmem",
		fmt.Sprintf("0x%08X", dspPinmuxRegister),
		"32",
	)
	if err != nil {
		return 0, fmt.Errorf(
			"read DSP pinmux: %w: %s",
			err,
			strings.TrimSpace(string(output)),
		)
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(output)), 0, 32)
	if err != nil {
		return 0, fmt.Errorf("parse DSP pinmux %q: %w", output, err)
	}
	return uint32(value), nil
}
