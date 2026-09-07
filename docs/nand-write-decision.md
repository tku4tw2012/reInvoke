# NAND write: the evidence for a go or no-go decision

This document exists so the owner can decide whether to write this unit's NAND.
It does not make the decision. It separates what is proven on this unit from
what is assumed, and it states plainly which unknowns are capable of ending the
project.

Every claim is labelled:

* **(V)** verified on this physical unit
* **(D)** vendor, SoC, or community documentation
* **(I)** inference, clearly reasoned but not proven

## The short version

Writing NAND is not blocked by the replacement platform. The platform is ready.
It is blocked by three unknowns about the hardware, and one of them can turn a
mistake into a permanently dead speaker.

The single question that decides everything: **does yellow mode survive a
corrupted NAND?** Yellow mode is the only recovery path that has ever been used
on this unit, and it has only ever been entered while the original flash was
intact. Whether it is owned by immutable mask ROM or by code stored in NAND is
**not established**. If it is the latter, a bad write removes the escape hatch
that would be needed to fix the bad write.

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

### A full backup exists, with one serious caveat

`hardware/dumps/20260902T215700Z-native-ram/invoke-nand-data.bin` is exactly
268,435,456 bytes and its recorded SHA-256 was re-verified for this document as
`edf38ef2af48d249c9925ebb6a94c716cfdb2c1ce575fb704283918cdd0e53be`. **(V)**

It is a real capture, not a sparse or zeroed file. Sampling 512 regions across
the device found 213 with real content and 298 fully erased, which is the
expected shape for a partly used NAND. The `hsqs` SquashFS magic appears exactly
at the offset the capture notes recorded. **(V)**

The caveat is decisive: `nand-dd.log` shows `131072+0 records` of 2048 bytes,
which is the data area only. **The backup contains no OOB bytes.** ECC syndromes,
bad-block markers, and Marvell metadata live in OOB and are not in this file.
It is a filesystem-level backup, not a programmer-level one, and it cannot by
itself restore a device whose OOB has been disturbed. **(V)**

### The partition map, reconstructed from the dump

Scanning the dump directly produced this region map. Regions are 128 KiB erase
blocks; "used" means the leading bytes are not all `0xFF`. **(V)**

| Offset | Size | Content |
| --- | --- | --- |
| `0x00000000` | 128 KiB | erased |
| `0x00020000` | 1.25 MiB | structured header, high entropy; boot stage |
| `0x00100000` | — | byte-identical header to `0x00020000`, redundant copy |
| `0x00a20000` | 8 MiB | **kernel partition, byte-exact match** |
| `0x01a20000` | 2.7 MiB | SquashFS, `SDK Tools_v2`, 2017-06-22 |
| `0x02920000` | ~46.6 MiB | SquashFS, `Barracuda_libre-12.2050.3`, 2021-02-04 |
| `0x0c000000` | — | known bad block |
| `0x0ffc0000` | 256 KiB | in use at end of device; BBT region per vendor map |

The kernel offset is not an inference. The previously extracted
`installed-kernel-partition.bin` was compared byte for byte against the dump at
several candidate offsets and matched exactly at `0x00a20000`, and nowhere else.

Two 8.38 MiB used regions also appear at `0x01020000` and `0x01f20000`, exactly
`0xF00000` apart, and the two SquashFS images are separated by the same
`0xF00000` stride. That regular spacing is suggestive of a paired slot layout but
the contents were not identified, so it stays **(I)**.

### The documented layout is wrong for this unit

The OTA2 `mtdparts` string in `extracted/ota2/OTA2/gen-cmd.sh` sums to 512 MiB.
This unit is 256 MiB. This is not a paperwork mismatch; it was proven live:
`nandrd 10700000 400`, the address where `rootfs` would begin under that map,
returned `Invalid address` on this unit. **(V)**

Any procedure derived from the shipped vendor layout would target addresses that
do not exist here.

## The three unknowns that matter

### 1. Yellow mode ownership — the project-ending one

Yellow mode has been entered dozens of times, always with the original flash
intact. **(V)** During it the device requests a chain of images
(`09_IMAGE → sysinit.img → bootloader.img → drm_erom.img → 79_IMAGE`) and
enumerates as `1286:8174`, `BG2CD S/N:<serial>`. **(V)**

What is not known is which stage decides to enter it. If an immutable boot ROM
selects yellow mode, then NAND contents cannot remove the recovery path and the
risk of a bad write is largely bounded. If instead a pre-bootloader stored in
NAND, or U-Boot's own environment, makes that decision, then a bad write can
remove the means of recovery.

