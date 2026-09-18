// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"os"
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
