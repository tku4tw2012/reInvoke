---
title: NAND write evidence and decision gates
description: Unit-specific evidence, blockers, and proof requirements for any persistent NAND change
ms.date: 2026-09-07
ms.topic: concept
---

The goal is for an already Bluetooth-updated Invoke to start reInvoke from wall
power and reach the owner's local assistant infrastructure without a USB boot
host. This session handles storage and startup, not microphone or assistant
implementation.

The current approach is a bounded, software-only persistent change. The owner
accepts DIY risk but does not want disassembly or whole-chip destruction.
Neither external chip cloning nor a deliberate full erase is a prerequisite.
No actual write has been approved by the current draft.

Use the
[active persistence plan](../.copilot-tracking/plans/2026-09-07/nand-persistence-draft-plan.md)
and its
[source-backed research](../.copilot-tracking/research/2026-09-07/nand-payload-persistence.md).
Earlier plans that required chip removal or assumed guaranteed recovery after
full erasure are superseded.

Every claim is labelled:

* **(V)** verified on this physical unit
* **(D)** vendor, SoC, or community documentation
* **(I)** inference, clearly reasoned but not proven
* **(A)** repeatable computation on an archived artifact, not a new live test

## Session handoff, 2026-09-07

The RAM platform work is closed and merged. Pull requests #1 and #2 are on
`main`, the working branch is deleted, and the tree is clean. This document is
the entry point for the NAND question; nothing else is in flight.

Current candidate is `pre-nand-rc11` in
`build/artifacts/pre-nand-rc11-20260907/`. It is pinned and reproducible but has
**not** had a cold boot, so it is the one thing a NAND session should not assume
is proven. `pre-nand-rc10` is the last candidate verified from a cold boot.

The physical unit is currently running binaries hot patched to match rc11
exactly. A reboot loads rc11 from its pinned image, so nothing is lost, but the
first boot of rc11 is still unverified.

What the platform can do today, all proven on hardware: cold boot, audio through
the DSP, Bluetooth pairing and playback with a safe volume ceiling, every
physical control including the rear indicator, a bounded isolated provisioning
window, and a complete setup path where an external client joins the speaker's
own access point and hands over credentials that put the speaker on the home
network.

Open items that are not NAND are listed under open defects in
`docs/current-product-contract.md`. The notable one is a media volume observed at
zero with no explanation.

## Read-only findings, 2026-09-07

The probes issued no write ioctl and targeted only the odd-numbered read-only
MTD minor. They rejected the even writable minor before issuing an MTD ioctl.
Device-side autonomous writes were not excluded. Probe source, compiler
identity, output, and hashes are in the external evidence archive. **(V)**

The latest offline work resolves the allocation map:

1. Eight identical CRC-valid version tables in this unit's retained capture
   place `rootfs` at `0x02920000-0x08320000` (end exclusive), a 90 MiB
   allocation. Every allocation named in both retained 83 containers agrees
   with the captured table. **(A, D)**
2. The earlier putative kernel at `0x00a20000` is `tz_en`. The actual
   `bootimgs` allocations are 10 MiB each. **(A)**
3. The changed block at `0x00660000` belongs to `factory_setting`, not a
   TrustZone or bootloader allocation. Its cause remains unknown; its factory
   contents remain outside candidate writes. **(A)**
4. The five ECC-failing pages are in `fw_stat`; the two known bad blocks are
   in `app`. Neither lies in the rootfs allocation. **(A)**

Earlier read-only observations still matter:

1. Two new complete logical reads match each other but not the 2026-09-02
   capture. One erase block at `0x00660000` now reads entirely `0xFF`; it
   previously contained 13,040 bytes of structured content. The time and cause
   of the transition are unknown. **(V)**
2. Each of the five historical bad pages still increments the kernel's
   uncorrectable-ECC counter. Adjacent control pages do not. The returned bytes
   are repeatable, but they remain uncorrectable. **(V)**
3. The live Linux OOB interface is internally inconsistent. It declares 64
   bytes, returns meaningful content only in bytes 0-31, and reports ECC
   positions 80-127. Current upstream Linux maps this Toshiba ID prefix to a
   part with 128 physical OOB bytes. **(V, D)**
4. The MTD `raw` mode is not raw on this driver. Normal and raw reads return the
   same bytes and both exercise hardware ECC because the driver wires
   `read_page_raw` to its hardware-ECC reader. **(V, D)**

