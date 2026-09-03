// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type scriptedRunner struct {
	commands      [][]string
	states        []string
	startErr      error
	terminateHook func()
}

func (r *scriptedRunner) Run(
	_ context.Context,
	name string,
	arguments ...string,
) ([]byte, error) {
	command := append([]string{name}, arguments...)
	r.commands = append(r.commands, command)
	if strings.HasSuffix(name, "wpa_supplicant") {
		return nil, r.startErr
	}
	if len(arguments) > 0 && arguments[len(arguments)-1] == "ping" {
		return []byte("PONG\n"), nil
	}
	if len(arguments) > 0 && arguments[len(arguments)-1] == "terminate" {
		if r.terminateHook != nil {
			r.terminateHook()
		}
		return nil, nil
	}
	if len(r.states) == 0 {
		return []byte("wpa_state=SCANNING\n"), nil
	}
	state := r.states[0]
	r.states = r.states[1:]
	return []byte("wpa_state=" + state + "\n"), nil
}

func TestWPAManagerReplacesExistingSupplicant(t *testing.T) {
	t.Parallel()

	directory := filepath.Join(t.TempDir(), "run")
	controlPath := filepath.Join(directory, "wpa")
	if err := os.MkdirAll(controlPath, 0700); err != nil {
		t.Fatalf("create control directory: %v", err)
	}
	socketPath := filepath.Join(controlPath, "mlan0")
	listener, err := net.ListenUnix(
		"unix",
		&net.UnixAddr{Name: socketPath, Net: "unix"},
	)
	if err != nil {
		t.Fatalf("create existing control socket: %v", err)
	}
	listener.SetUnlinkOnClose(true)
	if err := os.Chmod(socketPath, 0770); err != nil {
		t.Fatalf("restrict control socket: %v", err)
	}

	runner := &scriptedRunner{
		states: []string{"COMPLETED"},
		terminateHook: func() {
			_ = listener.Close()
		},
	}
	manager := wpaManager{
		runner: runner,
		writeConfig: func(path string, content []byte) error {
			return os.WriteFile(path, content, 0600)
		},
		supplicantPath: "/bin/wpa_supplicant",
		clientPath:     "/bin/wpa_cli",
		configPath:     filepath.Join(directory, "wpa.conf"),
		controlPath:    controlPath,
		interfaceName:  "mlan0",
		driverName:     "nl80211,wext",
		connectTimeout: 10 * time.Second,
		expectedUID:    uint32(os.Geteuid()),
	}

	err = manager.Apply(context.Background(), wifiRequest{
		SSID:       "replacement-network",
		Passphrase: "replacement-password",
		Security:   "wpa2-psk",
	})
	if err != nil {
		t.Fatalf("replace existing supplicant: %v", err)
	}
	if len(runner.commands) != 4 {
		t.Fatalf("command count = %d", len(runner.commands))
	}
	if runner.commands[0][len(runner.commands[0])-1] != "ping" ||
		runner.commands[1][len(runner.commands[1])-1] != "terminate" {
		t.Fatalf("unexpected replacement commands: %#v", runner.commands)
	}
}

func TestDeriveWPA2PSK(t *testing.T) {
	t.Parallel()

	actual := hex.EncodeToString(deriveWPA2PSK("password", "IEEE"))
	const expected = "f42c6fc52df0ebef9ebb4b90b38a5f902e83fe1b135a70e23aed762e9710a12e"
	if actual != expected {
		t.Fatalf("PSK = %s", actual)
	}
}

func TestRenderWPAConfig(t *testing.T) {
	t.Parallel()

	request := wifiRequest{
		SSID:       "test-network",
		Passphrase: "secret-passphrase",
		Security:   "wpa2-psk",
		Hidden:     true,
	}
	config := string(renderWPAConfig(request, "/run/reinvoke/wpa"))
	if strings.Contains(config, request.Passphrase) {
		t.Fatal("config contains plaintext passphrase")
	}
	for _, expected := range []string{
		"ctrl_interface=/run/reinvoke/wpa",
		"ssid=746573742d6e6574776f726b",
		"psk=",
		"key_mgmt=WPA-PSK",
		"scan_ssid=1",
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("config does not contain %q", expected)
		}
	}
}

func TestValidateWiFiRequest(t *testing.T) {
	t.Parallel()

	valid := wifiRequest{
		SSID:       "test-network",
		Passphrase: "test-password",
		Security:   "wpa2-psk",
	}
	if err := validateWiFiRequest(valid); err != nil {
		t.Fatalf("valid request: %v", err)
	}
	valid.Passphrase = "short"
	if err := validateWiFiRequest(valid); err == nil {
		t.Fatal("short passphrase was accepted")
	}
}

func TestWPAManagerApply(t *testing.T) {
	t.Parallel()

	directory := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatalf("create runtime directory: %v", err)
	}
	runner := &scriptedRunner{states: []string{"SCANNING", "COMPLETED"}}
	manager := wpaManager{
		runner: runner,
		writeConfig: func(path string, content []byte) error {
			if strings.Contains(string(content), "test-password") {
				return os.ErrInvalid
			}
			return os.WriteFile(path, content, 0600)
		},
		supplicantPath: "/bin/wpa_supplicant",
		clientPath:     "/bin/wpa_cli",
		configPath:     filepath.Join(directory, "wpa.conf"),
		controlPath:    filepath.Join(directory, "wpa"),
		interfaceName:  "mlan0",
		driverName:     "nl80211,wext",
		connectTimeout: 10 * time.Second,
	}

	err := manager.Apply(context.Background(), wifiRequest{
		SSID:       "test-network",
		Passphrase: "test-password",
		Security:   "wpa2-psk",
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(runner.commands) != 3 {
		t.Fatalf("command count = %d", len(runner.commands))
	}
	for _, argument := range runner.commands[0] {
		if argument == "test-password" {
			t.Fatal("passphrase appeared in process arguments")
		}
	}
}

func TestWPAState(t *testing.T) {
	t.Parallel()

	if state := wpaState([]byte("ssid=test\nwpa_state=COMPLETED\n")); state != "COMPLETED" {
		t.Fatalf("state = %q", state)
	}
}
