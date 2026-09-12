// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusContractNeverSerializesPrivateState(t *testing.T) {
	store := fixtureStore(t)
	path := filepath.Join(store.runtime, "persistence-status.json")
	view := statusFor(testSnapshot(), "ready", "PERSIST_COMMITTED")
	if err := writePublicStatus(path, store.uid, func() error { return nil }, view); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || len(content) > 512 {
		t.Fatal("status unavailable or exceeds contract bound")
	}
	for _, private := range []string{
		testProfile().SSID, testProfile().PSK, "02:00:00:00:00:01", "02:00:00:00:00:02", "microphone-state",
	} {
		if bytes.Contains(content, []byte(private)) {
			t.Fatal("status exposed private data")
		}
	}
	var parsed publicStatus
	if err := json.Unmarshal(content, &parsed); err != nil || parsed != view ||
		parsed.Snapshot != "present" || parsed.WiFiProfile != "present" {
		t.Fatal("public status contract mismatch")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("status is not private RAM metadata")
	}
	for _, result := range []string{"arbitrary data", "PERSIST_\nINJECTION", "PERSIST_" + strings.Repeat("A", 65)} {
		bad := view
		bad.Result = result
		if err := writePublicStatus(path, store.uid, func() error { return nil }, bad); err == nil {
			t.Fatal("unbounded or non-token status accepted")
		}
	}
}

func TestAbsentAndUnknownStatusAreNotReadyStateClaims(t *testing.T) {
	absent := statusFor(snapshot{Version: 1, Files: map[string][]byte{}}, "prepared", "PERSIST_PREPARED")
	if absent.Snapshot != "absent" || absent.WiFiProfile != "absent" {
		t.Fatal("missing state reported present")
	}
	failed := statusFor(snapshot{}, "volatile", "PERSIST_APP_PARTITION_ABSENT")
	if failed.Snapshot != "unknown" || failed.WiFiProfile != "unknown" || failed.Phase != "volatile" {
		t.Fatal("failed storage reported persistence success")
	}
}
