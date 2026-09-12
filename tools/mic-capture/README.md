---
title: reInvoke microphone capture owner
description: Build and protocol reference for the local privacy-gated microphone service
---

## Purpose

`reinvoke-mic-capture` is the only packaged owner of the Invoke capture PCM.
It supervises the checksum-gated donor ALSA `arecord` helper, polls the MCU
privacy state file, selects the donor voice-recognition channel, and
serves fixed-format records over a root-only Unix socket.

See [Privacy-gated microphone capture](../../docs/microphone-capture.md) for the
wire format, privacy contract, failure behavior, and threat model.
The synchronous authority/fence protocol described there is deferred, not
implemented. Hardware capture acceptance is historical RAM evidence; candidate
02 has not repeated native data-path/privacy acceptance.

## Build

```bash
tools/mic-capture/build.sh \
  --output <archive>/build/artifacts/reinvoke-mic-capture \
  --client-output <archive>/build/artifacts/reinvoke-mic-capture-client
```

The build uses the archived Ubuntu Go 1.18.1 compiler and produces static ARMv7
binaries with deterministic flags. That private toolchain must already exist;
the public clone does not contain it.

## Test

```bash
tools/mic-capture/test.sh
```

The suite runs `go vet` and `go test -race`.

## Test client

On the target:

```bash
<staged-client> \
  -socket /run/reinvoke/mic-capture/audio.sock \
  -duration 10s \
  -output <private-ram-output>
```

The client prints JSON Lines generation, progress, and summary records. With
`-reconnect`, it waits through unavailable service generations and reconnects
when a new stream appears. Ordinary mute does not itself create a new
generation. Capture is an attended, separately approved action, not an
instruction to collect microphone audio during an offline build.
