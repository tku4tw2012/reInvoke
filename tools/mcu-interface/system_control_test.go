// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestDuckAttenuatesWithoutLosingTheChosenLevel proves a duck lowers what is
// played without overwriting what the user selected, so releasing it returns
// to the same level rather than to a default.
func TestDuckAttenuatesWithoutLosingTheChosenLevel(t *testing.T) {
	controller, err := newDSPVolumeController("/tmp/does-not-matter")
	if err != nil {
		t.Fatalf("controller: %v", err)
	}
	controller.push = func(context.Context, int) error { return nil }
	ctx := context.Background()

	if _, err := controller.SetVolume(ctx, 80); err != nil {
		t.Fatalf("set volume: %v", err)
	}
	if level := controller.effectiveLevel(); level != 80 {
		t.Fatalf("undicked level is %d, expected 80", level)
	}

	snapshot, err := controller.SetDuck(ctx, "voice", duckSoft)
	if err != nil {
		t.Fatalf("soft duck: %v", err)
	}
	if snapshot.Volume != 80 {
		t.Fatalf("duck changed the chosen level to %d", snapshot.Volume)
	}
	if snapshot.Duck != "soft" {
		t.Fatalf("duck reported as %q", snapshot.Duck)
	}
	soft := controller.effectiveLevel()
	if soft >= 80 || soft == 0 {
		t.Fatalf("soft duck produced %d, expected quieter but audible", soft)
	}

	if _, err := controller.SetDuck(ctx, "voice", duckNone); err != nil {
		t.Fatalf("release duck: %v", err)
	}
	if level := controller.effectiveLevel(); level != 80 {
		t.Fatalf("release restored %d, expected the chosen 80", level)
	}
}

// TestOverlappingDucksHoldTheDeepest proves that releasing one duck while
// another is still held does not restore full volume. The donor kept a map of
// named ducks for exactly this reason.
func TestOverlappingDucksHoldTheDeepest(t *testing.T) {
	controller, err := newDSPVolumeController("/tmp/does-not-matter")
	if err != nil {
		t.Fatalf("controller: %v", err)
	}
	controller.push = func(context.Context, int) error { return nil }
	ctx := context.Background()
	if _, err := controller.SetVolume(ctx, 100); err != nil {
		t.Fatalf("set volume: %v", err)
	}

	if _, err := controller.SetDuck(ctx, "alert", duckSoft); err != nil {
		t.Fatalf("soft duck: %v", err)
	}
	if _, err := controller.SetDuck(ctx, "voice", duckHard); err != nil {
		t.Fatalf("hard duck: %v", err)
	}
	hard := controller.effectiveLevel()

	// Releasing the shallower duck must leave the deeper one in force.
	if _, err := controller.SetDuck(ctx, "alert", duckNone); err != nil {
		t.Fatalf("release soft: %v", err)
	}
	if level := controller.effectiveLevel(); level != hard {
		t.Fatalf("releasing the soft duck changed level to %d, expected %d", level, hard)
	}
	if _, err := controller.SetDuck(ctx, "voice", duckNone); err != nil {
		t.Fatalf("release hard: %v", err)
	}
	if level := controller.effectiveLevel(); level != 100 {
		t.Fatalf("releasing every duck gave %d, expected 100", level)
	}
}

// TestMuteBeatsDuck proves mute still silences regardless of ducking.
func TestMuteBeatsDuck(t *testing.T) {
	controller, err := newDSPVolumeController("/tmp/does-not-matter")
	if err != nil {
		t.Fatalf("controller: %v", err)
	}
	controller.push = func(context.Context, int) error { return nil }
	ctx := context.Background()
	if _, err := controller.SetVolume(ctx, 60); err != nil {
		t.Fatalf("set volume: %v", err)
	}
	if _, err := controller.SetDuck(ctx, "voice", duckSoft); err != nil {
		t.Fatalf("duck: %v", err)
	}
	if _, err := controller.SetMuted(ctx, true); err != nil {
		t.Fatalf("mute: %v", err)
	}
	if level := controller.effectiveLevel(); level != 0 {
		t.Fatalf("muted level is %d, expected 0", level)
	}
}

// TestApplyTimezoneRejectsEscapes proves the zone name cannot be used to point
// /etc/localtime outside the zone directory. This procedure is reachable from
// the WAMP router, so a legitimate name and a traversal arrive the same way.
func TestApplyTimezoneRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	zones := filepath.Join(root, "zoneinfo")
	if err := os.MkdirAll(filepath.Join(zones, "America"), 0o755); err != nil {
		t.Fatalf("stage zones: %v", err)
	}
	good := filepath.Join(zones, "America", "New_York")
	if err := os.WriteFile(good, []byte("TZif"), 0o644); err != nil {
		t.Fatalf("stage zone: %v", err)
	}
	secret := filepath.Join(root, "secret")
	if err := os.WriteFile(secret, []byte("x"), 0o644); err != nil {
		t.Fatalf("stage secret: %v", err)
	}

	originalRoot, originalLink, originalState := zoneinfoRoot, timezoneLinkPath, timezoneStatePath
	zoneinfoRoot = zones
	link := filepath.Join(root, "localtime")
	timezoneLinkPath = link
	timezoneStatePath = filepath.Join(root, "state", "timezone")
	defer func() {
		zoneinfoRoot = originalRoot
		timezoneLinkPath = originalLink
		timezoneStatePath = originalState
	}()

	for _, bad := range []string{"../secret", "/etc/shadow", "America/../../secret", "", "America"} {
		if err := applyTimezone(bad); err == nil {
			t.Fatalf("applyTimezone accepted %q", bad)
		}
	}
	if _, err := os.Lstat(link); err == nil {
		t.Fatal("a rejected timezone still wrote the link")
	}

	if err := applyTimezone("America/New_York"); err != nil {
		t.Fatalf("applyTimezone rejected a real zone: %v", err)
	}
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("link not created: %v", err)
	}
	if target != good {
		t.Fatalf("link points at %q, expected %q", target, good)
	}
	if name := currentTimezone(); name != "America/New_York" {
		t.Fatalf("currentTimezone reported %q", name)
	}
}

// TestDuckArgumentsRejectsMalformed proves the payload reader does not accept
// shapes the donor never sent.
func TestDuckArgumentsRejectsMalformed(t *testing.T) {
	for _, args := range [][]interface{}{
		{},
		{""},
		{1},
		{"voice", 2},
		{"voice", "sideways"},
		{"voice", "soft", "extra"},
	} {
		if _, _, err := duckArguments(args); err == nil {
			t.Fatalf("duckArguments accepted %v", args)
		}
	}
	name, state, err := duckArguments([]interface{}{"voice", "hard"})
	if err != nil || name != "voice" || state != duckHard {
		t.Fatalf("duckArguments(voice,hard) = %q,%v,%v", name, state, err)
	}
	if _, state, err := duckArguments([]interface{}{"voice"}); err != nil || state != duckNone {
		t.Fatalf("a bare name should release the duck, got %v,%v", state, err)
	}
}
