// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

type pairingSignalController struct {
	pidPath    string
	executable string
	readFile   func(string) ([]byte, error)
	readlink   func(string) (string, error)
	signal     func(int, syscall.Signal) error
}

func (controller pairingSignalController) Apply(
	ctx context.Context,
	event inputEvent,
) error {
	if event.Name != "bluetooth-long" {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	readFile := controller.readFile
	if readFile == nil {
		readFile = os.ReadFile
	}
	readlink := controller.readlink
	if readlink == nil {
		readlink = os.Readlink
	}
	signal := controller.signal
	if signal == nil {
		signal = syscall.Kill
	}
	content, err := readFile(controller.pidPath)
	if err != nil {
		return fmt.Errorf("read pairing agent PID: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil || pid < 2 {
		return errors.New("pairing agent PID is invalid")
	}
	actual, err := readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if err != nil {
		return fmt.Errorf("inspect pairing agent process: %w", err)
	}
	if actual != controller.executable {
		return errors.New("pairing agent PID belongs to another executable")
	}
	if err := signal(pid, syscall.SIGUSR1); err != nil {
		return fmt.Errorf("signal pairing agent: %w", err)
	}
	return nil
}
