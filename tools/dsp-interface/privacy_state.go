// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
)

func microphoneMuteRequired(path string) (bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read microphone state: %w", err)
	}
	switch string(content) {
	case "muted\n":
		return true, nil
	case "unmuted\n":
		return false, nil
	default:
		return false, fmt.Errorf("invalid microphone state in %s", path)
	}
}
