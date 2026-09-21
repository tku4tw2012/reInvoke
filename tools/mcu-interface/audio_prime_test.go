// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestZeroReaderSuppliesExactlyTheSilenceAsked proves the priming stream is
// as long as it claims. A reader that ran short would stop the pipeline early
// and put the hardware back in the state priming exists to avoid.
func TestZeroReaderSuppliesExactlyTheSilenceAsked(t *testing.T) {
	const want = 48000 * 2 * 2 * 3 // 3s of 48k stereo S16
	reader := &zeroReader{remaining: want}
	got, err := io.Copy(io.Discard, reader)
	if err != nil {
		t.Fatalf("reading silence: %v", err)
	}
	if got != want {
		t.Fatalf("silence was %d bytes, want %d", got, want)
	}
	if n, err := reader.Read(make([]byte, 16)); n != 0 || err != io.EOF {
		t.Fatalf("a spent reader returned %d bytes and %v", n, err)
	}

	// And it must actually be silent. A priming stream carrying anything
	// audible would be worse than the click it replaces.
	var buffer bytes.Buffer
	if _, err := io.Copy(&buffer, &zeroReader{remaining: 1024}); err != nil {
		t.Fatalf("reading silence: %v", err)
	}
	for index, b := range buffer.Bytes() {
		if b != 0 {
			t.Fatalf("priming sample %d is %d, not silence", index, b)
		}
	}
}

// TestPrimeOpensTheDonorsDevicesThenSilence proves the sequence matches
// alsa-init.sh: every softvol device opened to create its control, then a
// silence stream on music, which shares a dmix ipc_key with the device cues
// render through so the two mix rather than colliding.
func TestPrimeOpensTheDonorsDevicesThenSilence(t *testing.T) {
	directory := t.TempDir()
	record := filepath.Join(directory, "calls.txt")
	player := filepath.Join(directory, "aplay")
	script := "#!/bin/sh\necho \"$@\" >>" + record + "\ncat >/dev/null\nexit 0\n"
	if err := os.WriteFile(player, []byte(script), 0o755); err != nil {
		t.Fatalf("stage player: %v", err)
	}

	// Cancelled at the end of the test. The priming stream is deliberately
	// left running in the background for the service's benefit, so a test
	// that walks away from it leaks a process into every test that follows.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	primeAudioPath(ctx, "", player, "", nil)

	// The silence runs in the background on purpose, so wait for the file to
	// carry every call rather than assuming they have all landed.
	var lines []string
	for attempt := 0; attempt < 50; attempt++ {
		data, err := os.ReadFile(record)
		if err == nil {
			lines = strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) > len(primeDevices) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(lines) < len(primeDevices)+1 {
		t.Fatalf("prime made %d calls, want %d devices plus silence: %v",
			len(lines), len(primeDevices), lines)
	}

	for index, device := range primeDevices {
		if !strings.Contains(lines[index], "-D "+device) {
			t.Fatalf("call %d was %q, want device %s", index, lines[index], device)
		}
	}

	silence := lines[len(primeDevices)]
	if !strings.Contains(silence, "-D music") {
		t.Fatalf("the priming stream ran on %q, not music", silence)
	}
	for _, want := range []string{"-f S16_LE", "-r 48000", "-c 2"} {
		if !strings.Contains(silence, want) {
			t.Fatalf("the priming stream is missing %s: %q", want, silence)
		}
	}
}

// TestPrimeSurvivesAMissingPlayer proves priming cannot stop a boot. Every
// step is best effort: a device that will not open is one control created
// later instead, and no silence is the behaviour that existed before.
func TestPrimeSurvivesAMissingPlayer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	primeAudioPath(ctx, "", filepath.Join(t.TempDir(), "absent"), "", nil)
	primeAudioPath(ctx, "", "", "", nil)
}

// TestCuesRenderThroughTheMixedDevice proves cues do not go back to raw
// hardware. plughw opens hw:1,0 exclusively, so a cue could not share the
// card with the priming stream, and each cue started and stopped the
// hardware itself: that start was the click heard before the chime.
func TestCuesRenderThroughTheMixedDevice(t *testing.T) {
	if cueDevice == "" || strings.HasPrefix(cueDevice, "plughw") ||
		strings.HasPrefix(cueDevice, "hw:") {
		t.Fatalf("cues render through %q, which bypasses the mixer", cueDevice)
	}
	for _, device := range primeDevices {
		if device == cueDevice {
			return
		}
	}
	t.Fatalf("cues render through %q, which priming never opens", cueDevice)
}
