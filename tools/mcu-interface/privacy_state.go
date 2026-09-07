// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	microphoneMutedState   = "muted\n"
	microphoneUnmutedState = "unmuted\n"
)

func loadOrInitializeMicrophoneState(path string) (bool, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := persistMicrophoneState(path, false); err != nil {
			return false, fmt.Errorf("initialize microphone state: %w", err)
		}
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read microphone state: %w", err)
	}
	switch string(content) {
	case microphoneMutedState:
		return true, nil
	case microphoneUnmutedState:
		return false, nil
	default:
		return false, fmt.Errorf("invalid microphone state in %s", path)
	}
}

func persistMicrophoneState(path string, muted bool) error {
	if path == "" {
		return nil
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(
		directory,
		"."+filepath.Base(path)+".",
	)
	if err != nil {
		return fmt.Errorf("create microphone state: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	content := microphoneUnmutedState
	if muted {
		content = microphoneMutedState
	}
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("protect microphone state: %w", err)
	}
	if _, err := temporary.WriteString(content); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write microphone state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync microphone state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close microphone state: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish microphone state: %w", err)
	}
	return nil
}
