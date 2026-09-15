---
title: The bootimgs record and why a custom kernel does not boot from NAND
description: Measured layout of the NAND boot records, two failed kernel-replacement attempts, and what would have to be true to succeed
ms.date: 2026-09-15
ms.topic: concept
---

## Summary

Replacing `rootfs` and `bsl` works. Replacing `bootimgs` does not. Two attempts
failed on this unit, and the reason is the format of the image `bootimgs`
carries, not signature enforcement.

USB ADB needs a custom kernel, a custom kernel needs `bootimgs`, and `bootimgs`
needs an image this project cannot currently produce. Network ADB over Wi-Fi is
unaffected and remains available on a NAND boot.

## Which records are safe to replace

Read from the vendor image and from a full NAND capture of this unit.

| Record | First bytes | Form | Safe to replace |
| ------ | ----------- | ---- | --------------- |
| `rootfs` | `68737173` (`hsqs`) | raw SquashFS | yes, proven |
| `bsl` | `68737173` (`hsqs`) | raw SquashFS | yes, proven |
| `bootimgs` | 40 zero bytes, then a descriptor | container | **no** |
| `block0`, `pre-bootloader`, `post-bootloader`, `tz_en`, `app` | — | vendor | never changed |

Every candidate through 05.8 replaced only the two SquashFS records. Each
booted. Candidates that changed `bootimgs` did not.

## Layout inside the bootimgs record

```text
+0x00000  128-byte descriptor: load address 0x02008000, size, padded size
+0x00080  embedded cmdline text, including the real mtdparts
+0x20000  a three-slot key-store table, 0xc00 (3,072) bytes, see below
+0x20c00  the image body the table's third slot describes
+0x4e0000 a gzip newc cpio for kernel 2.6.35.4, stale and unrelated
```

The embedded cmdline settles a question the earlier layout notes got wrong:

```text
mtdparts=mv_nand:128K(block0),1M(pre-bootloader),2M(post-bootloader),
2M(postbootloaderB),5M(factory_setting),5M(tz_en),1M(tz_en-B),10M(bootimgs_B),
5M(bsl),10M(bootimgs),90M(rootfs),123M(app),1M(fw_stat)
```

The compact 256 MiB layout applies, not the 512 MiB one in the OTA2 bundle. It
places `bsl` at `0x01a20000`, `bootimgs` at `0x01f20000` and `rootfs` at
`0x02920000`, all confirmed against the capture. An earlier note calling
`0x00a20000` an 8 MiB kernel container was wrong; that offset is `tz_en`.

## The 44-byte header is slot one of a three-slot table, not the whole header

What earlier notes called "a 44-byte Marvell header" is the first field of a
larger, fixed-size table. This was found by directly re-examining the retained
NAND capture and four staging files, then checked against the real bootloader
source that defines it — not taken from any single source on faith.

**Measured** (`+0x20000` in `bootimgs`, i.e. byte 0 of what was called the
44-byte header, checked against the full 256 MiB capture
`current-main-256MiB.bin`, SHA-256 `33f16be0d...638489f`, and independently
against `sysinit.img`, `bootloader.img`, `bcm_erom.bin.usb` and `drm_erom.img`
in the archive staging set): a sliding-window Shannon-entropy scan finds three
short, low-entropy, structured regions, each starting an otherwise-featureless
1,024-byte span, at relative offsets `+0x000`, `+0x400` and `+0x800`. No fourth
such region appears anywhere in the next 8 KiB scanned. All three offsets and
their leading bytes are identical across all five files:

| Offset | First 8 bytes | Reads as (LE) |
| ------ | ------------- | ------------- |
| `+0x000` | `01 00 00 00 37 c2 xx xx` | version 1, type `0xc237` |
| `+0x400` | `01 00 00 00 e1 a2 01 00` | version 1, type `0xa2e1` |
| `+0x800` | `01 00 00 00 xx xx xx xx` | version 1, type varies by file |

