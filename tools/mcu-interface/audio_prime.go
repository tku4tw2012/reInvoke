// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"time"
)

// The donor primes the audio path before anything is meant to be heard. Its
// /usr/bin/alsa-init.sh does three things:
//
//	for softvol in system music timer voice call
//	do
//	        echo -n | aplay -D ${softvol} 2>/dev/null || true
//	done
//	arecord -D dsp_mic -f S32_LE -r 48000 -d 1 &>/dev/null &
//	aplay -Dmusic /system/data/silence_3sec.wav &
//
// The loop opens each device with empty input, which is what creates its
// softvol control: ALSA does not add the element until the plugin is first
// instantiated. That is why the donor has a "music" control from boot and
// this runtime only grew one after something had played.
//
// The last line is the part that matters here. Three seconds of silence
// starts the pipeline while nothing is meant to be audible, so the first real
// sound plays into hardware that is already running. Without it every cue
// started and stopped the card, and the owner heard a click immediately
// before the startup chime on a cold boot. It was never reproducible at
// runtime, because by then something had already started the pipeline.
//
// The capture line is not copied. Microphone capture is its own service here
// and starting a second reader would contend with it.

// primeDuration is how long the priming stream runs. The donor's file is
// three seconds; this generates the silence rather than shipping a file,
// because the length only has to cover the gap between priming and the first
// cue, and generating it keeps the runtime free of an audio asset whose only
// content is zeroes.
const primeDuration = 3 * time.Second

// primeRate and primeChannels match the hardware so the plug layer has no
// conversion to set up while the stream is starting.
const (
	primeRate     = 48000
	primeChannels = 2
)

// primeDevices are opened with empty input to create their softvol controls,
// in the donor's own order.
var primeDevices = []string{"system", "music", "timer", "voice", "call"}

// primeAudioPath starts the audio pipeline so that later sounds do not have
// to. It returns once the priming stream is running, not once it has
// finished: the point is for it to still be running when the caller unmutes
// the outputs and renders the first cue.
//
// Every step is best effort. A device that cannot be opened is one control
// that will be created later instead, and a priming stream that fails to
// start leaves exactly the behaviour that existed before this ran. Neither is
// worth refusing to boot over, so failures are logged and passed over.
func primeAudioPath(
	ctx context.Context,
	loader string,
	player string,
	libPath string,
	logf func(string, ...interface{}),
) {
	if player == "" {
		return
	}

	run := func(args []string, stdin bool) *exec.Cmd {
		var command *exec.Cmd
		if loader != "" {
			command = exec.CommandContext(ctx, loader,
				append([]string{player}, args...)...)
		} else {
			command = exec.CommandContext(ctx, player, args...)
		}
		if libPath != "" {
			command.Env = append(os.Environ(), "LD_LIBRARY_PATH="+libPath)
		}
		if stdin {
			command.Stdin = emptyReader{}
		}
		return command
	}

	for _, device := range primeDevices {
		command := run([]string{"-D", device, "-q", "-"}, true)
		if err := command.Run(); err != nil && logf != nil {
			logf("prime %s: %v", device, err)
		}
	}

	// Silence on the music device, which shares its dmix ipc_key with the
	// device cues render through, so the two mix rather than colliding.
	frames := primeRate * primeChannels * 2 * int(primeDuration/time.Second)
	silence := run([]string{
		"-D", "music",
		"-f", "S16_LE",
		"-r", "48000",
		"-c", "2",
		"-q", "-",
	}, false)
	silence.Stdin = &zeroReader{remaining: frames}
	if err := silence.Start(); err != nil {
		if logf != nil {
			logf("prime silence: %v", err)
		}
		return
	}
	if logf != nil {
		logf("AUDIO_PRIMED %s of silence", primeDuration)
	}
	go func() { _ = silence.Wait() }()
}

// emptyReader is an immediately empty stdin, so `aplay -D dev -` opens the
// device and exits rather than waiting for samples.
type emptyReader struct{}

func (emptyReader) Read([]byte) (int, error) { return 0, io.EOF }

// zeroReader supplies a bounded run of silence without allocating it.
type zeroReader struct{ remaining int }

func (r *zeroReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > r.remaining {
		n = r.remaining
	}
	for i := range p[:n] {
		p[i] = 0
	}
	r.remaining -= n
	return n, nil
}
