---
title: Owned Bluetooth speaker control boundary
description: Current MCU-owned BlueALSA authority and historical WAMP contract inventory
ms.date: 2026-09-05
ms.topic: concept
---

The RAM-only BlueZ and BlueALSA path now delivers audible A2DP to the Invoke
speakers. This changes the replacement boundary: neither donor
`music-source-manager` nor donor `audio-ui` is in the media data path.
The [current product and architecture contract](../current-product-contract.md)
is normative.

## Minimum autonomous stack

For a phone-controlled Bluetooth speaker, no replacement WAMP music service is
required. BlueZ owns connection and transport state, BlueALSA owns A2DP PCM
volume and mute state, and `bluealsa-aplay` owns the ALSA playback lifetime. The
existing owned pairing agent and lifecycle launcher are sufficient around those
components.

The accepted image implements the bridge inside `reinvoke-mcu-interface`, so
there is no second target-side volume authority. It:

1. reads BlueZ device/transport state and BlueALSA `org.bluealsa.PCM1` state;
2. sets BlueALSA `Volume` and mute bits rather than keeping a second authoritative
   value;
3. handles physical rotary events at the MCU boundary before compatibility
   publication;
4. exposes the required audio/state WAMP subset; and
5. publishes changes observed from either D-Bus or physical input.

The physical amplifier and DAC mute gates remain a separate safety boundary.
Compatibility volume calls do not directly unmute physical hardware. The MCU
policy owner opens the gates only for a verified active-PCM lease whose thread ID
matches ALSA and the expected playback executable.

The bridge has one authority for each value:

| State | Authority | Compatibility projection |
|---|---|---|
| Connected source | BlueZ `org.bluez.Device1` | registered/active source `com.harman.bluetooth` |
| Transport/playback | BlueZ transport plus the BlueALSA PCM lifecycle | `bluetooth.state` and `com.harman.stateChanged` |
| Volume and mute | BlueALSA `org.bluealsa.PCM1.Volume` | music volume/mute procedures and events |

BlueALSA packs each channel's mute bit and 0-127 A2DP volume into a `uint16`.
The donor WAMP API uses 0-100. A bridge therefore needs one documented rounding
rule and must update both channels together; it must not infer mute from volume
zero because the two values are independent in both APIs.

## Historical donor `music-source-manager` contract

Previously preserved Bonefish logs and isolated execution of the unchanged
donor binary show these registrations:

| Procedures | Purpose |
|---|---|
| `com.harman.music.{next,pause,prev,repeat,resume,shuffle,skipto,stop}` | Forward controls to the active source |
| `com.harman.source.{flush,get-active,get-registered,nowPlayingUpdate,register,start,trackPositionUpdate,volumeSet}` | Source registry, metadata, and volume routing |
| `com.harman.music-source-manager.shutdown` | Donor lifecycle |

It subscribes to `com.harman.volumeChanged` and publishes
`com.harman.ready.music-source-manager` plus periodic
`com.harman.heartbeat.music-source-manager`.

The source calls exercised in isolated emulation have these exact shapes:

| Call | Result |
|---|---|
| `source.register ["com.harman.bluetooth"]` | no arguments |
| `source.get-registered []` | `["com.harman.bluetooth"]` |
| `source.start ["com.harman.bluetooth"]` | no arguments |
| `source.get-active []` before/after start | `[""]` / `["com.harman.bluetooth"]` |

The donor Bluedroid service needed this registry. BlueZ and BlueALSA do not, so
none of these procedures is required by the minimum replacement.

## Historical donor `audio-ui` contract

`audio-ui` registers twelve procedures. Six form the volume/query compatibility
surface:

| Procedure | Observed call and result |
|---|---|
| `com.harman.volumeGet` | returns keyword state with `music` and `system` |
| `com.harman.volumeSet` | `[value, "music"]`; clamps to 0-100; returns `[effective, "music"]` and full volume state |
| `com.harman.volumeAdjust` | `[delta, "music"]`; clamps the result; same result shape |
| `com.harman.musicMuteSet` | `[boolean, "music"]`; returns `[boolean, "music"]` and full volume state |
| `com.harman.musicMuteToggle` | no arguments; same result shape as `musicMuteSet` |
| `com.harman.stateGet` | returns the full stream map as keyword arguments |

`com.harman.extStateUpdate ["bluetooth"] {"state":"playing"}` returns no
arguments, updates the Bluetooth entry, and publishes
`com.harman.stateChanged ["bluetooth"]` with the full stream map as keyword
arguments.

Volume changes publish `com.harman.volumeChanged ["music", value]`. Muting at a
stored volume of 35 publishes `volumeChanged ["music", 0]`, then
`com.harman.musicMuteChanged [true]`; the stored volume remains 35.

The other five registered procedures are alert/demo/voice-agent compatibility:
`com.harman.aui.alertPlay`, `com.harman.aui.alertCancel`,
`com.harman.aui.registerVoiceAgent`, `com.harman.aui.demo-action`,
and `com.harman.demoIntro`. They are not needed for Bluetooth-speaker
operation. The service subscribes to
`com.harman.test.inputEvent` and `com.harman.music.stateChanged`.

## Host reference implementation and current target

[`speaker-control-service.mjs`](../../tools/control/speaker-control-service.mjs)
is the historical host-side reference used to recover and test the minimum
contract. One MsgPack WAMP session
registers the eleven relevant procedures and subscribes to the rotary-input
topic. Its
[`speaker-control-state.mjs`](../../tools/control/speaker-control-state.mjs)
core implements clamping, result shapes, event order, stream state, and source
registration. The service and core have dependency-free unit and fake-router
protocol tests.

The service can replace both donor processes for contract testing. Its
`--bluetooth-active` option supplies the expected single-source registry state
when a test does not have a BlueZ adapter. Rotary events currently apply one
logical percent per event; this is an explicit software policy rather than a
claim about the donor's acceleration curve.

[`speaker-control-backend.mjs`](../../tools/control/speaker-control-backend.mjs)
implements the corresponding host-side playback adapter without guessing target
state. It requires an
explicit BlueALSA PCM path and injectable source and transport observers,
serializes changes, synchronizes stereo gain and mute, handles a missing PCM,
and projects authoritative snapshots back into the WAMP state model.

The target does not run Node.js or either reference module. The static ARM MCU
service uses the packaged BlueALSA CLI against the explicit peer/PCM mapping,
coalesces rotary updates, writes stereo volume and mute authoritatively, and
projects compatibility state. Reconnect and no-PCM behavior are fail-closed.

The active-PCM lease remains a separate speaker-safety input, not a volume
signal. Current physical validation covers rotary changes and earlier attended
playback; the newest accepted image still needs the final attended
playback-continuity run. No physical probing is part of this work.
