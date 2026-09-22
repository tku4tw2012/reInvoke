---
title: reInvoke microphone capture owner
description: Build and client reference for the mic mute-gated local capture service
---

`reinvoke-mic-capture` is the packaged capture-PCM owner. It supervises the
pinned donor `arecord`, polls MCU mute state and serves channel 0 over a
root-only Unix socket. The [wire/mic mute contract](../../docs/microphone-capture.md)
is authoritative; synchronous per-delivery fencing is deferred, not implemented.
Capture and the mute gate were measured on the device on 2.2.7; a repeated
acceptance campaign has not been run.

## Runtime interface

Default input is `hw:1,0`, stereo `S32_LE`, 48 kHz, 256-frame periods and a
4,096-frame buffer. Output is mono `S32_LE`, without resampling; this does not
establish AEC or beamforming activation.

Default socket is `/run/reinvoke/mic-capture/audio.sock`. Each connection
starts with a 32-byte `RINVOMIC` version-1 header, followed by records containing
a 24-byte generation/sequence/timestamp header and 1,024 PCM bytes.
Numeric fields are little-endian; exact offsets are in [protocol.go](protocol.go).
Polled mute state does not fence every delivery; queued data and root bypass
remain part of the threat model.

## Build and test

Run from the repository root with the retained Ubuntu Go 1.18.1 toolchain and
fresh private outputs; the compiler is not in the public clone:

```bash
tools/mic-capture/build.sh --archive-root "${REINVOKE_ARCHIVE}" \
  --output "<artifact-dir>/reinvoke-mic-capture" \
  --client-output "<artifact-dir>/reinvoke-mic-capture-client"
tools/mic-capture/test.sh --archive-root "${REINVOKE_ARCHIVE}"
```

The build produces deterministic static ARMv7 binaries; tests run `go vet`
and `go test -race`.

## Target test client

Requires an existing target access channel. The running build serves a root
SSH login and USB ADB from a cold boot.

> [!WARNING]
> This captures microphone audio. Use an attended, consented test and private
> RAM-backed output, never raw audio in a public issue.

```bash
"<absolute-staged-client>" -socket /run/reinvoke/mic-capture/audio.sock \
  -duration 10s -output "<private-ram-output>"
```

The client emits JSON Lines generation/progress/summary records.
`-reconnect` waits through unavailable generations and reconnects; ordinary mute
does not create a new generation. Omit `-output` for statistics without retained
PCM. Build or host-test success is not native capture acceptance.
