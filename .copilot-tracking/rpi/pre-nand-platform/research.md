---
title: Pre-NAND platform research
description: Evidence and decisions for completing the RAM-only replacement platform before any persistence review
ms.date: 2026-09-03
ms.topic: concept
---

## Scope

Complete every required RAM-only replacement and acceptance task before the
separate NAND persistence decision. NAND writes, boot-slot changes, and
persistent installation are excluded.

## Research streams

| Stream | Status | Owner | Required result |
|--------|--------|-------|-----------------|
| ALSA microphone capture | Complete | `alsa-capture-research` | Root cause, remediation options, and muted test criteria |
| Remaining service contracts | Complete | `contract-gap-research` | Prioritized must-have and optional contract gaps |
| RAM integration readiness | Complete | `integration-readiness` | Ordered packaging and cold-boot acceptance gaps |

## Established baseline

* Yellow-mode U-Boot and the checksum-gated RAM image are reproducible.
* Networking, provisioning, MCU control, DSP boot/control, Bluetooth A2DP,
  rotary volume, and audible ALSA playback have live hardware evidence.
* The owned DSP service downloads the exact image, receives the boot event,
  answers `getVer`, and does not change amplifier or DAC mute policy.
* ALSA card 1 exposes a capture substream, but every tested hardware
  configuration was rejected and produced zero frames.
* NAND persistence work requires a later, explicit review and is outside this
  RPI cycle.

## Initial contract findings

The first contract inventory identified these release blockers for a standalone
RAM-only speaker:

1. Move the audio and Bluetooth service graph from host-side ADB launchers into
   target-side supervision.
2. Remove the donor SquashFS dependency used to obtain `dbus-daemon`.
3. Add one mute-policy authority that unmutes only for verified playback and
   remutes on stop, failure, or shutdown.
4. Port the tested speaker-control reference to a static target binary so
   rotary events, BlueALSA volume, and mute state have one owner.
5. Recover button events and use a bounded device-initiated pairing window.
6. Define a stable RAM-platform Bluetooth identity.

LED indication, owned earcons, AVRCP validation, liveness publications, and
evidence sidecars improve product completeness but do not block first
standalone playback. OTA, boot-slot selection, MCU firmware update, factory
setters, and cloud voice-agent contracts belong to the later persistence or
explicitly excluded scope.

The corrected archive pass verified that the MCU uses I2C address `0x36`;
address `0x20` is the shared IO expander and `0x4c` is the DAC. Donor strings
bound the physical input vocabulary to volume up/down, Bluetooth button,
Bluetooth long press, and microphone-mute button. Held captures contain only
the rotary codes, so numeric button codes still require offline disassembly or
an attended capture.

The donor MCU service is also the LED animation player. Eight shipped animation
assets are held, and the donor supervisor configuration provides exact
`auto_recovery`, readiness, and heartbeat expectations. Direct binutils
analysis recovered the remaining contract:

* Raw key codes `0x00` through `0x09` map to action short/long, Bluetooth
  short/long, microphone short/long, reset short/long, and rotary up/down.
* Non-rotary keys publish one canonical string on
  `com.harman.vui.keypress`.
* LED assets contain one 13-byte intensity frame per top-panel update.
* The player sends at most 390 asset bytes to MCU address `0x36` with opcode
  `0x0e`, first-chunk flag `0x01`, later flag `0x00`, and 280 ms chunk pacing.

## Integration readiness findings

The current cold-boot image is not yet the complete replacement platform.
Only PID 1, radio support, and the owned network/provisioning binaries are
packaged. Bonefish, MCU, DSP, D-Bus, BlueZ, BlueALSA, audio playback, and the
control bridge are still host-staged after boot.

The prerequisite order is:

1. Enforce the initramfs size budget. The current image is about 40 MiB against
   a 68 MiB hard limit, so the 49 MiB donor rootfs cannot be embedded.
2. Package an on-device WAMP router or remove the router dependency.
3. Add checksum-gated owned MCU and DSP binaries plus `dsp-img.ldr`.
4. Decouple the MCU heartbeat from WAMP connectivity and supervise all critical
   services with bounded restart backoff.
5. Package D-Bus, BlueZ, BlueALSA, playback, pairing, and the target-side
   speaker-control authority.
6. Harden PID 1 reaping, console backoff, shutdown mute, MTD node permissions,
   low-level BusyBox applets, artifact hashes, and module-tree selection.
7. Add a machine-readable acceptance collector and repeat-boot harness.

The final pre-NAND gate requires at least five consecutive cold boots. Each
iteration must verify the release manifest, no NAND mount, owned binary hashes,
radio interfaces, continuous MCU heartbeat, default-deny unmute, exact DSP
download and version, guarded audio, no kernel fault or zombie growth, and
yellow-mode recovery.

Documentation also needs correction after implementation. The highest-impact
stale claims describe the audio path as still awaiting acceptance, retain the
donor DSP in the target architecture, provide an obsolete initramfs recipe,
and omit the MCU heartbeat reset hazard.

## ALSA capture findings

The most likely immediate failure is an ALSA parameter mismatch, not a missing
capture driver. Harman routes both playback and capture through the Berlin
playback callbacks and assigns one fixed buffer geometry:

* Period bytes: 2,048
* Period count: 16
* Buffer bytes: 32,768
* Coherent capture format: stereo 48 kHz `S32_LE`
* Required period size for `S32_LE`: 256 frames

The earlier hardware sweep tested 256-frame periods with counts 2, 4, and 8,
but not the required count of 16. TinyALSA submits exact values, so ALSA core
rejects its normal 1,024-frame, four-period defaults before the Berlin capture
callback runs.

After valid hardware parameters are established, two separate runtime
dependencies remain possible: the MIC port derives its clock from the SEC
playback domain, and the external DSP may need to be booted and placed in its
normal microphone mode before I2S data arrives. These can be separated with
muted silent playback and ordered DSP tests. Kernel changes are deferred until
those zero-code tests fail.

The exact geometry was then validated on the physical unit. TinyALSA opened
card 1 PCM 0 at stereo 48 kHz `S32_LE`, 256-frame periods, and a 4,096-frame
buffer. The active ALSA status reached `RUNNING`, its hardware and application
pointers both advanced to 47,616 frames, and the finalized five-second capture
contained 121,344 frames. Of those, 99.98 percent were nonzero; left and right
channels had correlation 0.975. Amplifier and DAC mute remained asserted.

The evidence bundle is retained outside Git at
`reinvoke-archive/hardware/usb-attempts/20260903T225737Z-microphone-capture-original-absent/native-platform-evidence/`.
This resolves the ALSA parameter-rejection diagnosis without a kernel change.
An attended speech or tapping correlation remains before claiming acoustic
microphone acceptance.

## Completion

Research is complete. Offline disassembly eliminated the need for a new button
or LED protocol capture; physical acceptance remains.
