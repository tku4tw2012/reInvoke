---
title: Privacy-gated microphone capture
description: Audio format, stream protocol, polled privacy gate and measured limits
ms.date: 2026-09-12
ms.topic: concept
---

`reinvoke-mic-capture` supplies trusted local consumers without giving them
raw ALSA access. Audio and configuration remain volatile; the service writes
neither to NAND. Wake-word recognition and assistant behavior are consumer
work, not capture features.

The implemented gate polls MCU-owned state. Native capture/privacy acceptance
is open; the measurements below are from RAM boots. See the
[current native ledger](native-nand-platform.md#current-result), not indicator
changes or Bluetooth connection, for candidate acceptance.

## Audio source

The hardware endpoint is `hw:1,0`, called `dsp_mic` by the donor.
The donor routes left to voice recognition and right to call audio.
The seven physical microphones are not exposed as seven raw ALSA channels.
Linux receives stereo "Audio in from DSP mic"; DSP Mic-Mute produced all-zero
samples in historical RAM measurements.

DSP part identity and physical microphone wiring remain unresolved.
No retained A/B capture proves beamforming, AEC, AGC or noise-reduction
activation, despite their presence in product material/configuration.

## Delivered format

The owner supervises `arecord` with these parameters:

| Parameter       | Native input                             | Delivered stream                        |
| --------------- | ---------------------------------------- | --------------------------------------- |
| Rate            | 48,000 Hz                                | 48,000 Hz                               |
| Channels        | 2                                        | 1, donor left channel                   |
| Format          | `S32_LE`                                 | `S32_LE`                                |
| Period          | 256 frames / 2,048 bytes                 | 256 frames / 1,024 payload bytes        |
| Hardware buffer | 4,096 frames / 16 periods / 32,768 bytes | Not a consumer buffer                   |
| Record interval | About 5.33 ms                            | About 5.33 ms while delivery is allowed |

No rate or sample-width conversion occurs. Consumer-specific conversion stays
outside the platform; channel selection belongs here because its semantics
come from the donor. Source: [source.go](../tools/mic-capture/source.go) and
[protocol.go](../tools/mic-capture/protocol.go).

## Stream protocol

`/run/reinvoke/mic-capture/audio.sock` is mode `0600` and admits UID 0 only.
Each connection starts with one 32-byte little-endian header:

| Offset | Size | Meaning                |
| -----: | ---: | ---------------------- |
| 0      | 8    | ASCII `RINVOMIC`       |
| 8      | 2    | Protocol version, 1    |
| 10     | 2    | Header length, 32      |
| 12     | 4    | Sample rate, 48000     |
| 16     | 2    | Channels, 1            |
| 18     | 2    | Format, 1 = `S32_LE`   |
| 20     | 4    | Frames per record, 256 |
| 24     | 8    | Capture generation     |

Each subsequent record is exactly 1,048 bytes:

| Offset | Size | Meaning                                     |
| -----: | ---: | ------------------------------------------- |
| 0      | 8    | Capture generation                          |
| 8      | 8    | Monotonically increasing sequence           |
| 16     | 8    | Service wall-clock timestamp in nanoseconds |
| 24     | 1024 | 256 mono `S32_LE` samples                   |

Unix streams do not preserve write boundaries. Use exact-length reads;
EOF within a header or record invalidates that partial data.
Timestamps use the service wall clock, not a monotonic sample clock.

A capture restart creates a new generation and closes existing clients.
Ordinary mute drops periods but does not close clients or advance generation.
Consumers must not interpret a stalled stream as EOF or manufacture continuity
across a reconnect. Use the [tool guide](../tools/mic-capture/README.md) for
service/client invocation.

The default per-client queue holds four periods. A full queue or a socket
write exceeding its 250 ms deadline disconnects that slow consumer.

## Implemented privacy boundary

The [MCU privacy controller](current-product-contract.md#microphone-privacy-boundary)
owns `/run/reinvoke/microphone-state`. Capture starts muted and polls it every
100 ms. Missing, invalid, oversized or `muted` state discards periods;
`unmuted` allows delivery after the next poll.

Every 250 ms, capture checks DSP executable, PID/start time and microphone
socket identity. A change restarts the stream generation and closes clients.
ALSA helper failure also triggers capture restart.

> [!IMPORTANT]
> This is not a synchronous mute fence. Polling and already queued records
> prevent a guarantee of zero delivery immediately after a mute request.
> Consumers must use the owned socket, not reopen raw ALSA; `hw_params` can
> overwrite a previous DSP route.

There is no MCU authority-epoch negotiation or proven ALSA reconfiguration
drain. Implementation: [main.go](../tools/mic-capture/main.go),
[state.go](../tools/mic-capture/state.go) and [hub.go](../tools/mic-capture/hub.go).

## Historical acceptance

RAM speech/tap measurements found 99.975% nonzero unmuted samples and exactly
0/244,736 nonzero muted samples. Capture checks covered 14 toggles, zero muted
delivery in measured windows, resumed unmuted delivery and recovery after DSP
restart. Those windows do not establish an instantaneous or adversarial
privacy guarantee.

The [RAM platform](native-ram-platform.md) records the hardware context.
Native Mic-Mute indicator changes are narrower evidence than a data-path test.

## Deferred synchronous privacy design

A stronger fence remains proposed, not part of the wire protocol above.
`BLOCKED`, `DRAIN`, `DRAINED`, authority epochs and `ALLOW` are not implemented
messages and must not be assumed by consumers.

The design would keep the MCU as sole privacy authority and require:

1. Blocked delivery after every ALSA configuration.
2. An MCU-requested synchronous capture fence, followed by confirmed DSP mute.
3. A drain of at least 64 consecutive all-zero native periods over at least
   200 ms, then restoration only of a known, still-requested unmuted policy.
4. A matching MCU authority epoch and explicit allowance before delivery.
5. Client closure and renewed synchronization on PCM, DSP or MCU generation
   changes, authority loss or invalid/muted state.

Ordinary mute would fence the exact capture generation before hardware mute;
an unresponsive owner would need verified termination. Confirmed hardware
mute must remain visible on the privacy indicator during synchronization.
A design decision and native tests are required before adopting this contract.

Even a synchronous fence cannot revoke bytes already copied into consumer
memory or kernel receive queues.

## Threat model

The single ALSA capture substream gives the owner runtime exclusivity while
open. This is an operational boundary, not an electrical microphone disconnect.
Root is trusted and can kill the owner or reopen the raw device.
Unprivileged consumers must not receive sound-device access or capabilities
that bypass the owner.