**Documentation** (the real bootloader source for this SoC family,
`nest-open-source.googlesource.com/manifest_repos/bootloader`, commit
`836ad32e08388e0e4ce8d03fe4f14d2c3ea8ba13`,
`berlin_tools/bootloader/bootloader.c`, fetched and read directly, not
summarized secondhand): these are exactly `MV_KEY_STORE_TYPE_AESK` (`0xc237`)
and `MV_KEY_STORE_TYPE_RSAK` (`0xa2e1`), two members of a `{ version; type; }`
tagged-record convention (`MV_KEY_STORE_HEAD`) used throughout this file. A
`MV_LASTK_IMAGE` union defines exactly this three-slot, 1,024-byte-per-slot
layout (its `h3` variant: `custk`, `extrsak`, `image`, `0xc00` bytes total) for
a kernel image that carries both a customer AES key and an RSA key record.
`load_lastk()` writes `MV_KEY_STORE_TYPE_ENDK` (`0x0f01c0de`, masked to
`0xc0de`) into the third slot's type field before handing the whole structure
to `bcm_image_verify()` for the kernel-loading path (gated `#if BG2CDP`,
confirmed present and active for this chip family). Measured against this
unit: the kernel file's third-slot type is `0x0402c0de`, whose low 16 bits
(`0xc0de`, the only bits `MV_KEY_STORE_TYPE_MASK` checks) match `ENDK`
exactly; the four non-kernel files (`sysinit.img`, `bootloader.img`,
`bcm_erom.bin.usb`, `drm_erom.img`) instead read `0xc3bb`-based values there,
which do not match any constant found in this source file. That is consistent
with the kernel specifically going through `load_lastk`/`bcm_image_verify`
while the other four files are consumed by a different, untraced code path.

A second, independent field match: the same source defines
`CPU_IMG_OFFS_IMGSIZ` as byte offset 40 (`0x28`) into a "CPU image" structure,
holding the image's length. At absolute offset `+0x828` (slot three, `+0x28`),
this unit's kernel file reads `fe 5f 4b 00`, decoding as `0x004b5ffe` =
4,939,774 bytes (4.71 MiB) — read directly from the capture, not asserted.
`CPU_IMG_OFFS_USRDATA` (byte offset 10) is defined as `0x0` for a NAND-sourced
image and `0xA33A` for a USB-sourced one; this unit's file reads `0x0` at that
position, consistent with a NAND boot.

**Inference, not yet fully traced**: the actual runtime consumer of this
per-file copy of the three-slot table remains unconfirmed. `load_lastk()`, as
read, populates its working copy from a separate, small, redundant "version
table" area (the last 4 KiB of NAND blocks 1 through 8), not from the
`bootimgs` image itself, then aliases a kernel-specific sub-field of that copy
against whatever memory the kernel image was just read into. Whether that is
the exact mechanism that reads and checks the copy embedded in the file itself
was not established from source reading alone; `bcm_image_verify()` on this
chip family is a mailbox call into the separate BCM co-processor, and the
processor's own firmware (closed, not in this repository) is very likely
where the per-file table is actually parsed. This is the same "closed
co-processor" boundary the payload-encryption question below runs into.

**What this changes**: the region that needs to be understood and reproduced
is not a 44-byte header followed immediately by payload. It is a fixed,
0xc00-byte (3,072-byte) three-slot table — customer AES key, RSA key record,
and a per-file third slot — followed by the actual image body starting at
`+0xc00`. The 44-byte boundary previously documented is real (it is where the
low-entropy, human-readable part of slot one ends) but it is not where the
header ends; slot one is padded with what is very likely wrapped key material
out to the full 1,024 bytes, same as slots two and three.

## The two failed attempts

| Candidate | What it wrote to `bootimgs` | Result |
| --------- | --------------------------- | ------ |
| 06.0 | a bare uImage at offset 0, both slots | no boot |
| 6.1 | vendor descriptor kept, uImage spliced at `+0x20000`, `bootimgs_B` left vendor | no boot |

06.0 destroyed the descriptor and left nothing at `+0x20000`. 6.1 corrected
the placement and still failed. Given the finding above, 6.1's splice point
was also wrong in a way not previously documented: writing a bare uImage
starting at `+0x20000` overwrites the entire three-slot table, not just a
44-byte header, and places kernel bytes where the AES-key, RSA-key and
image-descriptor slots belong instead of at the body's real start, `+0x20c00`.
Either defect alone would be expected to fail; which one actually caused the
observed failure was not isolated, and does not need to be, since a correct
attempt must fix both.

## Signing is not the barrier

