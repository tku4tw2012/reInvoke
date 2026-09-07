---
title: reInvoke revival roadmap
description: Staged roadmap and completed milestones for closed-unit reInvoke software replacement
ms.date: 2026-09-05
ms.topic: overview
---

## End state

The [current product and architecture contract](current-product-contract.md)
defines the accepted target. This roadmap retains completed stages as historical
milestones and identifies the remaining product work.

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

That donor finding was a comparison point. The current target is the owned
RAM-only reInvoke stack, not Harman's 2021 firmware.

### 3. Safe observation on one physical sample — complete

The closed sample completed USB/U-Boot access, RAM boot, NAND readback, Wi-Fi,
Bluetooth, playback and capture ALSA, MCU, DSP, controls, LEDs, and attended
audio checks. The original
[no-disassembly procedure](no-disassembly-observation-procedure.md) is retained
as the historical plan; [U-Boot access](uboot-access.md) is the current USB
procedure.

The sample carries `Barracuda_libre-12.2050.3`, not the 2021 final image.
Yellow-mode USB and owned RAM boot are resolved and no longer a project gate.

Do not flash until a recovery and image-integrity procedure is independently
established.

### 4. Software interface validation — accepted boundary

The required MCU, DSP, audio, button, LED, and microphone contracts were
recovered without opening the enclosure. Owned MCU and DSP services now
implement the target boundary. Physical meanings for every button/animation,
occasional missing MCU Mic-Mute events, and onboarding orchestration remain
explicit gaps rather than blockers hidden behind donor binaries.

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

### 6. Minimal revival demonstrator — implemented, final campaign pending

The owned PID 1, Bluetooth playback, volume, speaker safety, microphone privacy,
LED transport, networking, provisioning boundary, and safe shutdown are
implemented. The current image still needs the remaining cold boots and one
attended playback-continuity run in [PLAN.md](../PLAN.md).

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
