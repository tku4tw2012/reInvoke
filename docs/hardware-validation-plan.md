---
title: Hardware validation plan
description: Closed-unit validation and software-interface recovery plan for reInvoke
ms.date: 2026-09-03
ms.topic: concept
---

## Validation boundary

This is the gate between static firmware research and any physical revival
work. It is intentionally non-destructive.

## What is already established without a physical unit

FCC regulatory filings for `APIHKINVOKE` (internal teardown photos and a
Bluetooth test report) already function as a proxy teardown and resolve
several items this plan would otherwise ask a physical sample to confirm.
See `docs/corpus/01_CANONICAL_HARDWARE_BASELINE.md` §3–4 for the sourced
detail. Confirmed from FCC evidence alone:

- A **Micro-USB connector**, documented in the Owner's Manual as
  factory-service-only, sits on a small lower connector PCB next to the
  barrel-style DC power jack — both are externally reachable without opening
  the enclosure.
- A separate lower key PCB carries three physical switches, reachable via the
  enclosure's normal buttons.
- Official firmware maintenance used exactly this USB path with Marvell's
  WinUSB driver infrastructure, matching the `VID_1286`/`PID_8100`/`PID_8101`
  USB recovery IDs already found in the driver `.inf` during firmware
  analysis.
- Board topology: lower connector/service PCB → lower key PCB → main
  electronics/audio-control PCB → removable daughterboard (compute module,
  Marvell 88DE3006/BG2CDP) → top UI/LED PCB, plus two cabled antennas.
- No externally accessible serial/UART jack is documented anywhere in the FCC
  photos or the Owner's Manual. If a UART exists, it is most plausibly
  internal test pads reachable only by opening the case.

Still unresolved even with FCC evidence: daughterboard connector pinout,
exact DRAM part/capacity, and the rotary encoder's exact protocol.

## Closed-unit validation path

The enclosure remains closed. The project uses only the external Micro-USB
service port, normal buttons, LEDs, Bluetooth, audio, and network behavior:

- **USB service port** — the most promising avenue. Since the factory update
  path is confirmed to run over this same Micro-USB connector, it is worth
  attempting read-only enumeration (`lsusb`/descriptor capture only, no
  vendor commands that could trigger a flash) to see whether the SoC's
  Marvell USB recovery mode responds without any board access.
- **External power/network observation** — measuring the wall adapter output,
  and passively observing DHCP/mDNS/UPnP/network behavior on boot, requires
  no disassembly.
- **Button/LED behavior** — correlating physical button presses and LED
  states against the boot messages and services already inferred from
  firmware (`docs/bundle-contents/invoke-flashing/runtime-interface-inventory.md`)
  is possible without opening the case.
- **Bluetooth/acoustic characterization** — if the Bluetooth stack still
  pairs and plays audio, the speaker/mic quality can be assessed without
  touching internals.

This path does not need the daughterboard connector pinout to replace the final
firmware. Software contracts are recovered from held binaries, emulation,
interposed system calls, WAMP traffic, kernel interfaces, and live RAM-only
logs. Unknown electrical details remain documented limitations, not blockers
for a maintained userland on the working BG2CDP platform.

## Before powering a sample

- Assign a sample ID and photograph enclosure labels and every board.
- Photograph both sides of the compute daughterboard and connector area.
- Record visible IC markings, board revisions, and cable/connector keying.
- Confirm the power adapter rating and inspect for damage.
- Keep a known-good firmware image and SHA-256 manifest offline; do not flash it.

## First powered observations

- Measure adapter output unloaded and at the device input.
- Use a current-limited bench supply only within the documented 19 V rating.
- Capture serial output at 115200 baud if the service pads are identified safely.
- Record boot messages, `/proc/mtd`, memory size, network interfaces, and
  process/service state as read-only observations.
- Do not interrupt boot, write U-Boot environment, mount partitions read-write,
  or invoke update/recovery commands during the first pass.

## Optional electrical research

Board-level measurements are outside the current project scope. They are not a
prerequisite for software inventory, RAM boot, or service replacement. If a
separate future hardware project opens a sacrificial unit, it can:

1. Map both daughterboard connectors pin-by-pin with continuity testing while
   unpowered.
2. Identify power, ground, reset, clocks, UART, I2C, SPI, USB, and digital
   audio candidates.
3. Use a logic analyzer on suspected buses during ordinary boot and local
   playback.
4. Correlate observed transactions with the firmware boundaries in
   `runtime-interface-inventory.md`.
5. Record captures, timestamps, probe settings, and sample ID as evidence.

## Software replacement decision gate

* Keep BG2CDP while yellow-mode RAM boot, USB recovery, networking, Bluetooth,
  audio, buttons, LEDs, MCU control, and DSP loading remain usable.
* Replace donor processes incrementally from software-derived contracts.
* Treat board firmware and calibration as immutable assets when no maintained
  replacement exists.
* Consider replacing or bypassing electronics only as a separate future
  project if the existing compute or audio/control path fails.

Any firmware write, board modification, desoldering, or cloud-account action
is outside this autonomous plan and requires explicit human control.
