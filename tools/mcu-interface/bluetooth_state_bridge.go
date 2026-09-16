// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// The donor Bluedroid stack reports its own Bluetooth state by calling
// com.harman.extStateUpdate with ["bluetooth"] and {"state": ...}. Nothing in
// this runtime registered that procedure, so every report came back
// wamp.error.no_such_procedure and the rear indicator never learned that a
// device had connected. Observed on candidate 05.8: nine rejected calls in one
// session, including {"state":"connected"} immediately after a successful A2DP
// connection, while /run/reinvoke/bluetooth-state still read "pairing".
//
// The three state strings below are the complete set the donor was observed to
// send. An unrecognised value is refused rather than written: the indicator
// contract only understands off/pairing/connected, and writing anything else
// makes the watcher log an invalid state and fall back to dark.
const bluetoothSubsystem = "bluetooth"

// externalBluetoothState maps a donor state report onto the indicator contract
// already understood by bluetoothStateWatcher.
func externalBluetoothState(state string) (string, error) {
	switch state {
	case "", "off":
		// The donor sends an empty state when it is neither pairing nor
		// connected, which is the idle case rather than a missing value.
		// "off" is accepted as the same thing so the indicator contract and
		// the donor vocabulary cannot drift apart.
		return "off", nil
	case "pairing":
		return "pairing", nil
	case "connected":
		return "connected", nil
	default:
		return "", fmt.Errorf("unsupported bluetooth state %q", state)
	}
}

// bluetoothStateReport extracts the subsystem and state from an
// extStateUpdate invocation, refusing anything that is not a Bluetooth report.
func bluetoothStateReport(
	args []interface{},
	kwargs map[string]interface{},
) (string, error) {
	if len(args) != 1 {
		return "", errors.New("invalid argument format")
	}
	subsystem, ok := args[0].(string)
	if !ok {
		return "", errors.New("invalid argument format")
	}
	if subsystem != bluetoothSubsystem {
		// Other subsystems may share this procedure; accept the call without
		// moving the Bluetooth indicator.
		return "", nil
	}
	raw, present := kwargs["state"]
	if !present {
		return "", errors.New("state is missing")
	}
	state, ok := raw.(string)
	if !ok {
		return "", errors.New("state is not a string")
	}
	return externalBluetoothState(state)
}

// writeBluetoothState publishes the indicator state atomically. The watcher
// polls this path, so a partial write would be read as an invalid state and
// darken the indicator.
func writeBluetoothState(path, state string) error {
	if path == "" {
		return errors.New("bluetooth state path is not configured")
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(state), 0o644); err != nil {
		return fmt.Errorf("write bluetooth state: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		os.Remove(temporary)
		return fmt.Errorf("publish bluetooth state: %w", err)
	}
	return nil
}

// ensureBluetoothStateDirectory creates the runtime directory the state file
// lives in so the first report cannot fail on a missing parent.
func ensureBluetoothStateDirectory(path string) error {
	if path == "" {
		return nil
	}
	return os.MkdirAll(filepath.Dir(path), 0o755)
}
