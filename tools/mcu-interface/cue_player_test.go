// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// buildWAV assembles a minimal PCM WAV for the decoder tests.
func buildWAV(channels, rate, bits int, samples []byte) []byte {
	out := []byte("RIFF")
	out = append(out, 0, 0, 0, 0)
	out = append(out, []byte("WAVE")...)
	fmtChunk := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmtChunk[0:2], 1)
	binary.LittleEndian.PutUint16(fmtChunk[2:4], uint16(channels))
	binary.LittleEndian.PutUint32(fmtChunk[4:8], uint32(rate))
	binary.LittleEndian.PutUint32(fmtChunk[8:12], uint32(rate*channels*bits/8))
	binary.LittleEndian.PutUint16(fmtChunk[12:14], uint16(channels*bits/8))
	binary.LittleEndian.PutUint16(fmtChunk[14:16], uint16(bits))
	out = append(out, []byte("fmt ")...)
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, uint32(len(fmtChunk)))
	out = append(out, size...)
	out = append(out, fmtChunk...)
	out = append(out, []byte("data")...)
	binary.LittleEndian.PutUint32(size, uint32(len(samples)))
	out = append(out, size...)
	out = append(out, samples...)
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))
	return out
}

func pcm16(values ...int16) []byte {
	out := make([]byte, 0, len(values)*2)
	for _, v := range values {
		pair := make([]byte, 2)
		binary.LittleEndian.PutUint16(pair, uint16(v))
		out = append(out, pair...)
	}
	return out
}

// TestDecodeWAVAcceptsTheDonorShape proves the donor cue format decodes: the
// device cues are 16-bit mono PCM at 22050 Hz.
func TestDecodeWAVAcceptsTheDonorShape(t *testing.T) {
	decoded, err := decodeWAV(buildWAV(1, 22050, 16, pcm16(1, -1, 300)))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.channels != 1 || decoded.sampleRate != 22050 || decoded.bits != 16 {
		t.Fatalf("decoded %+v", decoded)
	}
	if len(decoded.samples) != 6 {
		t.Fatalf("sample bytes = %d, want 6", len(decoded.samples))
	}
}

// TestDecodeWAVRefusesWhatItCannotPlay proves unsupported shapes are refused
// rather than played as if they were something else.
func TestDecodeWAVRefusesWhatItCannotPlay(t *testing.T) {
	cases := map[string][]byte{
		"not a riff":    []byte("this is not audio at all"),
		"empty":         {},
		"24 bit":        buildWAV(1, 22050, 24, pcm16(1, 2, 3)),
		"four channels": buildWAV(4, 22050, 16, pcm16(1, 2, 3, 4)),
		"absurd rate":   buildWAV(1, 5, 16, pcm16(1, 2)),
		"no data chunk": buildWAV(1, 22050, 16, nil)[:36],
	}
	for name, data := range cases {
		if _, err := decodeWAV(data); err == nil {
			t.Fatalf("decodeWAV accepted %s", name)
		}
	}
}

// TestScaleSamplesAttenuatesExactly proves scaling down is exact and that a
// zero gain silences rather than leaving residue.
//
// It deliberately does not claim to exercise the saturation clamps in
// scaleSamples. cueGain never returns more than 1 and the function returns
// early at 1, so scaling only ever reduces and those clamps cannot be reached.
// They are kept as cheap insurance against a future caller, not as behaviour
// this test covers: a test that cannot fail proves nothing.
func TestScaleSamplesAttenuatesExactly(t *testing.T) {
	samples := pcm16(32767, -32768, 1000, -1000)
	scaleSamples(samples, 0.5)
	got := []int16{}
	for i := 0; i+1 < len(samples); i += 2 {
		got = append(got, int16(binary.LittleEndian.Uint16(samples[i:i+2])))
	}
	want := []int16{16383, -16384, 500, -500}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sample %d = %d, want %d", i, got[i], want[i])
		}
	}

	// A gain of zero must silence rather than leave residue.
	silent := pcm16(32767, -32768)
	scaleSamples(silent, 0)
	for _, b := range silent {
		if b != 0 {
			t.Fatal("zero gain left audible samples")
		}
	}
}

// TestCueGainNormalisesByPeak proves a quiet cue and a mastered one end up at
// the same level. The donor's own cues range from 5 percent of full scale to
// 100 percent, so a single gain would make some inaudible and others painful.
func TestCueGainNormalisesByPeak(t *testing.T) {
	quiet := cueGain(100, 1703) // S_301_d_micoff, 5 percent of full scale
	loud := cueGain(100, 32767) // S_311_d_pluggedin, mastered to full scale
	quietPeak := 1703 * quiet
	loudPeak := 32767 * loud
	if diff := quietPeak - loudPeak; diff > 1 || diff < -1 {
		t.Fatalf("normalised peaks differ: %.1f vs %.1f", quietPeak, loudPeak)
	}

	// Volume scales the target, below the level where the clamp takes over.
	// Above that the samples are already as loud as they may safely be and
	// only the DSP carries further volume.
	below := int(cueMaxPeak*100/cueTargetPeak) / 2
	if half := cueGain(below, 32767); half >= cueGain(below*2, 32767) {
		t.Fatal("halving the volume did not lower the gain")
	}
	if cueGain(0, 32767) != 0 {
		t.Fatal("volume zero should be silent")
	}
	// Scaling never asks for more than a sample can hold, at any volume or
	// any source peak, so boosting a quiet cue cannot clip.
	for _, volume := range []int{1, 5, 20, 50, 100} {
		for _, peak := range []int{10, 1703, 20016, 32767} {
			if scaled := float64(peak) * cueGain(volume, peak); scaled > 32767 {
				t.Fatalf("volume %d on a %d peak asks for %.0f", volume, peak, scaled)
			}
		}
	}
}

