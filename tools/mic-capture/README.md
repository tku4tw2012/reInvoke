---
title: reInvoke microphone capture owner
description: Build and protocol reference for the local privacy-gated microphone service
---

## Purpose

`reinvoke-mic-capture` is the only packaged owner of the Invoke capture PCM.
It supervises the checksum-gated donor ALSA `arecord` helper, applies the MCU
privacy-authority protocol, selects the donor voice-recognition channel, and
serves fixed-format records over a root-only Unix socket.

See [Privacy-gated microphone capture](../../docs/microphone-capture.md) for the
wire format, privacy contract, failure behavior, and threat model.

## Build

```bash
tools/mic-capture/build.sh \
  --output /tmp/reinvoke-mic-capture \
  --client-output /tmp/reinvoke-mic-capture-client
```

The build uses the archived Ubuntu Go 1.18.1 compiler and produces static ARMv7
binaries with deterministic flags.

## Test

```bash
tools/mic-capture/test.sh
```

The suite runs `go vet` and `go test -race`.

## Test client

On the target:

```bash
/tmp/reinvoke-mic-capture-client \
  -socket /run/reinvoke/mic-capture/audio.sock \
  -duration 10s \
  -output /tmp/microphone.s32le
```

The client prints JSON Lines generation, progress, and summary records. With
`-reconnect`, it waits through privacy and service generations and reconnects
when a new authorized stream appears.
