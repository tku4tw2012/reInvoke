// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDSPBootStateLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dsp-booted")
	if err := clearDSPBootState(path); err != nil {
		t.Fatal(err)
	}
	if err := recordDSPBootState(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("boot state mode = %v", info.Mode())
	}
	if err := recordDSPBootState(path); err != nil {
		t.Fatalf("duplicate boot event failed: %v", err)
	}
	if err := clearDSPBootState(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("boot state still exists: %v", err)
	}
}

func TestDSPBootStateRejectsNonRegularPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dsp-booted")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := recordDSPBootState(path); err == nil {
		t.Fatal("non-regular boot state was accepted")
	}
}