The complete captures are not raw restore images. That does not rule out a
targeted logical filesystem write using the controller's normal hardware-ECC
path. It means target-specific backup, ECC verification and restoration must be
defined rather than assumed.

## The short version

The RAM-only platform is sufficient for continued NAND investigation, while its
remaining non-NAND acceptance gaps stay recorded in the current product
contract. No current artifact is a validated persistent firmware.

There is a concrete rootfs allocation to investigate now. Remaining work is to
select a compatible startup change, verify the writer's scope and ECC behavior,
and explain the residual boot and recovery risk before the first write. A stock
reflash is not automatically needed to establish any of those.

## What is genuinely known about the flash

### Geometry has three different layers

The running kernel reports a single unpartitioned device. **(V)**

```
mtd1: 10000000 00020000 "mv_nand"
NAND device: Manufacturer ID: 0x98, Chip ID: 0xda (Toshiba 256MiB 8-bit),
256MiB, page size: 2048, OOB size: 64
```

`0x10000000` is 268,435,456 bytes, so this is a 256 MiB part with a 128 KiB
erase block. No partition table is published by this kernel, so there is no
offset map to write against without reconstructing one. The 64-byte OOB value is
the Harman driver's MTD declaration, not a verified physical spare-area size.

The unit reports full ID `98 DA 90 15 76 16`. **(V)** Current upstream
[Linux NAND identification](https://github.com/torvalds/linux/blob/master/drivers/mtd/nand/raw/nand_ids.c)
maps prefix `98 DA 90 15 76` to `TC58NVG1S3H`, with 2 KiB pages, 128-byte
physical OOB, 128 KiB erase blocks, and an 8-bit-per-512-byte ECC requirement.
**(D)** The package marking has not been inspected, so exact-model identity
remains documentation-backed rather than physically confirmed.

The Harman/Marvell source configures this ID for 48-bit BCH per 2 KiB. Under BCH
its controller path uses 32 OOB bytes per single-plane page, even though MTD
publishes 64. **(D)** Live OOB reads confirm that only bytes 0-31 carry
controller-returned content. **(V)**

The current model is therefore:

| Layer | OOB size | Evidence |
|---|---:|---|
| Toshiba physical spare area | 128 bytes | **(D)** exact ID-prefix match |
| Harman Linux MTD declaration | 64 bytes | **(V)** |
| Marvell BCH controller-visible area | 32 bytes | **(V, D)** |

This reconciliation explains the visible numbers. It does not settle how to
produce or restore all physical OOB bytes on this unit.

### Neither logical capture is a restore image

`hardware/dumps/20260902T215700Z-native-ram/invoke-nand-data.bin` is exactly
268,435,456 bytes and its recorded SHA-256 was re-verified for this document as
`edf38ef2af48d249c9925ebb6a94c716cfdb2c1ce575fb704283918cdd0e53be`. **(V)**

It is a real capture. Sampling 512 evenly spaced 4 KiB regions found 213 holding
content, 298 fully erased and one all-zero, the expected shape for a partly used
NAND. The `hsqs` SquashFS magic appears exactly where the capture notes place it.
**(V)**

Three limitations matter, and together they mean this file should not be treated
as a restore image.

**It contains no OOB.** `nand-dd.log` shows `131072+0 records` of 2048 bytes,
which is the data area only. ECC syndromes, bad-block markers and controller
metadata live in OOB and are absent. **(V)**

**Five pages were never read correctly.** During the capture the kernel logged
`uncorrectable ECC error` at pages `0x1fc81`, `0x1fc86`, `0x1fc8a`, `0x1fc8b` and
`0x1fc8c`. **(V)** The SHA-256 proves the file has not changed since; it says
nothing about whether those bytes are right. A restore from this image would
write back data the device itself could not read.

**It is an ECC-processed logical read**, not a raw programmer image. It reflects
what the MTD layer handed back, after correction where correction succeeded.

Two complete logical rereads on 2026-09-07 are byte-identical, with SHA-256:

```text
2fac4159fe23aa25581c29f6c90033af3a1126a02593db0bd47e2c10d2c09f19
```

They differ from the 2026-09-02 image only in
`0x00660000-0x0067ffff`. The current block is all `0xFF`; the old capture held
13,040 non-`0xFF` bytes across 14 pages there. **(V)**

The cause is unknown. The old block was not independently reread in September,
so a prior read artifact is not excluded. A real erase or an autonomous
early-stage action is also not excluded. No preserved console log contains an
operator-issued erase or write command. **(V, I)**

The decoded captured version table places `0x00660000` inside
`factory_setting`. **(A)** Earlier labels derived from the two example layouts
were wrong. This state change does not make the block free space, and the
rootfs-only candidate excludes the entire factory allocation.

### Controller-visible OOB was captured, but physical OOB was not

Two complete captures of 32 OOB bytes per page match exactly:

```text
size:    4194304 bytes
SHA-256: a2df824977591738f4d3ce51bd070308a19a58a3b8827af2751b5069428a9251
```

They contain the mirrored `Bbt0` and `1tbB` signatures and version byte. **(V)**
Requests for 64 bytes return the same first 32 followed by 32 zero bytes on
every page. The live ECC layout names 48 ECC bytes at positions 80-127 and
free bytes 2-79, which cannot fit the MTD-reported 64 bytes. **(V)**

The controller-visible capture preserves useful metadata. It omits the
documented physical ECC area and is not a programmer backup. **(I)**

### The five bad pages fail a controlled test

A read-only probe measured `ECCGETSTATS` around one-page reads. Known-good pages
at `0x0fe40000` and `0x0fe46800` left the failed counter unchanged. Each
historical suspect page increased it by one:

```text
0x0fe40800
0x0fe43000
0x0fe45000
0x0fe45800
0x0fe46000
```

The test first failed closed against the writable MTD minor, and the adjacent
good pages prove that a zero delta was observable. **(V)** Normal and MTD raw
mode return the same data; raw mode still increments the failed counter because
this driver routes it through hardware ECC. **(V, D)**

### The allocation map decoded from the captured version tables

Marvell's source defines the version-table records and their CRC check. Eight
copies in the retained capture are byte-identical and pass that check. The
[offline inspector](../tools/nand-inspect/README.md) reproduces these results.
All end addresses below are exclusive. **(A, D)**

| Name | Start | End | Allocation |
|---|---|---|---|
| `block0` | `0x00000000` | `0x00020000` | 128 KiB |
| `pre-bootloader` | `0x00020000` | `0x00120000` | 1 MiB |
| `post-bootloader` | `0x00120000` | `0x00320000` | 2 MiB |
| `postbootloaderB` | `0x00320000` | `0x00520000` | 2 MiB |
| `factory_setting` | `0x00520000` | `0x00a20000` | 5 MiB |
| `tz_en` | `0x00a20000` | `0x00f20000` | 5 MiB |
| `tz_en-B` | `0x00f20000` | `0x01020000` | 1 MiB |
| `bootimgs_B` | `0x01020000` | `0x01a20000` | 10 MiB |
| `bsl` | `0x01a20000` | `0x01f20000` | 5 MiB |
| `bootimgs` | `0x01f20000` | `0x02920000` | 10 MiB |
| `rootfs` | `0x02920000` | `0x08320000` | 90 MiB |
| `app` | `0x08320000` | `0x0fe20000` | 123 MiB |
| `fw_stat` | `0x0fe20000` | `0x0ff20000` | 1 MiB |

The table leaves the final tail unnamed. The recorded flash BBT copies are in
that tail and are excluded from candidate writes.

The rootfs superblock reports 48,831,891 used bytes. `bsl` contains the smaller
auxiliary/recovery SquashFS. These observations and the valid tables replace
the previous heuristic labels. **(A)**

The old file named `installed-kernel-partition.bin` was an 8 MiB carve starting
at `0x00a20000`, inside `tz_en`; its name is not a valid identification. Do not
use that carve as a kernel backup or write target.

Matching allocations do not establish the complete installed slot-selection
algorithm, cryptographic acceptance, or a writer's erase scope.
These offsets belong to the captured unit. Another Invoke needs its own
metadata check rather than inheriting this map by model name.

### The main vendor layout does not fit this unit

The `mtdparts` string in `extracted/ota2/OTA2/gen-cmd.sh` totals 512 MiB and
allocates `192M(rootfs)`. This unit is 256 MiB, so that layout cannot apply.
**(V)**

Separately, `nandrd 10700000 400` returned `Invalid address` on this unit,
showing the accessible address space is smaller than that layout assumes. **(V)**
An earlier draft claimed `0x10700000` was the rootfs *start* under that map. It
is not; it merely falls inside the oversized rootfs allocation. The rejection is
real, the attribution was wrong.

The bundle also ships a different, compact 256 MiB layout in
`79_IMAGE.examples`. **(D)** So there is not one vendor layout to follow but at
least two. Neither example is now used to select this unit's offsets. The
source-decoded 83 descriptors explicitly declare 256 MiB and agree with the
captured version-table allocations instead. **(A)**

## The unknowns that matter

### 1. Whether recovery survives a damaged flash

Yellow mode has been entered dozens of times, always with the original flash
intact. **(V)** During it the device requests a chain of images
(`09_IMAGE → sysinit.img → bootloader.img → drm_erom.img → 79_IMAGE`) and
enumerates as `1286:8174`, `BG2CD S/N:<serial>`. **(V)**

An earlier draft asserted this unit only ever reports `bDeviceSubClass=0xFE` and
used that as evidence against boot-ROM ownership. **That was false.** Captures in
the archive contain both `0xFE` and `0xFF`, and at least one yellow-mode capture
is entirely `0xFF`. **(V)** The claim is withdrawn. Transient `0xFF` stages are
consistent with an early ROM stage participating, but that does not establish
that the whole recovery chain is independent of NAND.

The necessary correction is subtler than the original framing. Even if an
immutable ROM recognises the yellow-mode trigger, recovery also needs the stages
that follow, and community analysis of this platform notes that corruption of
`block0` or the pre-bootloader can defeat recovery even when the trigger is
honoured. **(D)**

So the question is not "does ROM own the trigger" but "does the entire path to a
RAM-loaded U-Boot survive corruption of the region we intend to write, and of any
region a mistaken write could reach".

The exact served `79_IMAGE` is comment-only. The live log shows it executing
`#skip` and immediately displaying the prompt, so that final script issues no
NAND command. **(V)** The preceding `bcm_erom.bin.usb`, `09_IMAGE`,
`sysinit.img`, `bootloader.img`, and `drm_erom.img` are opaque high-entropy
blobs under the available static tools. They expose no useful NAND strings or
literal `0x00660000`, but those negative scans do not prove unreachable code or
NAND independence. **(V, I)**

**Status: recovery after corruption remains unproven.** It is a risk to disclose
for a particular proposed change, not a reason to require whole-chip erasure.

Passive bus tracing can narrow the question only if it observes `CE#`, `WE#`,
`RE#`, `CLE`, `ALE`, and `R/B#`, begins before power, and then detects a known
read command after the prompt as a positive control. Even a clean trace proves
only that no access was observed on those signals.

Two later functional controls answer different questions:

* Reaching the prompt with the original removed and the socket empty proves
  electrical independence only if a post-prompt identification command fails.
* Reaching the prompt with a confirmed blank compatible chip proves independence
  from valid NAND content only if identification succeeds and reviewed boot
  regions still read blank.

Those physical experiments are outside the owner's current constraints. The
software-only plan does not require proving electrical independence from the
entire chip.

### 2. Physical OOB and controller ECC

U-Boot reports 32 bytes of OOB per 2 KiB page. Linux reports 64, while upstream
identification of the Toshiba part reports 128 physical bytes. **(V, D)**

An earlier draft called this a three-way contradiction by adding an
`oobsize=128 bytes` string. That was wrong: the 128 figure comes from a
`Software_Version` text blob inside the secondary SquashFS, which is historical
SDK build metadata, not a description of this controller's geometry. **(V)** It
has been removed from the argument.

Live reads now show that 32 is the meaningful controller-visible window and 64
is the vendor driver's inconsistent MTD declaration. The driver layout refers
to bytes through 127, consistent with the documented 128-byte physical spare
area, but those physical locations have not been read directly. **(V, D, I)**

The controller can plausibly generate hidden ECC during a normal write, but
that behavior has not been verified against a raw programmer read or a bootable
clone. Plausibility is not a write authorization.

The retained source does explicitly route normal page-program transfers through
the configured BCH engine. This is a credible logical-write path to investigate
without manufacturing raw ECC bytes. Its behavior on the target still needs a
bounded installation and verification procedure. **(D)**

**Status: better understood, still unresolved for writing and restoration.**

### 3. Bad blocks and BBT handling

The kernel finds mirrored bad block tables at pages 131008 and 130944, and
records two physical bad blocks at `0x0c000000` and `0x0c020000`. **(V)** A
U-Boot `nandbad` scan reported the same two. **(V)**

An earlier draft claimed Linux "counts six" and framed that as a disagreement
between boot stages. That was a misreading: `mtdblock` reports every block
`_block_isbad()` rejects, which includes blocks deliberately reserved for the BBT
itself. There is no evidence of a disagreement about physical defects, and the
claim is withdrawn.

What remains true is that BBT handling by any writer is unspecified, the BBT
lives partly in OOB the logical backup does not fully contain, and the device
already has real defects plus five pages that cannot be read correctly.

The two data-table copies and their controller-visible OOB signatures are now
captured and unchanged across logical images. They encode the two physical bad
blocks. **(V)** Factory markers and hidden physical OOB remain uncaptured.

**Status: unknown handling, on a device with known defects.**

### 4. Whether any persistent image would boot at all

An earlier draft omitted this entirely. Project notes record that kernel signing
is expected and that rootfs verification and slot selection remain unknown.
**(D)** Nothing here demonstrates that a modified persistent image would satisfy
whatever the boot chain checks.

Separately, the validated RAM initramfs is about 30.9 MiB. The captured table
allocates 10 MiB to each boot-image region and 90 MiB to rootfs. A rootfs-only
design therefore has a plausible size envelope, but it still requires a
measured build and a startup adapter, not a claim that the existing RAM file
can be written unchanged. **(A, I)**

### 5. Why the factory-setting block changed

The current logical image has one newly blank erase block at `0x00660000`.
**(V)** No captured operator command explains it. The transition may have
occurred in a boot stage, stock runtime, earlier experiment, or the old read
path. **(I)** Its time and mechanism are unknown.

No candidate installation will intentionally alter this factory allocation.
The anomaly remains recorded; it does not identify the rootfs as unsafe or
justify erasing the factory area.

## What a safe write would require

| Precondition | Status |
| --- | --- |
| A 256 MiB allocation map for this unit | **Established from archived metadata.** Eight CRC-valid captured tables agree with both 83 packages. Active selection is separate. |
| Recovery after failure of the proposed change | **Unproven.** Must be evaluated for the selected target, with residual risk stated. |
| Appropriate ECC write path | **Source-backed candidate.** Normal writes use hardware BCH; raw-mode readback is not raw and verification must check ECC counters. |
| Target-specific backup and restoration | **To design.** Current captures preserve logical rootfs and visible OOB, not a complete raw chip image. |
| Preserved factory and status data | **Required boundary.** Exclude the changed factory block, `fw_stat`, and BBT tail. |
| Modified image satisfies signing and verification | **Unproven.** Signing expected, never tested. |
| Known slot-selection behaviour | **Unknown.** |
| Power-loss-safe erase and write sequencing | **Not designed.** No reviewed installer exists. |
| Readback verification after write | **Not designed.** |
| Image fits its target region | **To measure.** Rootfs has 90 MiB; a new persistent build has not been validated. |

## Options, with honest trade-offs

**Do nothing and keep booting from RAM.** This adds no intentional NAND-write
risk. The platform already works this way including audio, Bluetooth, physical
controls and provisioning. The unit needs a host to boot.

**Develop a scoped software-only installation.** Use the decoded rootfs
allocation, qualify its startup and writer behavior, and present the smallest
change with explicit risk before writing it.

**Write anyway.** This bets a one-of-a-kind unit on assumptions this repository's
own review declined to make, with a backup that cannot restore OOB and contains
five pages of unverified data.

**Remove the risk instead of accepting it.** A second unit, or a removable
original plus a verified replacement-chip clone, changes the calculus because a
mistake occurs on replaceable media. An improvised in-circuit clip is not
automatically safe; it can cause bus contention or back-power the board.

## What comes before the first write

1. Finish choosing the smallest startup change, preferably without changing
   kernel or early boot records.
2. Validate its filesystem, runtime paths, vendor-service exclusions and kernel
   compatibility in a nonpersistent rehearsal where possible.
3. Specify exact erase-block bounds, ECC behavior, backup and readback checks.
4. State the expected restoration path and its limits, including the
   consequence of power loss during this operation.
5. Have the owner select the current or another unit and approve this specific
   write. Reserve a hardware window instead of interrupting microphone work.

A complete destructive-recovery research campaign is not part of these steps.

## Recommendation

Continue the scoped persistence work. The actual 90 MiB rootfs allocation is
now established from the capture rather than guessed from example scripts.
This is meaningful progress toward a no-disassembly installation.

Do not execute the withdrawn full-erase runbook. The next decision is a
particular startup change with a reviewed footprint and an honest DIY-risk
statement, not whether every imaginable NAND failure is recoverable.

The owner retains the decision on unit choice and the first actual write.
