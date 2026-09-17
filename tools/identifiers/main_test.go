// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"math"
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

// TestAnswersDonorOOBEQuery guards the recovered mechanism that enables the
// radio. The donor calls com.harman.stateGet at startup and gates its entire
// enable path on the reply: wamp_on_pair returns immediately unless byte 0x95
// is set, and that byte is written only after the reply is matched against
// "system" then "normal".
//
// Confirmed live: answering produced "system state is normal" followed by
// "OOBE is finished, initializing...". Nothing else in this runtime provides
// the procedure, so dropping it leaves hci0 at 00:00:00:00:00:00.
func TestAnswersDonorOOBEQuery(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)

	if stateProcedure != "com.harman.stateGet" {
		t.Fatalf("the donor queries com.harman.stateGet, not %q", stateProcedure)
	}
	if !strings.Contains(text, `stateProcedure: {"system": map[string]interface{}{"state": "normal"}}`) &&
		!strings.Contains(text, `stateProcedure:                 {"system": map[string]interface{}{"state": "normal"}}`) {
		t.Fatal(`stateGet must answer {"system":{"state":"normal"}}; the donor ` +
			`matches the nested value against "system" then "normal"`)
	}

	// The reply must travel as kwargs. The observed working yield was
	// [YIELD, id, {}, [], {...}] with empty positional args.
	if !strings.Contains(text, "[]interface{}{}, result,") {
		t.Fatal("the yield must place the result in kwargs with empty args")
	}
}

// Numeric call arguments arrive through encoding/json, which has one number
// type and hands back float64. Rejecting that dropped every rotary volume
// change and the startup level, with the DSP left at its power-on gain, so
// this asserts the whole-number path and the guard around it.
func TestCallArgumentsSurviveJSONDecoding(t *testing.T) {
	var arguments []interface{}
	if err := json.Unmarshal([]byte("[5]"), &arguments); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := arguments[0].(float64); !ok {
		t.Fatalf("expected encoding/json to yield float64, got %T", arguments[0])
	}

	encoded, err := encodeMessagePack(arguments)
	if err != nil {
		t.Fatalf("encode whole number: %v", err)
	}
	// One-element array holding a positive fixint.
	if want := []byte{0x91, 0x05}; !bytes.Equal(encoded, want) {
		t.Errorf("encoded = % x, want % x", encoded, want)
	}

	// Fractions, non-finite values and anything encoding/json has already
	// rounded must be refused rather than encoded as a different number.
	for _, bad := range []interface{}{
		2.5,
		math.Inf(1),
		math.Inf(-1),
		math.NaN(),
		float64(1 << 53 + 2),
		-float64(1 << 53 + 2),
	} {
		if _, err := encodeMessagePack([]interface{}{bad}); err == nil {
			t.Errorf("expected %v to be rejected", bad)
		}
	}

	// Negative whole numbers still have to encode.
	if _, err := encodeMessagePack([]interface{}{-5.0}); err != nil {
		t.Errorf("encode negative whole number: %v", err)
	}
}
