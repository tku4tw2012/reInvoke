// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestRegisteredProceduresDisjointFromMCU guards a defect observed on
// hardware in Candidate 05.2: this service registered com.harman.volumeGet,
// which reinvoke-mcu-interface also owns. Whichever service registers first
// wins, so mcu-interface failed registration and reconnected every five
// seconds forever. Every physical control was dead while the LEDs still
// animated, which reads as a hardware fault rather than a naming conflict.
//
// Candidate 05 masked this because its identity provider crash-looped and
// never registered anything at all.
func TestRegisteredProceduresDisjointFromMCU(t *testing.T) {
	ours := proceduresIn(t, "main.go")
	if len(ours) == 0 {
		t.Fatal("no procedures found in this service; the extraction is wrong")
	}

	mcuPath := filepath.Join("..", "mcu-interface", "wamp.go")
	if _, err := os.Stat(mcuPath); err != nil {
		t.Skipf("mcu-interface source unavailable: %v", err)
	}
	theirs := proceduresIn(t, mcuPath)
	if len(theirs) == 0 {
		t.Fatal("no procedures found in mcu-interface; the extraction is wrong")
	}

	// Prove the extraction actually works before trusting a clean result:
	// a name mcu-interface certainly owns must be present in its set.
	if !theirs["com.harman.volumeGet"] {
		t.Fatal("extraction failed: mcu-interface must own com.harman.volumeGet")
	}

	for name := range ours {
		if theirs[name] {
			t.Fatalf("%s is owned by mcu-interface; registering it here makes "+
				"mcu-interface reconnect forever and kills every physical control", name)
		}
	}
}

func proceduresIn(t *testing.T, path string) map[string]bool {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`"(com\.harman\.[A-Za-z.-]+)"`)
	found := map[string]bool{}
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		// Comments name procedures to explain why they are excluded; counting
		// them would invert the check and fail on the very fix it guards.
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		for _, match := range pattern.FindAllStringSubmatch(line, -1) {
			found[match[1]] = true
		}
	}
	return found
}
