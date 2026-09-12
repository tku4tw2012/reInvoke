// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"path/filepath"
	"regexp"
)

const statusFile = "/run/reinvoke/persistence-status.json"

var resultPattern = regexp.MustCompile(`^PERSIST_[A-Z_]{1,64}$`)

type publicStatus struct {
	Version     int    `json:"version"`
	Phase       string `json:"phase"`
	Snapshot    string `json:"snapshot"`
	WiFiProfile string `json:"wifi_profile"`
	Result      string `json:"result"`
}

func statusFor(state snapshot, phase, result string) publicStatus {
	view := publicStatus{
		Version: 1, Phase: phase, Snapshot: "unknown", WiFiProfile: "unknown", Result: result,
	}
	if state.Version == 1 {
		view.Snapshot = "absent"
		view.WiFiProfile = "absent"
		if len(state.Files) != 0 || state.WiFi != nil {
			view.Snapshot = "present"
		}
		if state.WiFi != nil {
			view.WiFiProfile = "present"
		}
	}
	return view
}

func writePublicStatus(path string, uid uint32, check func() error, view publicStatus) error {
	if view.Version != 1 ||
		(view.Phase != "prepared" && view.Phase != "ready" && view.Phase != "degraded" &&
			view.Phase != "volatile" && view.Phase != "stopped") ||
		(view.Snapshot != "present" && view.Snapshot != "absent" && view.Snapshot != "unknown") ||
		(view.WiFiProfile != "present" && view.WiFiProfile != "absent" && view.WiFiProfile != "unknown") ||
		!resultPattern.MatchString(view.Result) {
		return errors.New("PERSIST_STATUS_INVALID")
	}
	if err := check(); err != nil {
		return err
	}
	if err := ensurePrivateDirectory(filepath.Dir(path), uid); err != nil {
		return err
	}
	content, err := json.Marshal(view)
	if err != nil || len(content) > 512 {
		return errors.New("PERSIST_STATUS_INVALID")
	}
	content = append(content, '\n')
	if old, err := readPrivate(path, uid, 512); err == nil && bytes.Equal(old, content) {
		return nil
	}
	return atomicPrivate(path, content, uid, nil)
}

func publishStatus(view publicStatus) {
	if !resultPattern.MatchString(view.Result) {
		view.Result = "PERSIST_INTERNAL_ERROR"
	}
	// The caller's existing named log remains available if RAM status itself
	// cannot be safely published. Never fall back to a persistent status file.
	if err := writePublicStatus(statusFile, 0, func() error { return checkRAM(runtimeDir) }, view); err != nil {
		log.Print("PERSIST_STATUS_UNAVAILABLE")
	}
}
