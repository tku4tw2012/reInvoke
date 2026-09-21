---
title: Version history
description: How the build numbering works, and what every earlier name maps to
ms.date: 2026-09-19
ms.topic: reference
---

# Version history

Builds are numbered `ERA.MILESTONE.ITERATION`.

* **Era** is what the speaker can do without help. `1` needs a host to boot;
  `2` boots from its own NAND.
* **Milestone** is a capability that did not exist before it.
* **Iteration** counts builds within that milestone.

The current build is **2.2.11**. The unit in hand runs **2.2.10**. Numbering began with 2.2.8;
everything before it was renamed after the fact.

## Why the numbers changed

The first eighteen days produced 236 dated artifacts under at least six
different naming conventions: `invoke-kernel-gcc49-*`, `runtime-v1`
through `v12` with `-repro` and `-final` and `-iteration14` suffixes,
`pre-nand-rc1` through `rc12`, `native-02` through `native-05`, then
`05.1` through `05.8`, then `05.8.7` through `05.8.13`, with an abandoned
`06.x` branch sitting chronologically in the middle of the `05.8.x` range.

Nothing about that sequence says what any build could do, and `05.8`
jumping straight to `05.8.7` means the numbers are not even a reliable
ordering. This scheme is read from the dates and the evidence, not from
arithmetic on the old names.

## Eras

### 1.x — runs from RAM, needs a host

| Version | Was | Dates | What it established |
| --- | --- | --- | --- |
| 1.0.z | `invoke-kernel-gcc49-*`, `invoke-kernel-acast-*` | 09-02 → 09-03 | A kernel that runs on this SoC, built with the NDK gcc 4.9 that matches the vendor's own module ABI |
| 1.1.z | `reinvoke-native-runtime-v1` … `v12`, with `-repro`, `-final` and `-iteration14` | 09-03 → 09-04 | A runtime loaded into RAM over USB: MCU, DSP, audio, capture |
| 1.2.z | `pre-nand-rc1` … `rc12`, `pre-nand-mic-rc1` | 09-05 → 09-08 | The runtime hardened enough to be worth writing to flash. `rc12` is still the base image every NAND build patches |

### 2.x — boots from NAND, no host

| Version | Was | Dates | What it established |
| --- | --- | --- | --- |
| 2.0.z | `reinvoke-nand-pilot-01`, `reinvoke-native-02` … `native-05` | 09-09 → 09-13 | The first writes to this unit's NAND, and a device that came back afterwards |
| 2.1.z | `reinvoke-native-05.1` … `05.8` | 09-14 → 09-15 | A NAND boot that reaches a working runtime unattended |
| 2.2.z | `reinvoke-native-05.8.7` … `05.8.13` | 09-16 → 09-19 | Behaviour: sound, lights, volume, Bluetooth, identity |

### 2.2.z in detail

This is the range where the speaker started behaving like a speaker, so the
mapping is listed build by build.

| Version | Was | Date | Flashed |
| --- | --- | --- | --- |
| 2.2.1 | 05.8.7 | 09-16 | yes |
| 2.2.2 | 05.8.8 | 09-17 | yes |
| 2.2.3 | 05.8.9 | 09-17 | yes |
| 2.2.4 | 05.8.10 | 09-17 | yes |
| 2.2.5 | 05.8.11 | 09-18 | yes |
| 2.2.6 | 05.8.12 | 09-19 | no |
| 2.2.7 | 05.8.13 | 09-19 | yes |
| 2.2.8 | — | 09-20 | yes |
| 2.2.9 | — | 09-20 | yes |
| 2.2.10 | — | 09-21 | yes |
| 2.2.11 | — | 09-21 | not yet |

## The abandoned 06.x branch

`06.0`, `06.1` and `06.2` were an attempt to boot a kernel this project
built, rather than the vendor's. They are not renumbered, because they are
not a step on this line: they are a dead end worth remembering.

The flashes themselves succeeded. `06.1` and `06.2` were written to NAND and
the device accepted them. What failed is that the kernel never ran.

The reason is recorded in [bootimgs format](bootimgs-format.md), and the
distinction matters if anyone is tempted to retry this. It is **not**
signature enforcement. The SoC's own first-stage signing is confirmed **off**
on this unit: the boot ROM prints `MRVL SIGN R :0000` and `CUST SIGN R :0000`
with the fuses unlocked, and an unsigned `bootloader.img` boots fine. A
kernel does not need to be signed to be accepted here.

The barrier is a separate, later mechanism inside the vendor's bootloader.
It reads the kernel slot with `bcpu0_image_encrypt` confirmed set and hands
those bytes to `bcm_image_verify(BCM_IMG_KERNEL_TYPE, ...)`, a mailbox call
into the closed BCM co-processor, before treating the result as a kernel.
`06.1` placed a bare, unencrypted uImage at the correct address; the address
was never the problem. The co-processor expects a customer key held in
silicon this project cannot reach, so the obstacle is not one that better
tooling or a signature could clear.

So: the vendor kernel is a fixed dependency, and everything this project
ships runs on top of it.

The NAND-side artifacts of that branch were deleted on 2026-09-20: the
`reinvoke-native-06.0` build output and the `flash-06.0`, `flash-06.1` and
`flash-06.2` evidence directories. Nothing pinned them and no document cited
them, and the conclusion above does not rest on them. The RAM-side `06.1`
artifacts were kept: they belong to era 1, not to this dead end, and the
name collision is only a naming accident.

## What was not renamed

Only the repository moved to this scheme. Two things deliberately did not.

**The local archive.** Its 236 artifact directories keep their original
names. They are pinned by path *and* SHA-256 in `tools/nand-pilot/build-lib.js`
and in the private build configuration, so renaming them would break every
pin for a cosmetic gain, and the archive is not published anyway.

**The evidence directories.** `flash-0589-20260917T125154Z` and its siblings
are records of something that happened at a particular moment. Renaming a
record of the past to match a naming decision made later would falsify it.
This table is the bridge between the two.
