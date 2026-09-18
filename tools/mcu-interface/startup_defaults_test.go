// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestAppearanceDefaultsAreOffByDefault pins the startup behaviour.
//
// The LED brightness and colour opcodes were recovered from the donor binary,
// not guessed, but no frame carrying them has reached this unit's
// microcontroller and there is no evidence of what it does with a command it
// does not recognise. Sending them at boot would make that an unobserved write
// on every boot. The flag makes the first one deliberate instead.
func TestAppearanceDefaultsAreOffByDefault(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(source)
	if !strings.Contains(text, `"apply-appearance-defaults", true,`) {
		t.Fatal("the startup appearance write is not enabled by default")
	}
	if !strings.Contains(text, "if *applyAppearanceDefaults {") {
		t.Fatal("ApplyDefaults is not gated by the flag")
	}
}

// TestNoUnprovenOpcodeReachesStartup guards the wider rule: no MCU opcode that
// has never been seen working on this unit may be written during startup
// without a gate. The proven ones are the indicator LEDs and the heartbeat,
// both of which this unit demonstrably responds to.
func TestNoUnprovenOpcodeReachesStartup(t *testing.T) {
	source, err := os.ReadFile("startup.go")
	if err != nil {
		t.Fatalf("read startup.go: %v", err)
	}
	for _, unproven := range []string{
		"setBrightnessCode", "setDeviceColorCode", "getDeviceColorCode",
		"setPowerModeCode", "getHWIDCode",
	} {
		if strings.Contains(string(source), unproven) {
			t.Fatalf("startup writes the unproven opcode %s", unproven)
		}
	}
}

// TestInitFlagsAreAccepted proves this service accepts the exact flags the
// generated init passes it.
//
// Removing the amplifier owner restriction dropped --playback-owner-executable
// from init while main still refused to start unless it arrived alongside
// --playback-status. The service crash-looped on hardware and every control it
// owns went with it: LEDs, buttons, volume, the boot cue. Nothing caught it
// because no test read the flags init actually writes.
//
// The generated init is read rather than the patcher source: the patcher
// contains both the text it searches for and the text it writes, and only the
// second reaches the device.
func TestInitFlagsAreAccepted(t *testing.T) {
	generated := os.Getenv("REINVOKE_GENERATED_INIT")
	if generated == "" {
		t.Skip("set REINVOKE_GENERATED_INIT to the built init to run this")
	}
	source, err := os.ReadFile(generated)
	if err != nil {
		t.Fatalf("read the generated init: %v", err)
	}
	init := string(source)

	// The flags this service defines are read from its own source rather than
	// from the flag package: a test binary has its own flag set, not the one
	// main builds.
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	defined := map[string]bool{}
	for _, match := range regexp.MustCompile(
		`flag\.(?:String|Int|Bool|Duration)\(\s*"([a-z0-9-]+)"`,
	).FindAllStringSubmatch(string(mainSource), -1) {
		defined[match[1]] = true
	}
	if len(defined) < 10 {
		t.Fatalf("only found %d flag definitions; the pattern is wrong", len(defined))
	}

	// Find the supervised invocation, not the executable test that precedes
	// it. The init checks the binary exists before starting it, and the first
	// mention is that check.
	index := strings.Index(init, "supervise mcu-interface")
	if index < 0 {
		t.Fatal("the generated init does not supervise mcu-interface")
	}
	invocation := init[index:]
	// A supervised command is one continued line; it ends at the first line
	// that does not continue.
	lines := strings.Split(invocation, "\n")
	var collected []string
	for _, line := range lines {
		collected = append(collected, line)
		if !strings.HasSuffix(strings.TrimRight(line, " \t"), "\\") {
			break
		}
	}
	invocation = strings.Join(collected, "\n")
	for _, field := range strings.Fields(invocation) {
		if !strings.HasPrefix(field, "--") || len(field) <= 2 {
			continue
		}
		name := strings.TrimPrefix(field, "--")
		if !defined[name] {
			t.Fatalf("init passes --%s but mcu-interface does not define it", name)
		}
	}

	// The specific pairing that broke: the owner flag must be gone.
	if strings.Contains(invocation, "playback-owner-executable") {
		t.Fatal("init still passes an amplifier owner; the restriction was removed")
	}
	if !strings.Contains(invocation, "playback-status") {
		t.Fatal("init no longer passes playback-status")
	}
}
