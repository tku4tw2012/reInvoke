---
title: Owned Bluetooth speaker control boundary
description: BlueALSA volume authority, MCU speaker authorization and PCM separation
ms.date: 2026-09-12
ms.topic: reference
---

BlueZ and patched BlueALSA replace donor `music-source-manager` and `audio-ui`.
The bridge runs inside the static ARM `reinvoke-mcu-interface`, not a second
target-side music service. The [current contract](../current-product-contract.md)
is normative; recovered donor calls are consolidated in
[control-plane emulation](control-plane-emulation.md#audio-and-source-contracts).

## State authority

| State              | Authority                                  | WAMP projection                                      |
| ------------------ | ------------------------------------------ | ---------------------------------------------------- |
| Connected source   | BlueZ `org.bluez.Device1`                  | Source `com.harman.bluetooth`                        |
| Transport/playback | BlueZ transport and BlueALSA PCM lifecycle | Bluetooth stream state and `com.harman.stateChanged` |
| Music volume/mute  | BlueALSA `org.bluealsa.PCM1.Volume`        | Volume/mute procedures and events                    |

The MCU reads D-Bus state, handles physical rotary input before compatibility
publication, and writes both BlueALSA channels together. BlueALSA packs mute
bits and 0-127 A2DP volumes into one `uint16`; WAMP uses 0-100 with integer
nearest rounding:

```text
write: (percent * 127 + 50) / 100
read:  (rawVolume * 100 + 63) / 127
```

Mute is independent of volume zero. A newly acquired transport is capped at
12 percent; its PCM is polled every 250 ms. This is a connection policy, not
a loudness calibration. Reconnect and missing-PCM handling are fail-closed.
See [BlueALSA controller](../../tools/mcu-interface/bluealsa.go).

## PCM and speaker safety

This diagram describes software ownership and logical signal flow, not board
wiring. PCM samples follow ALSA, not the DSP service's SPI control channel.

```mermaid
flowchart TB
  Peer["Allowlisted Bluetooth source"] --> Decode["BlueZ and BlueALSA<br/>SBC decode"]
  Decode --> Player["bluealsa-aplay"]
  Player --> PCM["ALSA playback PCM"]
  PCM --> Output["Audio output and speakers"]
  Player -. "Thread lease" .-> Policy["MCU playback policy"]
  PCM -. "RUNNING and owner_pid" .-> Policy
  Keys["Rotary or WAMP volume call"] --> Volume["MCU BlueALSA<br/>volume adapter"]
  Volume -. "Stereo volume and mute" .-> Decode
  Policy -. "I2C mute gates" .-> Output
  DSP["Owned DSP service"] -. "SPI firmware/control" .-> BoardDSP["Board DSP"]
```

Every 100 ms, the MCU verifies that the lease thread matches ALSA `owner_pid`,
`/proc/<tid>/exe` resolves to the packaged player, and PCM is `RUNNING`.
Only that conjunction authorizes physical amplifier/DAC unmute. Losing
authorization reasserts mute after a 1.5-second holdoff; shutdown requests mute
directly. Failed unmute attempts to restore mute. Neither DSP boot nor a
compatibility volume setter grants this authorization.

The lease proves an owner and PCM delivery, not audible content: positive
buffers can contain zero-valued samples. See
[playback policy](../../tools/mcu-interface/playback_policy.go) and
[controller sequencing](../../tools/mcu-interface/controller.go).

## Host reference versus target

The dependency-free Node
[state core](../../tools/control/speaker-control-state.mjs),
[service](../../tools/control/speaker-control-service.mjs) and
[backend](../../tools/control/speaker-control-backend.mjs) test the compatibility
contract. One WAMP session registers eleven relevant procedures and subscribes
to rotary input. The core covers clamping, result shapes, event order and
source/stream state.

`--bluetooth-active` supplies a single-source test state without an adapter.
Reference rotary handling applies one logical percent per event, not a claimed
donor acceleration curve. The injectable backend requires an explicit
BlueALSA PCM path and source/transport observers; it serializes changes,
synchronizes stereo gain/mute and projects authoritative snapshots.

The target instead uses the packaged BlueALSA CLI with explicit peer/PCM
mapping and coalesced rotary updates. It runs neither Node.js nor the reference
modules.

## Evidence boundary

Host tests and historical RAM runs exercise the safety implementation.
Candidate 02 separately demonstrated native attended sound and rotary volume.
Candidate 03 has connection/provisioning evidence, not a repeated acoustic
campaign. Native microphone capture/privacy acceptance remains open.
Candidate-specific results belong in the
[native NAND guide](../native-nand-platform.md).