The repository's own prior review rejects the optimistic reading: the component
emitting the recovery enumeration "has not been proven to be immutable mask
ROM", and the documented `99_IMAGE` failure is cited as a counterexample to
universal recovery through that path. **(D)**

One concrete data point cuts against assuming ROM ownership: this unit reports
`bDeviceSubClass=0xFE` across sixteen captures, not `0xFF`, meaning the boot
ROM's own `usb_boot` branch was never the one observed. The later image chain was
served by something else, and what that something is has not been identified.
**(V)**

**Status: unproven, and it is the deciding question.**

### 2. The OOB width disagreement

U-Boot reports 32 bytes of OOB per 2 KiB page. Linux on this same unit reports
64 bytes. **(V)** Both cannot be describing the same geometry correctly.

This matters because ECC syndrome placement depends on it. If a write is
performed with one assumption and the reader uses the other, the data is
unreadable and the affected region will not boot. This is exactly the class of
error that produces a device that looks bricked while the flash chip is
physically fine.

Additionally, the vendor NAND image header inside the dump describes
`pagesize=2048 bytes, 64 pages per block, oobsize=128 bytes`, a third value
again. **(V)** Three sources, three numbers, none reconciled.

**Status: unresolved, and it must be settled before any write.**

### 3. Bad blocks already exist, and the two boot stages disagree about how many

The device already has bad blocks. Today's boot log records them directly:
`nand_read_bbt: bad block at 0x00000c000000` and `0x00000c020000`. **(V)**

A U-Boot `nandbad 0 2048` scan reported those same two blocks plus one
uncorrectable ECC page at `0x0FE40800`. **(V)**

But Linux, in the same boot log, reports `mtdblock0: 6 bad block(s) found`.
**(V)** U-Boot found two, Linux counts six. That is a third disagreement between
the two boot stages about the state of the same flash, alongside the OOB width
conflict, and it points at the same underlying cause: the two stages do not share
a view of the OOB area where bad-block markers live.

Whether a writer would consult the BBT, skip bad blocks, or fail on them is not
established, nor is whether the BBT itself is stored in OOB. Since the backup has
no OOB, a BBT rebuilt incorrectly after an erase could mark good blocks bad or,
worse, hand out blocks that are actually failing.

**Status: unknown handling, on a device already known to have defects, where the
two boot stages cannot even agree how many.**

## What a safe write would require

These are the preconditions. The point of listing them is that most are not yet
met.

| Precondition | Status |
| --- | --- |
| Correct 256 MiB partition map | **Largely met.** Kernel offset proven, two SquashFS offsets proven, boot region located. Slot semantics still inferred. |
| Recovery survives a bad write | **Unproven.** The deciding unknown. |
| Correct OOB/ECC layout for writing | **Unproven and actively contradicted**, three different values. |
| Restorable backup | **Partially met.** Data-area image exists and verifies; no OOB, so not a programmer-level restore. |
| Known-good write tooling | **Not established.** No reviewed installer exists in this repository. |
| Image fits its target region | **Not met.** The RAM initramfs is about 31 MiB against an 8 MiB kernel region; a persistent design would differ from what has been validated. |

## Options, with honest trade-offs

**Do nothing and keep booting from RAM.** Costs nothing, risks nothing, and the
platform already works this way including audio, Bluetooth, physical controls
and provisioning. The unit needs a host to boot.

**Resolve the unknowns before writing.** Determine yellow mode's owner, reconcile
OOB, and capture a true programmer-level image. This is real work and some of it
may require physical access to the flash, but it converts an unbounded risk into
a bounded one.

**Write anyway.** The honest framing is that this bets a one-of-a-kind unit on an
assumption the repository's own review explicitly declined to make.

**Take the risk off the table first.** A second unit, or a NAND programmer clip
that can read and restore OOB, changes the calculus entirely, because then a
mistake is recoverable rather than final.

## What would move this to a go

In rough order of value:

1. Establish which boot stage selects yellow mode. If it is mask ROM and can be
   demonstrated with NAND deliberately made unbootable in a reversible way, the
   dominant risk collapses.
2. Reconcile the OOB width across U-Boot, Linux, and the vendor header, and
   determine which applies when writing.
3. Capture a true raw image including OOB, so a restore is possible.
4. Write and review an installer that targets the verified offsets, verifies
   after write, and handles bad blocks explicitly.

Only after those does the size and slot question become the main problem.

## Recommendation

Not yet. Not because the software is unready, but because the recovery path is
unproven and the backup cannot restore OOB. The cost of waiting is that the
speaker needs a host to boot. The cost of being wrong is the speaker.

That trade is the owner's to make, and this document exists so it can be made on
evidence rather than optimism.