// TestCueGainLiftsAQuietCue proves normalisation works upward as well as down.
//
// The gain was capped at one in two places, so a donor cue mastered at five
// percent of full scale stayed there while a mastered one was brought down to
// meet it. That is attenuation, not normalisation, and it only looked right
// while the target sat below every cue's own peak.
func TestCueGainLiftsAQuietCue(t *testing.T) {
	const quietPeak = 1703 // S_301_d_micoff, 5 percent of full scale
	gain := cueGain(5, quietPeak)
	if gain <= 1 {
		t.Fatalf("gain %v leaves a five percent cue where it was", gain)
	}
	lifted := float64(quietPeak) * gain
	if lifted > 32767 {
		t.Fatalf("lifting a quiet cue asks for %.0f", lifted)
	}
	// And the samples must actually move, not just the number.
	samples := pcm16(quietPeak, -quietPeak)
	scaleSamples(samples, gain)
	if samplePeak(samples) <= quietPeak {
		t.Fatal("scaleSamples ignored a gain above one")
	}
}

// TestPlayRefusesNamesOutsideTheCueDirectory proves a cue name cannot reach
// another part of the filesystem.
func TestPlayRefusesNamesOutsideTheCueDirectory(t *testing.T) {
	player := &cuePlayer{directory: t.TempDir(), player: "/bin/true"}
	for _, name := range []string{"../escape", "/etc/passwd", "", "a/b"} {
		if err := player.Play(context.Background(), name); err == nil {
			t.Fatalf("Play accepted %q", name)
		}
	}
}

// TestPlayReportsAMissingCue proves a cue that is not installed is an error
// rather than silence that looks like success.
func TestPlayReportsAMissingCue(t *testing.T) {
	player := &cuePlayer{directory: t.TempDir(), player: "/bin/true"}
	if err := player.Play(context.Background(), "not_installed"); err == nil {
		t.Fatal("Play reported success for a missing cue")
	}
}

// TestPlayRendersThroughTheConfiguredPlayer proves the samples reach the
// renderer, using a stub that records what it was given.
func TestPlayRendersThroughTheConfiguredPlayer(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "rendered")
	stub := filepath.Join(dir, "player.sh")
	if err := os.WriteFile(stub,
		[]byte("#!/bin/sh\ncat > "+record+"\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cue.wav"),
		buildWAV(1, 22050, 16, pcm16(20000, -20000, 10000)), 0o644); err != nil {
		t.Fatalf("write cue: %v", err)
	}
	player := &cuePlayer{
		directory: dir,
		player:    stub,
		volume:    func() int { return 100 },
	}
	if err := player.Play(context.Background(), "cue"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	rendered, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("renderer wrote nothing: %v", err)
	}
	if len(rendered) != 6 {
		t.Fatalf("renderer received %d bytes, want 6", len(rendered))
	}
	// The loudest sample must sit at whatever was asked for, which at full
	// volume is the clamp rather than the target: cueTargetPeak is calibrated
	// against the DSP gain this unit boots at, and at volume 100 it asks for
	// far more than a sample can hold.
	want := cueTargetPeak
	if want > cueMaxPeak {
		want = cueMaxPeak
	}
	peak := samplePeak(rendered)
	if peak > int(want)+1 || peak < int(want)-1 {
		t.Fatalf("rendered peak %d, want about %v", peak, want)
	}
}

// TestWaitAppliedBlocksUntilTheDSPAccepts proves a startup cue waits for the
// audio path rather than assuming it.
//
// The DSP sits between the DAC and the speaker and registers its procedures
// several seconds after this service starts. On hardware the boot cue rendered
// at 32 seconds while the DSP had not accepted a volume: the amplifier and DAC
// were open, the samples were scaled correctly, the renderer exited cleanly,
// the log said CUE_PLAYED, and nothing was audible.
func TestWaitAppliedBlocksUntilTheDSPAccepts(t *testing.T) {
	controller, err := newDSPVolumeController("/tmp/does-not-matter")
	if err != nil {
		t.Fatalf("controller: %v", err)
	}
	// Nothing has been applied, so a waiter must time out rather than proceed.
	if controller.WaitApplied(context.Background(), 50*time.Millisecond) {
		t.Fatal("WaitApplied returned true before the DSP accepted anything")
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		controller.markApplied()
	}()
	if !controller.WaitApplied(context.Background(), 2*time.Second) {
		t.Fatal("WaitApplied did not observe the DSP accepting a level")
	}
	// It stays satisfied once applied.
	if !controller.WaitApplied(context.Background(), 50*time.Millisecond) {
		t.Fatal("WaitApplied forgot that the DSP had accepted")
	}
}

// TestMarkAppliedIsIdempotent proves repeated success does not panic on an
// already closed channel. Every accepted volume marks the path applied.
func TestMarkAppliedIsIdempotent(t *testing.T) {
	controller, err := newDSPVolumeController("/tmp/does-not-matter")
	if err != nil {
		t.Fatalf("controller: %v", err)
	}
	controller.markApplied()
	controller.markApplied()
	controller.markApplied()
	if !controller.WaitApplied(context.Background(), time.Second) {
		t.Fatal("WaitApplied did not observe the mark")
	}
}

// TestWaitAppliedHonoursCancellation proves a cancelled startup does not hold
// a cue waiting for a DSP that is never coming.
func TestWaitAppliedHonoursCancellation(t *testing.T) {
	controller, err := newDSPVolumeController("/tmp/does-not-matter")
	if err != nil {
		t.Fatalf("controller: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if controller.WaitApplied(ctx, 10*time.Second) {
		t.Fatal("WaitApplied ignored a cancelled context")
	}
}
