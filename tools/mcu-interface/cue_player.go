// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

// Device cues: the short sounds the speaker makes about itself.
//
// The donor played these from audio-ui through aui::g_alert_player, separate
// from music. Its own asset-to-state table pairs each cue with the state that
// triggers it, for example S_311_d_pluggedin with system:booting, which is the
// sound this speaker made when it finished starting up.
//
// The cue is rendered to the hardware device rather than to the music PCM,
// because the music PCM is created by the donor stack and only exists while
// that stack is running; a boot cue has to play before it. That also means the
// softvol control the user's volume rides on is not in the path, so the
// samples are scaled here instead. Played unscaled on this unit the result was
// reported as "extremely loud", which is what a raw DAC write sounds like.
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	// cueMaxBytes bounds what will be read as a cue. The longest donor device
	// cue is S_311_d_pluggedin at 4.2 seconds.
	cueMaxBytes = 2 * 1024 * 1024
	// cueTimeout bounds a single playback so a stuck render cannot hold the
	// device open.
	cueTimeout = 15 * time.Second
)

// The cue levels below are measured from the shipped files, not chosen.
//
// Power_On 20016, BT_Pairing 12958, BT_Connected 10806, Volume_Max 10505.
// That 5.6 dB spread is the donor's own mastering and it is meaningful: the
// startup fanfare is supposed to be louder than the blip that says the dial
// will not go further. One shared gain keeps it.
func dialAmplitude(percent int) float64 {
	if percent <= 0 {
		return 0
	}
	if percent > 100 {
		percent = 100
	}
	const rangeDB = 38.0
	return math.Pow(10, (-rangeDB+rangeDB*float64(percent)/100)/20)
}

// cueReferenceDial and cueReferenceGain anchor the curve where the level was
// measured: on 2026-09-19 Power_On at dial 34 peaked at 10008 and was judged
// right, and that file peaks at 20016.
const cueReferenceDial = 34
const cueReferenceGain = 10008.0 / 20016.0

// cueLoudestPeak is the loudest of the shipped cues, which is what the gain
// ceiling has to be computed against so that holding one gain for every cue
// still cannot clip the loudest one.
const cueLoudestPeak = 20016.0

// cueMaxPeak is the loudest a scaled cue may be, short of full scale so the
// arithmetic cannot clip.
const cueMaxPeak = 29000.0

type cuePlayer struct {
	directory string
	player    string
	loader    string
	libPath   string
	logf      func(string, ...interface{})

	// volume reports the level a cue should play at, 0 through 100.
	volume func() int

	mu      sync.Mutex
	playing context.CancelFunc
}

// wavPCM is the decoded body of a cue.
type wavPCM struct {
	channels   int
	sampleRate int
	bits       int
	samples    []byte
}

// decodeWAV reads a PCM WAV. Only the shape the donor cues actually use is
// accepted; anything else is refused rather than guessed at.
func decodeWAV(data []byte) (wavPCM, error) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return wavPCM{}, errors.New("not a RIFF/WAVE file")
	}
	var out wavPCM
	seenFormat := false
	offset := 12
	for offset+8 <= len(data) {
		id := string(data[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		body := offset + 8
		if size < 0 || body+size > len(data) {
			return wavPCM{}, errors.New("chunk runs past the end of the file")
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return wavPCM{}, errors.New("short format chunk")
			}
			if format := binary.LittleEndian.Uint16(data[body : body+2]); format != 1 {
				return wavPCM{}, fmt.Errorf("unsupported WAV format %d, expected PCM", format)
			}
			out.channels = int(binary.LittleEndian.Uint16(data[body+2 : body+4]))
			out.sampleRate = int(binary.LittleEndian.Uint32(data[body+4 : body+8]))
			out.bits = int(binary.LittleEndian.Uint16(data[body+14 : body+16]))
			seenFormat = true
		case "data":
			if !seenFormat {
				return wavPCM{}, errors.New("data chunk before format chunk")
			}
			out.samples = data[body : body+size]
			if out.bits != 16 {
				return wavPCM{}, fmt.Errorf("unsupported sample width %d", out.bits)
			}
			if out.channels < 1 || out.channels > 2 {
				return wavPCM{}, fmt.Errorf("unsupported channel count %d", out.channels)
			}
			if out.sampleRate < 8000 || out.sampleRate > 48000 {
				return wavPCM{}, fmt.Errorf("unsupported sample rate %d", out.sampleRate)
			}
			return out, nil
		}
		offset = body + size
		if size%2 == 1 {
			offset++
		}
	}
	return wavPCM{}, errors.New("no data chunk")
}

// scaleSamples applies a gain to 16-bit little-endian samples in place.
//
// Saturating rather than wrapping matters: a wrapped sample is a full-scale
// sign flip, which is exactly the click a speaker should never be asked to
// reproduce.
func scaleSamples(samples []byte, gain float64) {
	// Gains above one are applied, not ignored.
	//
	// This used to return early on any gain of one or more, which quietly
	// undid half of the normalisation: cueGain could ask for a quiet cue to
	// be brought up and nothing here would do it. The clamp below is what
	// keeps that safe, and cueGain never asks for more than a sample can
	// hold anyway.
	if gain == 1 {
		return
	}
	if gain <= 0 {
		for i := range samples {
			samples[i] = 0
		}
		return
	}
	for i := 0; i+1 < len(samples); i += 2 {
		value := float64(int16(binary.LittleEndian.Uint16(samples[i : i+2])))
		scaled := value * gain
		if scaled > 32767 {
			scaled = 32767
		}
		if scaled < -32768 {
			scaled = -32768
		}
		binary.LittleEndian.PutUint16(samples[i:i+2], uint16(int16(scaled)))
	}
}

