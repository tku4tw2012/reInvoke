// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"
)

const bluetoothStateMaximumSize = 32

type bluetoothIndicatorController interface {
	SetBackIfChanged(string) error
}

type bluetoothStateWatcher struct {
	path           string
	indicator      bluetoothIndicatorController
	interval       time.Duration
	readFile       func(string) ([]byte, error)
	logf           func(string, ...interface{})
	lastStatus     string
	lastWriteError string
}

func finalizeBluetoothIndicator(
	watcherDone <-chan error,
	indicator bluetoothIndicatorController,
) (watcherErr, clearErr error) {
	watcherErr = <-watcherDone
	if err := indicator.SetBackIfChanged("off"); err != nil {
		clearErr = fmt.Errorf("final Bluetooth indicator clear: %w", err)
	}
	return watcherErr, clearErr
}

func (watcher *bluetoothStateWatcher) Run(ctx context.Context) error {
	interval := watcher.interval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if err := watcher.indicator.SetBackIfChanged("off"); err != nil {
				return fmt.Errorf("clear Bluetooth indicator: %w", err)
			}
			return nil
		default:
		}
		watcher.reconcile()
		select {
		case <-ctx.Done():
			if err := watcher.indicator.SetBackIfChanged("off"); err != nil {
				return fmt.Errorf("clear Bluetooth indicator: %w", err)
			}
			return nil
		case <-ticker.C:
		}
	}
}

func (watcher *bluetoothStateWatcher) reconcile() {
	readFile := watcher.readFile
	if readFile == nil {
		readFile = readBoundedBluetoothState
	}
	logf := watcher.logf
	if logf == nil {
		logf = log.Printf
	}
	content, readErr := readFile(watcher.path)
	mode, status, stateErr := decodeBluetoothIndicatorState(content, readErr)
	if status != watcher.lastStatus {
		if stateErr != nil {
			logf("Bluetooth indicator state: %v; using safe off", stateErr)
		} else {
			logf("Bluetooth indicator state=%s", status)
		}
		watcher.lastStatus = status
	}
	if err := watcher.indicator.SetBackIfChanged(mode); err != nil {
		if err.Error() != watcher.lastWriteError {
			logf("Bluetooth indicator write: %v", err)
			watcher.lastWriteError = err.Error()
		}
		return
	}
	if watcher.lastWriteError != "" {
		logf("Bluetooth indicator write recovered")
		watcher.lastWriteError = ""
	}
}

func readBoundedBluetoothState(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, bluetoothStateMaximumSize+1))
	if err != nil {
		return nil, err
	}
	if len(content) > bluetoothStateMaximumSize {
		return nil, errors.New("state file is too large")
	}
	return content, nil
}

func decodeBluetoothIndicatorState(
	content []byte,
	readErr error,
) (mode, status string, err error) {
	if readErr != nil {
		return "off", "error:" + readErr.Error(), readErr
	}
	state := strings.TrimRight(string(content), " \t\r\n")
	switch state {
	case "pairing":
		return "slow-blink", state, nil
	case "connected":
		return "on", state, nil
	case "off":
		return "off", state, nil
	default:
		return "off", "invalid:" + state, fmt.Errorf(
			"invalid state %q",
			state,
		)
	}
}
