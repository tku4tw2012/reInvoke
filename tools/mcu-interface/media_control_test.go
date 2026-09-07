// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTopTapTogglesRemotePlayback(t *testing.T) {
	statusPath := filepath.Join(t.TempDir(), "status")
	var calls [][]string
	controller := blueZMediaController{
		command:    "/bin/media-control",
		peer:       "AA:BB:CC:DD:EE:FF",
		statusPath: statusPath,
		run: func(_ context.Context, args ...string) ([]byte, error) {
			calls = append(calls, append([]string(nil), args...))
			return nil, nil
		},
	}
	if err := controller.Apply(
		context.Background(),
		inputEvent{Name: "action"},
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		statusPath,
		[]byte("state: RUNNING\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := controller.Apply(
		context.Background(),
		inputEvent{Name: "action"},
	); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"/bin/media-control", "AA:BB:CC:DD:EE:FF", "play"},
		{"/bin/media-control", "AA:BB:CC:DD:EE:FF", "pause"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}
