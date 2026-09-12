// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func profileManager(t *testing.T, runner *scriptedRunner) wpaManager {
	t.Helper()
	directory := t.TempDir()
	return wpaManager{
		runner:         runner,
		writeConfig:    func(path string, data []byte) error { return os.WriteFile(path, data, 0600) },
		supplicantPath: defaultSupplicant, clientPath: defaultWPAClient,
		configPath: filepath.Join(directory, "station.conf"), controlPath: filepath.Join(directory, "wpa"),
		interfaceName: defaultInterface, driverName: defaultDriver,
		connectTimeout: 20 * time.Millisecond, expectedUID: uint32(os.Geteuid()),
	}
}

func TestSaveProfileOnlyAfterAssociationAcknowledgement(t *testing.T) {
	request := wifiRequest{SSID: "synthetic-station", Passphrase: "synthetic-passphrase", Security: "wpa2-psk"}
	for _, scenario := range []struct {
		name     string
		runner   scriptedRunner
		wantSave bool
	}{
		{"completed", scriptedRunner{states: []string{"COMPLETED"}}, true},
		{"associating", scriptedRunner{states: []string{"ASSOCIATING"}}, false},
		{"start-fails", scriptedRunner{startErr: errors.New("synthetic failure")}, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			manager := profileManager(t, &scenario.runner)
			saved := 0
			manager.saveProfile = func(profile stationProfile) error {
				saved++
				if profile.SSID != request.SSID || profile.Security != request.Security ||
					len(profile.PSK) != 64 || strings.Contains(profile.PSK, request.Passphrase) {
					t.Fatal("save hook did not receive validated derived profile")
				}
				if len(scenario.runner.commands) < 2 ||
					scenario.runner.commands[len(scenario.runner.commands)-1][5] != "status" {
					t.Fatal("profile save preceded status acknowledgement")
				}
				return nil
			}
			err := manager.Apply(context.Background(), request)
			if scenario.wantSave {
				if err != nil || saved != 1 {
					t.Fatalf("associated profile not saved: %v", err)
				}
			} else if err == nil || saved != 0 {
				t.Fatal("failed association overwrote working profile")
			}
		})
	}
}

func TestResumeUsesValidatedDerivedKeyAndDoesNotSaveAgain(t *testing.T) {
	request := wifiRequest{SSID: "synthetic-station", Passphrase: "synthetic-passphrase", Security: "wpa2-psk", Hidden: true}
	profile := profileFromRequest(request)
	runner := &scriptedRunner{states: []string{"COMPLETED"}}
	manager := profileManager(t, runner)
	manager.saveProfile = func(stationProfile) error { t.Fatal("resume rewrote saved profile"); return nil }
	if err := manager.Resume(context.Background(), profile); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(manager.configPath)
	if err != nil || !strings.Contains(string(content), "psk="+profile.PSK) ||
		strings.Contains(string(content), request.Passphrase) ||
		!strings.Contains(string(content), "scan_ssid=1") {
		t.Fatal("resume config mismatch")
	}
	for _, command := range runner.commands {
		for _, argument := range command {
			if strings.Contains(argument, profile.PSK) || strings.Contains(argument, request.SSID) {
				t.Fatal("private profile appeared in process arguments")
			}
		}
	}
}

func TestRejectInvalidResumeBeforeCommands(t *testing.T) {
	for _, profile := range []stationProfile{
		{}, {SSID: "synthetic", PSK: "bad", Security: "wpa2-psk"},
		{SSID: "synthetic\nnetwork", PSK: strings.Repeat("a", 64), Security: "wpa2-psk"},
		{SSID: "synthetic", PSK: strings.Repeat("a", 64), Security: "open"},
	} {
		runner := &scriptedRunner{}
		manager := profileManager(t, runner)
		if err := manager.Resume(context.Background(), profile); err == nil || len(runner.commands) != 0 {
			t.Fatal("invalid resume profile started a command")
		}
	}
}

func TestPersistenceFailureLeavesSuccessfulAssociationUsable(t *testing.T) {
	runner := &scriptedRunner{states: []string{"COMPLETED"}}
	manager := profileManager(t, runner)
	manager.saveProfile = func(stationProfile) error { return errors.New("synthetic storage failure") }
	request := wifiRequest{SSID: "synthetic-station", Passphrase: "synthetic-passphrase", Security: "wpa2-psk"}
	if err := manager.Apply(context.Background(), request); err != nil {
		t.Fatal("persistence failure blocked volatile onboarding")
	}
	if len(runner.commands) != 2 {
		t.Fatal("persistence failure terminated successful association")
	}
}

func TestPublicWiFiStatusIsAllowlistedAndDoesNotImplyDHCP(t *testing.T) {
	if !validWiFiStatus("PERSIST_WIFI_ASSOCIATED") || !validWiFiStatus("PERSIST_WIFI_SAVE_FAILED") {
		t.Fatal("documented association status rejected")
	}
	for _, token := range []string{"PERSIST_WIFI_ONLINE", "PERSIST_WIFI_SSID=private", "PERSIST_WIFI_SAVED\nextra", ""} {
		if validWiFiStatus(token) {
			t.Fatal("unknown or data-bearing status accepted")
		}
	}
	runner := &scriptedRunner{states: []string{"COMPLETED"}}
	manager := profileManager(t, runner)
	manager.saveProfile = func(stationProfile) error { return nil }
	var reported []string
	manager.reportStatus = func(token string) { reported = append(reported, token) }
	if err := manager.Apply(context.Background(), wifiRequest{
		SSID: "synthetic-station", Passphrase: "synthetic-passphrase", Security: "wpa2-psk",
	}); err != nil {
		t.Fatal(err)
	}
	if len(reported) != 1 || reported[0] != "PERSIST_WIFI_SAVED" {
		t.Fatal("association/profile result was not reported as its bounded token")
	}
}
