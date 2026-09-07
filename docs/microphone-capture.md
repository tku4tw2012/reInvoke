---
title: Privacy-gated microphone capture
description: Local capture-owner protocol, privacy contract, and audio format
---

## Scope

`reinvoke-mic-capture` exposes the Invoke microphone to local trusted services
without allowing those services to open the ALSA hardware node. Wake-word
recognition and remote assistant behavior are intentionally outside this
component.

The capture path is volatile. It writes no audio or configuration to NAND.

## Audio source

The hardware PCM is card 1, device 0. The donor calls it `dsp_mic` and routes
its channels as:

* left: voice recognition; and
* right: call audio.

The physical seven-microphone array is not exposed as seven ALSA channels.
Linux receives a two-channel stream that the donor explicitly describes as
"Audio in from DSP mic", and DSP Mic-Mute changes that stream to all-zero
samples.

The platform does not claim that beamforming, acoustic echo cancellation, AGC,
or noise reduction is active. Product material and donor configuration show
that those capabilities were intended, but no retained A/B capture proves
which algorithms the normal DSP route enables.

## Delivered format

The owner opens the native endpoint as:

```text
device: hw:1,0
rate: 48000 Hz
hardware channels: 2
hardware format: S32_LE
period: 256 frames / 2048 bytes
buffer: 4096 frames / 16 periods / 32768 bytes
```

It selects the left donor voice-recognition channel and delivers:

```text
rate: 48000 Hz
channels: 1
format: S32_LE
frames per record: 256
payload bytes per record: 1024
record interval: approximately 5.33 ms
```

The owner performs no sample-rate or sample-width conversion. Consumers differ
in their wake-word input requirements, so conversion belongs in the consumer.
Channel selection stays in the platform because the donor defines the channel
semantics.

## Stream protocol

The root-only Unix socket is:

```text
/run/reinvoke/mic-capture/audio.sock
```

Each connection begins with one 32-byte little-endian header:

| Offset | Size | Meaning |
|---:|---:|---|
| 0 | 8 | ASCII `RINVOMIC` |
| 8 | 2 | protocol version, currently 1 |
| 10 | 2 | header length, 32 |
| 12 | 4 | sample rate, 48000 |
| 16 | 2 | channels, 1 |
| 18 | 2 | format, 1 = `S32_LE` |
| 20 | 4 | frames per record, 256 |
| 24 | 8 | capture generation |

Every record is exactly 1,048 bytes:

| Offset | Size | Meaning |
|---:|---:|---|
| 0 | 8 | capture generation |
| 8 | 8 | monotonically increasing sequence |
| 16 | 8 | service wall-clock timestamp in nanoseconds |
| 24 | 1024 | 256 mono `S32_LE` samples |

Unix streams do not preserve write boundaries. Consumers must use exact-length
reads. EOF in the middle of a header or record invalidates that partial data.
A generation never continues across mute, DSP restart, capture restart, or MCU
privacy-authority restart.

## Privacy contract

The MCU privacy controller remains the only privacy policy authority.
The capture owner cannot send DSP Mic-Mute commands directly.

After each ALSA configuration:

1. delivery remains blocked while the owner drains and discards input;
2. the owner connects to the MCU privacy authority;
3. the MCU synchronously fences all delivery and waits for `BLOCKED`;
4. the MCU forces DSP mute and waits for `EVENT_MIC_MUTE`;
5. the MCU restores unmute only if the entry policy was known, requested
   unmuted, and had no pending mute; and
6. the MCU returns its random process-lifetime authority epoch and final state.

For an ordinary physical or API mute, the MCU fences the capture owner before
persisting `muted` or sending the DSP command. If the owner cannot acknowledge,
the MCU verifies and terminates that exact process generation before continuing
the hardware mute.

The state file is a secondary fail-closed signal:

```text
/run/reinvoke/microphone-state
```

Missing, invalid, oversized, or `muted` state blocks delivery. `unmuted` alone
does not authorize capture; the current PCM, DSP, and MCU authority generations
must also match the completed synchronization.

Bytes already copied into a consumer's process or kernel receive queue cannot
be revoked by any IPC design. The owner keeps queues shallow, closes all
connections at the synchronous fence, and guarantees that it queues no new
record after returning `BLOCKED`.

## Failure and restart behavior

The capture owner starts blocked. It closes every client and restarts its
capture generation when:

* the ALSA helper exits;
* the DSP process, process-start time, or mic-control socket inode changes;
* the MCU privacy process or process-start time changes;
* the MCU authority epoch changes;
* the authority connection closes or sends invalid protocol; or
* privacy state becomes missing, invalid, or muted.

The owner configures ALSA again after a DSP restart and repeats the entire
privacy synchronization. No old connection survives into the new generation.

## Threat model

The one ALSA capture substream gives the owner runtime exclusivity while it is
open. The data socket admits UID 0 only.

Root is trusted. A hostile root process can kill the owner and open the raw
device, so this is an operational ownership boundary rather than protection
from a compromised root account. Unprivileged services must not receive raw
sound-device access or capabilities that bypass the owner.
