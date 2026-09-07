# NAND write: the evidence for a go or no-go decision

This document exists so the owner can decide whether to write this unit's NAND.
It does not make the decision. It separates what is proven on this unit from
what is assumed, and it states plainly which unknowns are capable of ending the
project.

An earlier draft overstated several claims. Those errors are corrected here and
called out where they were load-bearing, because a decision this consequential
should not rest on tidier evidence than actually exists.

Every claim is labelled:

* **(V)** verified on this physical unit
* **(D)** vendor, SoC, or community documentation
* **(I)** inference, clearly reasoned but not proven

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

## The short version

Writing NAND is not blocked by the replacement platform. The platform is ready.
It is blocked by unresolved questions about the hardware, and by the discovery
that the existing backup is not as trustworthy as its checksum suggests.

Two things dominate the risk:

1. **The recovery path is unproven against a damaged flash.** Yellow mode has
   only ever been entered with the original flash intact.
2. **The backup contains bytes that were never read correctly.** Five pages
   produced uncorrectable ECC errors during the capture itself.

## What is genuinely known about the flash

### Geometry

The running kernel reports a single unpartitioned device. **(V)**

```
mtd1: 10000000 00020000 "mv_nand"
NAND device: Manufacturer ID: 0x98, Chip ID: 0xda (Toshiba 256MiB 8-bit),
256MiB, page size: 2048, OOB size: 64
```

`0x10000000` is 268,435,456 bytes, so this is a 256 MiB part with a 128 KiB
erase block. No partition table is published by this kernel, so there is no
offset map to write against without reconstructing one.

### The backup is a complete-length capture, not a proven-good one

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

### The partition map, reconstructed from the dump

Scanning the dump directly produced this region map. Regions are 128 KiB erase
blocks; "used" means the leading bytes are not all `0xFF`. **(V)**

| Offset | Size | Content |
| --- | --- | --- |
| `0x00000000` | 128 KiB | erased |
| `0x00020000` | 1.25 MiB | structured header, high entropy |
| `0x00100000` | — | byte-identical header to `0x00020000` |
| `0x00a20000` | 8 MiB | container matching the extracted kernel partition |
| `0x01a20000` | 2.7 MiB | SquashFS, `SDK Tools_v2`, 2017-06-22 |
| `0x02920000` | ~46.6 MiB | SquashFS, `Barracuda_libre-12.2050.3`, 2021-02-04 |
| `0x0c000000`, `0x0c020000` | — | bad blocks recorded by the kernel BBT |
| `0x0ffc0000` | 256 KiB | in use at end of device |

Two further 8.38 MiB used regions appear at `0x01020000` and `0x01f20000`,
exactly `0xF00000` apart, and the two SquashFS images share that same stride.
The spacing is suggestive of a paired slot layout, but the contents were not
identified, so this remains **(I)**.

An earlier draft called the `0x00a20000` offset "proven, not an inference". That
was wrong, and the reasoning was circular: `installed-kernel-partition.bin` was
itself carved out of this dump, so matching it back only proves the carve is
faithful. An exhaustive block-aligned scan does show the 8 MiB file occurs there
and nowhere else, which is a real boundary result **(V)**, but the label "kernel"
comes from a vendor layout **(D)**, not from independently identifying executable
kernel content at that address.

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
least two, and which applies to this production unit is unsettled.

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

**Status: unproven, and it is the deciding question.**

### 2. The OOB width disagreement between boot stages

U-Boot reports 32 bytes of OOB per 2 KiB page. Linux on this same unit reports
64 bytes. **(V)**

An earlier draft called this a three-way contradiction by adding an
`oobsize=128 bytes` string. That was wrong: the 128 figure comes from a
`Software_Version` text blob inside the secondary SquashFS, which is historical
SDK build metadata, not a description of this controller's geometry. **(V)** It
has been removed from the argument.

The remaining 32-versus-64 discrepancy is genuine but should not be overstated
either. The two numbers may describe different things, a controller-visible
window versus the physical spare area, and disagreement alone does not prove a
writer would choose the wrong ECC layout. What it does mean is that the
authoritative geometry for *writing* has not been established, and ECC syndrome
placement depends on getting it right.

**Status: unresolved, and it must be settled before any write.**

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
lives in the OOB area the backup does not contain, and the device already has
real defects plus five pages that cannot be read correctly.

**Status: unknown handling, on a device with known defects.**

### 4. Whether any persistent image would boot at all

An earlier draft omitted this entirely. Project notes record that kernel signing
is expected and that rootfs verification and slot selection remain unknown.
**(D)** Nothing here demonstrates that a modified persistent image would satisfy
whatever the boot chain checks.

Separately, the validated RAM initramfs is about 30.9 MiB against an 8 MiB
container, so a persistent design would not be the artifact that has been tested.
It would be a new, unvalidated one.

## What a safe write would require

| Precondition | Status |
| --- | --- |
| A 256 MiB layout that applies to this unit | **Partly met.** Content boundaries proven; labels and slot semantics come from conflicting vendor layouts. |
| Full recovery path survives corruption of the target | **Unproven.** The deciding unknown. |
| Authoritative OOB/ECC geometry for writing | **Unproven.** 32 vs 64 unresolved. |
| A restorable backup | **Not met.** Complete-length capture, but no OOB and five uncorrectable pages. |
| Modified image satisfies signing and verification | **Unproven.** Signing expected, never tested. |
| Known slot-selection behaviour | **Unknown.** |
| Power-loss-safe erase and write sequencing | **Not designed.** No reviewed installer exists. |
| Readback verification after write | **Not designed.** |
| Image fits its target region | **Not met.** ~30.9 MiB validated artifact vs 8 MiB container. |

## Options, with honest trade-offs

**Do nothing and keep booting from RAM.** Costs nothing, risks nothing. The
platform already works this way including audio, Bluetooth, physical controls and
provisioning. The unit needs a host to boot.

**Resolve the unknowns first.** Establish that the full recovery path survives a
damaged target region, settle the write-time OOB geometry, and capture a true raw
image including OOB. This converts an unbounded risk into a bounded one.

**Write anyway.** This bets a one-of-a-kind unit on assumptions this repository's
own review declined to make, with a backup that cannot restore OOB and contains
five pages of unverified data.

**Remove the risk instead of accepting it.** A second unit, or a programmer clip
that can read and restore OOB directly, changes the calculus entirely, because a
mistake becomes recoverable rather than final.

## What would move this to a go

1. Demonstrate that the complete path to a RAM-loaded U-Boot survives corruption
   of the intended target region, and of regions a mistaken write could reach.
2. Establish the authoritative OOB and ECC geometry used when writing.
3. Capture a true raw image including OOB, and re-read the five uncorrectable
   pages until they either read consistently or are known-lost.
4. Establish what the boot chain verifies, and whether a modified image passes.
5. Determine slot-selection behaviour.
6. Write and review an installer that targets verified offsets, sequences erase
   and write to tolerate power loss, handles bad blocks explicitly, and verifies
   by readback.

Only after those does the size and layout question become the main problem.

## Recommendation

Not yet, and the case for waiting got stronger rather than weaker while this
document was being checked. The backup is less trustworthy than its checksum
implied, and three claims that made the picture look tidier turned out to be
wrong.

The cost of waiting is that the speaker needs a host to boot. The cost of being
wrong is the speaker.

That trade is the owner's to make, and this document exists so it can be made on
evidence rather than optimism.