The boot ROM prints its own fuse state during recovery, before NAND is touched:

```text
OTP LOCK     :0
RKEK CRC     :ECBB4B55
AESK0 CRC    :ECBB4B55
SIGNK7 CRC   :190A55AD
MRVL SIGN R  :0000
CUST SIGN R  :0000
```

Both signature-required registers read zero and the fuses are unlocked. These
are checksums of key slots in the SoC, not of any image this project writes.

A second measurement points the same way. `bcm_erom.bin.usb` and the NAND
`pre-bootloader` share a byte-identical 44-byte header while 23,624 of 24,532
payload bytes differ, so that header cannot contain a hash or signature over
the payload.

A direct byte comparison of `sysinit.img` against `bootloader.img` reproduces
the same exact cut point: identical through byte 43, diverging at byte 44
(`0x2c`), with every one of the next 512 payload bytes differing. This pair's
header carries class byte `0x01` at offset 6, versus `0x00` for the
`bcm_erom.bin.usb`/`pre-bootloader` pair above. Two independent pairs, in two
different header classes, landing on the same boundary is evidence the cut is
structural, not a coincidence of one file pair. This left open, at the time,
whether a longer enclosing structure with per-instance data starting at byte
44 existed. It does: see the three-slot table described above. The RSA-key
slot's presence in the format is a provision for signature verification, not
evidence it is active; it does not conflict with the fuse readout above, which
still shows enforcement off on this specific unit.

## What the payload is, and is not

Measured directly at `bootimgs+0x20c00` (the body start established above),
re-run at that corrected offset rather than assumed carried over from the old
`+0x2002c` figure:

* not zlib, gzip, bzip2 or lzma: no valid stream at any tested offset in 4 MiB;
  the handful of 2-byte gzip-magic byte pairs found are within the count
  expected from chance alone in random data, not a real stream
* no ECB structure: zero repeated 16-byte blocks across the full 4 MiB checked
* entropy 8.00, which does not separate encryption from compression, since
  `bsl` is plain SquashFS and also measures 8.00
* no uImage magic, no zImage magic and no ARM NOP sled anywhere checked

`81_IMAGE`, the recovery kernel, is a plain uImage and accepts a custom build.
That is why a custom kernel boots from RAM and not from NAND: the recovery path
takes a uImage, the NAND path does not.

## What would have to be true first

1. The three-slot table (`0xc00` bytes: customer AES key, RSA key record, a
   per-file third slot) is understood well enough to generate one, not just
   the 44-byte customer-key record that begins it.
2. The transform applied to the body starting at `+0xc00` is identified, or
   shown to be optional.
3. Either is demonstrated without a flash cycle, because each guess currently
   costs one non-booting boot.

Until then, keep `bootimgs` at the vendor image.

## Rejected: overriding the U-Boot environment

U-Boot reports `environment in SPI flash is invalid` and falls back to
compiled-in defaults whose `bootcmd` is a Marvell TFTP/NFS development setting.
`saveenv` would make a stored environment valid and could point `bootcmd` at a
kernel loaded from NAND, bypassing `bootimgs`.

It was rejected on evidence. If the NAND boot consulted that `bootcmd`, an
invalid environment would make every boot attempt a TFTP fetch from a Marvell
lab address, and the unit would never start. It starts reliably, so the NAND
path does not use that variable.

The risk is also asymmetric. The recovery U-Boot reads the same environment.
Today it finds it invalid and waits at its prompt, which is exactly what
yellow-mode recovery depends on. A valid environment carrying a `bootcmd` may
autoboot instead of waiting, putting the one path that has never failed at
risk for a lever the evidence says is not connected.

## Verified on this unit

* A custom kernel boots from RAM and brings up USB ADB: `/sys/class/udc`
  populated with `f7ed0100.udc`, `mv-udc` bound, `adb ... usb:2-1.2`.
* The runtime prefers an existing USB gadget and falls back to TCP 5555, so a
  NAND boot on the vendor kernel still offers network ADB.
* No loadable UDC module ships in either module tree, so USB ADB cannot be
  added to the vendor kernel without replacing it.
* Neither failed attempt damaged the unit. The bad-block list is unchanged at
  `0x0c000000` and `0x0c020000`, and yellow mode continued to work throughout.
