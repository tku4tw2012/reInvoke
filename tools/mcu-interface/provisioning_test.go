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

func TestMicMuteLongOpensProvisioningWindow(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "window.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	requests := make(chan map[string]interface{}, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		var request map[string]interface{}
		if decodeErr := json.NewDecoder(connection).Decode(&request); decodeErr != nil {
			return
		}
		requests <- request
		_ = json.NewEncoder(connection).Encode(map[string]interface{}{
			"accepted": true,
		})
	}()

	controller := provisioningController{socketPath: socketPath}
	if err := controller.Apply(
		context.Background(),
		inputEvent{Name: "micmute-long"},
	); err != nil {
		t.Fatal(err)
	}
	request := <-requests
	if request["operation"] != "OPEN" {
		t.Fatalf("request = %#v", request)
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
