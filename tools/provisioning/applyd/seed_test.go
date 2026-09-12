// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func syntheticSeed() stationProfile {
	return stationProfile{SSID: "synthetic-first-boot", PSK: strings.Repeat("a", 64), Security: "wpa2-psk"}
}

func TestSavedProfileAlwaysPrecedesOptionalSeed(t *testing.T) {
	saved := syntheticSeed()
	saved.SSID = "synthetic-saved-station"
	seedCalls := 0
	seed := func() (stationProfile, error) { seedCalls++; return syntheticSeed(), nil }
	selected, seeded, err := selectBootProfile(func() (stationProfile, error) { return saved, nil }, seed)
	if err != nil || seeded || selected != saved || seedCalls != 0 {
		t.Fatal("optional seed overrode saved profile")
	}
	selected, seeded, err = selectBootProfile(func() (stationProfile, error) {
		return stationProfile{}, errSavedProfileAbsent
	}, seed)
	if err != nil || !seeded || selected != syntheticSeed() || seedCalls != 1 {
		t.Fatal("verified absent profile did not select optional seed")
	}
	for _, failure := range []error{errors.New("corrupt state"), errors.New("storage unavailable")} {
		_, seeded, err := selectBootProfile(func() (stationProfile, error) {
			return stationProfile{}, failure
		}, seed)
		if err == nil || seeded || seedCalls != 1 {
			t.Fatal("unknown saved state was treated as absence")
		}
	}
}

func TestSeedUsesConditionalSaveOnlyAfterCompleted(t *testing.T) {
	for _, scenario := range []struct {
		name, state string
		wantSave    bool
	}{
		{"associated", "COMPLETED", true},
		{"not-associated", "ASSOCIATING", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			runner := &scriptedRunner{states: []string{scenario.state}}
			manager := profileManager(t, runner)
			manager.saveProfile = func(stationProfile) error {
				t.Fatal("seed used unconditional profile replacement")
				return nil
			}
			saves := 0
			manager.saveSeedProfile = func(profile stationProfile) error {
				saves++
				if profile != syntheticSeed() || len(runner.commands) != 2 ||
					runner.commands[1][5] != "status" {
					t.Fatal("seed save preceded association acknowledgement")
				}
				return nil
			}
			err := manager.ResumeSeed(context.Background(), syntheticSeed())
			if scenario.wantSave {
				if err != nil || saves != 1 {
					t.Fatal("associated seed not conditionally saved")
				}
			} else if err == nil || saves != 0 {
				t.Fatal("failed seed association changed saved profile")
			}
		})
	}
}

func TestStrictSeedProfileDecoder(t *testing.T) {
	content, _ := json.Marshal(syntheticSeed())
	if actual, err := decodeSeedProfile(content); err != nil || actual != syntheticSeed() {
		t.Fatal("valid structured derived seed rejected")
	}
	for _, invalid := range [][]byte{
		[]byte(`{"ssid":"a","ssid":"b","psk":"` + strings.Repeat("a", 64) + `","security":"wpa2-psk"}`),
		[]byte(`{"ssid":"a","psk":"` + strings.Repeat("a", 64) + `","security":"wpa2-psk","hidden":null}`),
		[]byte(`{"ssid":"a","passphrase":"not-a-derived-profile","security":"wpa2-psk"}`),
		[]byte(`{"ssid":"a","psk":"short","security":"wpa2-psk"}`),
		[]byte(`{"SSID":"a","psk":"` + strings.Repeat("a", 64) + `","security":"wpa2-psk"}`),
		append(append([]byte(nil), content...), []byte("{}")...),
		[]byte{0xff},
	} {
		if _, err := decodeSeedProfile(invalid); err == nil {
			t.Fatal("invalid, ambiguous or plaintext seed accepted")
		}
	}
}
