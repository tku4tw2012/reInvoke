// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCaptureSourceReadsExactPeriodsAndStops(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-loader")
	content := "#!/bin/sh\n" +
		"dd if=/dev/zero bs=2048 count=3 2>/dev/null\n" +
		"sleep 30\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	source, err := startCaptureSource(ctx, sourceConfig{
		loader:      script,
		libraryPath: "/unused",
		arecord:     "/unused",
		device:      "unused",
	})
	if err != nil {
		t.Fatalf("start source: %v", err)
	}
	for index := 0; index < 3; index++ {
		select {
		case period := <-source.periods:
			if len(period) != nativePeriodBytes {
				t.Fatalf("period %d size = %d", index, len(period))
			}
		case <-time.After(time.Second):
			t.Fatalf("period %d timed out", index)
		}
	}
	start := time.Now()
	cancel()
	source.stop()
	if time.Since(start) > time.Second {
		t.Fatal("source did not stop promptly")
	}
}

func TestCaptureSourceRejectsPartialPeriod(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-loader")
	content := "#!/bin/sh\n" +
		"dd if=/dev/zero bs=1024 count=1 2>/dev/null\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	source, err := startCaptureSource(context.Background(), sourceConfig{
		loader:      script,
		libraryPath: "/unused",
		arecord:     "/unused",
		device:      "unused",
	})
	if err != nil {
		t.Fatalf("start source: %v", err)
	}
	select {
	case err := <-source.done:
		if err == nil {
			t.Fatal("partial period was accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("partial source did not fail")
	}
}
