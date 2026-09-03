---
title: Owned Bluetooth speaker control boundary
description: WAMP contract inventory and minimum replacement for music-source-manager and audio-ui
ms.date: 2026-09-03
ms.topic: concept
---

The RAM-only BlueZ and BlueALSA path now delivers audible A2DP to the Invoke
speakers. This changes the replacement boundary: neither donor
`music-source-manager` nor donor `audio-ui` is in the media data path.

## Minimum autonomous stack

For a phone-controlled Bluetooth speaker, no replacement WAMP music service is
required. BlueZ owns connection and transport state, BlueALSA owns A2DP PCM
volume and mute state, and `bluealsa-aplay` owns the ALSA playback lifetime. The
existing owned pairing agent and lifecycle launcher are sufficient around those
components.

One owned bridge service is needed only to retain the Invoke rotary control or a
legacy local WAMP API. That bridge should:

1. read BlueZ device/transport state and BlueALSA `org.bluealsa.PCM1` state;
2. set BlueALSA `Volume` and mute bits rather than keep a second authoritative
   value;
3. subscribe to `com.harman.test.inputEvent` for the donor MCU adapter's rotary
   events;
4. expose the small audio/state WAMP subset below; and
5. publish changes observed from either D-Bus or WAMP.

The physical amplifier and DAC mute gates remain a separate safety boundary.
The bridge must not automatically call MCU unmute procedures.

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

## Observed music-source-manager contract

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

## Observed audio-ui contract

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

## Implementation status and live adapter boundary

[`speaker-control-service.mjs`](../../tools/control/speaker-control-service.mjs)
is the smallest independently testable owned service. One MsgPack WAMP session
registers the eleven relevant procedures and subscribes to the rotary-input
topic. Its
[`speaker-control-state.mjs`](../../tools/control/speaker-control-state.mjs)
core implements clamping, result shapes, event order, stream state, and source
registration. The service and core have dependency-free unit and fake-router
protocol tests.

The service can therefore replace both donor processes for contract testing,
but is not yet the complete closed-unit playback controller. Its
`--bluetooth-active` option supplies the expected single-source registry state
when a test does not have a BlueZ adapter. Rotary events currently apply one
logical percent per event; this is an explicit software policy rather than a
claim about the donor's acceleration curve.

[`speaker-control-backend.mjs`](../../tools/control/speaker-control-backend.mjs)
implements the playback adapter without guessing target state. It requires an
explicit BlueALSA PCM path and injectable source and transport observers,
serializes changes, synchronizes stereo gain and mute, handles a missing PCM,
and projects authoritative snapshots back into the WAMP state model.

Three details still need a closed-unit software integration test before the
adapter can become the default:

* the authoritative BlueALSA PCM object appears only after a peer connects, so
  reconnect and no-PCM behavior must be specified; and
* the 0-127 BlueALSA to 0-100 WAMP conversion and the exact BlueZ-to-legacy
  transport-state vocabulary need a captured private-D-Bus trace; and
* the validated `bluealsa-aplay -D plughw:1,0` path bypasses the donor `music`
  soft-volume PCM. Switching it to that PCM, or disabling BlueALSA software
  volume, would change the tested audio path.

Until those are measured, enabling a second volume controller would risk state
drift or unexpected gain. The remaining implementation is a static ARM version
of this tested adapter with target-verified object and state mappings, not
ports of both donor services.

No physical probing is part of this work. The owned service was validated
against a fake WAMP router and against Bonefish in an isolated user/network
namespace. It is currently a host-side reference because the target rootfs does
not provide Node.js; conversion to the static ARM bridge follows only after the
D-Bus mapping above is fixed.
