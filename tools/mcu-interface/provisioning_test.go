// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
)

func runFakeProvisioningServer(
	t *testing.T,
	socketPath string,
	outcome string,
) {
	t.Helper()
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		var request map[string]interface{}
		_ = json.NewDecoder(connection).Decode(&request)
		_ = json.NewEncoder(connection).Encode(map[string]interface{}{
			"accepted":         true,
			"duration_seconds": int64(5),
		})
		if outcome != "" {
			_ = json.NewEncoder(connection).Encode(
				map[string]interface{}{"outcome": outcome},
			)
		}
	}()
}

func TestMicMuteLongOpensProvisioningWindow(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "window.sock")
	runFakeProvisioningServer(t, socketPath, "")
	controller := provisioningController{socketPath: socketPath}
	if err := controller.Apply(
		context.Background(),
		inputEvent{Name: "micmute-long"},
	); err != nil {
		t.Fatal(err)
	}
}

func TestProvisioningControllerPlaysSetupAndOutcomeAnimations(t *testing.T) {
	for _, outcome := range []string{"applied", "failed", "timeout"} {
		outcome := outcome
		t.Run(outcome, func(t *testing.T) {
			socketPath := filepath.Join(t.TempDir(), "window.sock")
			runFakeProvisioningServer(t, socketPath, outcome)
			writer := &recordingLEDWriter{}
			lights := &ledPlayer{
				directory: t.TempDir(),
				writer:    writer,
			}
			controller := provisioningController{
				socketPath: socketPath,
				lights:     lights,
			}
			if err := controller.Apply(
				context.Background(),
				inputEvent{Name: "micmute-long"},
			); err != nil {
				t.Fatalf("Apply: %v", err)
			}
		})
	}
}

func TestProvisioningControllerIgnoresOtherEvents(t *testing.T) {
	controller := provisioningController{socketPath: "/missing"}
	if err := controller.Apply(
		context.Background(),
		inputEvent{Name: "micmute"},
	); err != nil {
		t.Fatal(err)
	}
}
