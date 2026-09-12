---
title: Privacy-gated microphone capture
description: Implemented RAM capture protocol, historical acceptance, and deferred synchronous privacy design
ms.date: 2026-09-12
---

## Scope

`reinvoke-mic-capture` exposes the Invoke microphone to local trusted services
without allowing those services to open the ALSA hardware node. Wake-word
recognition and remote assistant behavior are intentionally outside this
component.

The capture path is volatile. It writes no audio or configuration to NAND.

The accepted sample/capture measurements are from host-loaded RAM boots.
Native candidate 02 demonstrated Mic-Mute indicator changes only; its
microphone data path has not repeated those measurements.

The checked-in implementation uses a polled state-file gate. The stronger
synchronous authority protocol retained below is a design, not implemented
behavior and not a candidate 02 or candidate 03 acceptance claim.

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
A capture restart, including one triggered by a changed DSP process/socket,
creates a new generation and closes old clients. Ordinary mute drops captured
periods but does not itself close clients or advance the generation in the
checked-in implementation.

## Implemented privacy boundary

The MCU owns privacy policy and writes `/run/reinvoke/microphone-state`.
The capture service starts muted and polls that file every 100 ms. Missing,
invalid, oversized, or `muted` state causes periods to be discarded;
`unmuted` permits delivery after the next poll. It checks the DSP executable,
PID/start time, and microphone-socket identity every 250 ms and restarts capture
if that identity changes.

This is not a synchronous mute fence. Already queued records and the polling
interval prevent a guarantee of zero delivery immediately upon a mute request.
The service does not currently negotiate MCU authority epochs or prove an
ALSA reconfiguration drain. Historical zero-delivery tests describe their
measured windows, not an instantaneous, adversarial privacy guarantee.
See the implementation in [main.go](../tools/mic-capture/main.go),
[state.go](../tools/mic-capture/state.go), and
[hub.go](../tools/mic-capture/hub.go).

## Deferred synchronous privacy design

The following stronger design was previously written as though implemented.
It is retained to preserve that intent and the correction. `BLOCKED`, `DRAIN`,
`DRAINED`, authority epochs, and `ALLOW` are not part of the checked-in capture
protocol. Implementing and validating them would be separate work.

The MCU privacy controller remains the only privacy policy authority.
The capture owner cannot send DSP Mic-Mute commands directly.

After each ALSA configuration:

1. delivery remains blocked while the owner drains and discards input;
2. the owner connects to the MCU privacy authority;
3. the MCU synchronously fences all delivery and waits for `BLOCKED`;
4. the MCU forces DSP mute and waits for `EVENT_MIC_MUTE`;
5. the MCU requests `DRAIN`, and the owner consumes 64 consecutive all-zero
   native periods over at least 200 ms before returning `DRAINED`;
6. the MCU restores unmute only if the entry policy was known, requested
   unmuted, and had no pending mute; and
7. the MCU returns its random process-lifetime authority epoch and final state,
   then sends `ALLOW` only for confirmed unmute.

This synchronization creates a brief, honest privacy transition after capture
configuration. When the entry policy was unmuted, the red privacy indication
can appear while mute is confirmed and the buffered path is drained, then clear
after confirmed restore. The implementation does not hide a hardware mute from
the indication.

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

### Failure and restart behavior required by the deferred design

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
