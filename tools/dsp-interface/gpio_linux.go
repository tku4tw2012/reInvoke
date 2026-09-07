// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

//go:build linux

package main

// sysfs GPIO backend, the same interface the donor uses: /sys/class/gpio with
// export, gpioN/direction, and gpioN/value. Nothing here is persistent
// storage; every path is a kernel control node.

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

type sysfsGPIO struct {
	root  string
	mu    sync.Mutex
	owned map[int]bool
}

func newSysfsGPIO(root string) *sysfsGPIO {
	return &sysfsGPIO{root: root, owned: map[int]bool{}}
}

func (lines *sysfsGPIO) pinPath(pin int) string {
	return lines.root + "/gpio" + strconv.Itoa(pin)
}

// Export is idempotent: a line another service already exported is left alone.
func (lines *sysfsGPIO) Export(pin int) error {
	lines.mu.Lock()
	defer lines.mu.Unlock()
	if _, err := os.Stat(lines.pinPath(pin)); err == nil {
		return nil
	}
	if err := os.WriteFile(
		lines.root+"/export",
		[]byte(strconv.Itoa(pin)),
		0,
	); err != nil {
		return fmt.Errorf("export GPIO %d: %w", pin, err)
	}
	lines.owned[pin] = true
	return nil
}

func (lines *sysfsGPIO) Unexport(pin int) error {
	lines.mu.Lock()
	defer lines.mu.Unlock()
	if !lines.owned[pin] {
		return nil
	}
	if _, err := os.Stat(lines.pinPath(pin)); err != nil {
		delete(lines.owned, pin)
		return nil
	}
	if err := os.WriteFile(
		lines.root+"/unexport",
		[]byte(strconv.Itoa(pin)),
		0,
	); err != nil {
		return fmt.Errorf("unexport GPIO %d: %w", pin, err)
	}
	delete(lines.owned, pin)
	return nil
}

func (lines *sysfsGPIO) Direction(pin int, output bool) error {
	direction := "in"
	if output {
		direction = "out"
	}
	if err := os.WriteFile(
		lines.pinPath(pin)+"/direction",
		[]byte(direction),
		0,
	); err != nil {
		return fmt.Errorf("set GPIO %d direction: %w", pin, err)
	}
	return nil
}

func (lines *sysfsGPIO) Write(pin int, high bool) error {
	value := "0"
	if high {
		value = "1"
	}
	if err := os.WriteFile(
		lines.pinPath(pin)+"/value",
		[]byte(value),
		0,
	); err != nil {
		return fmt.Errorf("write GPIO %d: %w", pin, err)
	}
	return nil
}

func (lines *sysfsGPIO) Read(pin int) (bool, error) {
	content, err := os.ReadFile(lines.pinPath(pin) + "/value")
	if err != nil {
		return false, fmt.Errorf("read GPIO %d: %w", pin, err)
	}
	return strings.TrimSpace(string(content)) != "0", nil
}
