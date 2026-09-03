---
title: reInvoke revival roadmap
description: Staged roadmap for closed-unit software replacement and recovery
ms.date: 2026-09-03
ms.topic: overview
---

## End state

Reach **repurposing completeness (L2)**: a documented, reproducible way to
reuse the Invoke enclosure, speakers, microphones, UI, and/or compute module,
with every required electrical and protocol assumption marked as proven,
measured, or unresolved. The first practical target is not a complete Cortana
replacement; it is a safe local network/audio/control stack that can preserve
the useful hardware.

## Stages

### 1. Preservation and firmware map — complete

Acquire, hash, mirror, classify, and extract the firmware without executing or
flashing it. Maintain the claim/evidence ledger and keep binaries outside Git.

### 2. Software boundary map — complete, and substantially exceeded

Documented boot, storage, OTA, services, ports, IPC, audio, Bluetooth, Wi-Fi,
MCU, and UI boundaries from static evidence. Two results go beyond a static
map.

The control plane was reconstituted off-device. The service bus is WAMP over
MsgPack routed by `bonefish`, and the device's own ARM binaries now run under
emulation on an x86 host, answering calls from a third-party client. See
`docs/emulation/control-plane-emulation.md`.

Harman's final firmware was recovered and analysed. `Barracuda_libre-12.2134.0`
removes Cortana and Spotify and adds a Wi-Fi blocker, converting the product
into a local Bluetooth speaker. See
`docs/bundle-contents/invoke-ota2/ota2-analysis.md`.

The hardware-side transport and electrical details remain unproven.

### 3. Safe observation on one physical sample — ready to start

Donor device(s) are available. The full non-invasive procedure is in
`docs/no-disassembly-observation-procedure.md`, ordered so the cheapest and
most decisive observations come first.

Because Harman's own final build already targets Bluetooth-speaker operation,
the first question is which firmware a unit carries and whether it pairs and
plays audio. A unit that does is already close to the end goal. Only after
that does USB download-mode probing matter, and that probe is a hard gate on
any RAM-boot work.

Do not flash until a recovery and image-integrity procedure is independently
established.

### 4. Software interface validation

Recover the MCU, DSP, audio, UI, button, LED, and microphone contracts without
opening the enclosure. Use held binaries, WAMP traffic, emulation shims,
interposed system calls, kernel interfaces, and live RAM-only logs. A
log-and-forward ioctl recorder can capture byte-exact I2C and SPI exchanges,
including device responses, while the donor process continues operating.

Electrical characterization and replacement-compute design are optional future
hardware projects. They do not gate a maintained userland on the working
BG2CDP platform.

### 5. Reuse decision

- **Keep BG2CDP:** selected for the current project. Yellow-mode RAM boot, USB
  recovery, networking, Bluetooth, audio, MCU control, and DSP loading work.
- **Replace compute:** optional future hardware project if BG2CDP becomes
  unusable.
- **Bypass electronics:** optional future hardware project if the existing
  audio/control path fails.

### 6. Minimal revival demonstrator

Build the smallest testable stack: local playback, volume/mute, LED/UI
feedback, Bluetooth or network input, and safe shutdown. Keep voice assistant
and cloud dependencies optional. Record reproducible setup and test results.

### 7. Hardening and preservation release

Publish interface evidence, scripts, measurements, compatibility limits,
recovery procedures, and a clear list of unknowns. Keep proprietary binaries
as referenced evidence rather than presenting them as a replacement software
distribution.

## Autonomous boundary

Repository analysis, static reverse engineering, metadata extraction,
documentation, and public-source discovery can proceed autonomously. Physical
measurements, device modification, firmware flashing, credential use, and
redistribution decisions require an explicit human-controlled test setup.