// samplePeak reports the largest absolute sample value in a 16-bit buffer.
func samplePeak(samples []byte) int {
	peak := 0
	for i := 0; i+1 < len(samples); i += 2 {
		value := int(int16(binary.LittleEndian.Uint16(samples[i : i+2])))
		if value < 0 {
			value = -value
		}
		if value > peak {
			peak = value
		}
	}
	return peak
}

// cueGain scales every cue by the same amount for a given dial position, so
// the levels the donor mastered into the files survive.
//
// It used to scale each cue to a common target peak, which is normalisation,
// and normalisation throws away exactly the information the donor put there.
// Measured on the shipped files: Power_On peaks at 20016 and Volume_Max at
// 10505, a deliberate 5.6 dB gap between a startup fanfare and a "that is as
// loud as it goes" blip. Driving both to one peak erased that gap and lifted
// Volume_Max by 9.5 dB at the top of the dial, which the owner heard as a
// loud bing on reaching full volume.
func cueGain(volume int, peak int) float64 {
	if volume <= 0 || peak <= 0 {
		return 0
	}
	if volume > 100 {
		volume = 100
	}
	// Anchored where the level was actually measured: at dial 34 Power_On
	// peaked at 10008 and was judged right, and that file peaks at 20016, so
	// the gain at that point is 0.5. Everything else follows the dial curve
	// from there, which keeps the cue tracking the same decibels as music.
	gain := cueReferenceGain *
		dialAmplitude(volume) / dialAmplitude(cueReferenceDial)
	// The ceiling has to be one gain for all cues, not a per-cue target,
	// or the relative levels come straight back apart at the top. Hold it
	// where the loudest shipped cue still fits.
	if maximum := cueMaxPeak / cueLoudestPeak; gain > maximum {
		gain = maximum
	}
	return gain
}

// Play renders one named cue. A cue already playing is cancelled first: these
// are status sounds, and the newest one is the one worth hearing.
func (player *cuePlayer) Play(parent context.Context, name string) error {
	if player == nil || player.directory == "" {
		return nil
	}
	if filepath.Base(name) != name || name == "" {
		return fmt.Errorf("invalid cue name %q", name)
	}
	path := filepath.Join(player.directory, name+".wav")
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cue %s is not installed: %w", name, err)
	}
	if info.Size() > cueMaxBytes {
		return fmt.Errorf("cue %s is %d bytes, over the limit", name, info.Size())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read cue %s: %w", name, err)
	}
	decoded, err := decodeWAV(raw)
	if err != nil {
		return fmt.Errorf("decode cue %s: %w", name, err)
	}

	level := 100
	if player.volume != nil {
		level = player.volume()
	}
	gain := cueGain(level, samplePeak(decoded.samples))
	if gain <= 0 {
		if player.logf != nil {
			player.logf("CUE_SKIPPED %s: volume %d gives no gain", name, level)
		}
		return nil
	}
	scaleSamples(decoded.samples, gain)

	ctx, cancel := context.WithTimeout(parent, cueTimeout)
	player.mu.Lock()
	if player.playing != nil {
		player.playing()
	}
	player.playing = cancel
	player.mu.Unlock()
	defer func() {
		cancel()
		player.mu.Lock()
		if player.playing != nil {
			player.playing = nil
		}
		player.mu.Unlock()
	}()

	// The renderer reads raw samples on standard input so the format is stated
	// rather than re-parsed, and so the scaled buffer never reaches the disk.
	args := []string{
		"-D", "plughw:1,0",
		"-f", "S16_LE",
		"-r", strconv.Itoa(decoded.sampleRate),
		"-c", strconv.Itoa(decoded.channels),
		"-q", "-",
	}
	var command *exec.Cmd
	if player.loader != "" {
		command = exec.CommandContext(ctx, player.loader,
			append([]string{player.player}, args...)...)
	} else {
		command = exec.CommandContext(ctx, player.player, args...)
	}
	if player.libPath != "" {
		command.Env = append(os.Environ(), "LD_LIBRARY_PATH="+player.libPath)
	}
	command.Stdin = bytesReader(decoded.samples)
	if err := command.Run(); err != nil {
		return fmt.Errorf("play cue %s: %w", name, err)
	}
	return nil
}

// PlayAsync renders a cue without blocking the caller. Cues accompany events
// like a button press or a state change, and none of those should wait on
// audio.
func (player *cuePlayer) PlayAsync(parent context.Context, name string) {
	if player == nil || player.directory == "" {
		return
	}
	go func() {
		err := player.Play(parent, name)
		if player.logf == nil {
			return
		}
		if err != nil {
			player.logf("CUE_FAILED %s: %v", name, err)
			return
		}
		player.logf("CUE_PLAYED %s", name)
	}()
}

// bytesReader adapts a byte slice to the reader the renderer reads from.
func bytesReader(data []byte) *bytes.Reader { return bytes.NewReader(data) }
