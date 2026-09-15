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
`0xc0de`) into its own working copy's third-slot type field before handing
that copy to `bcm_image_verify()` (gated `#if BG2CDP`, confirmed present and
active for this chip family). Measured against this unit: the kernel file's
own third-slot type is `0x0402c0de`, whose low 16 bits (`0xc0de`, the only
bits `MV_KEY_STORE_TYPE_MASK` checks) match `ENDK` exactly; the four
non-kernel files (`sysinit.img`, `bootloader.img`, `bcm_erom.bin.usb`,
`drm_erom.img`) instead read `0xc3bb`-based values there, which do not match
any constant found in this source file. This match is real, but — see the
correction below — `load_lastk()`'s own copy is sourced from a different NAND
location than this file, so a matching type value here is evidence of a
shared build-time convention (the kernel file is the one everyone expects to
carry a final/`ENDK`-tagged record), not evidence that `load_lastk` itself
reads these exact bytes.

A second, independent field match: the same source defines
`CPU_IMG_OFFS_IMGSIZ` as byte offset 40 (`0x28`) into a "CPU image" structure,
holding the image's length. At absolute offset `+0x828` (slot three, `+0x28`),
this unit's kernel file reads `fe 5f 4b 00`, decoding as `0x004b5ffe` =
4,939,774 bytes (4.71 MiB) — read directly from the capture, not asserted.
`CPU_IMG_OFFS_USRDATA` (byte offset 10) is defined as `0x0` for a NAND-sourced
image and `0xA33A` for a USB-sourced one; this unit's file reads `0x0` at that
position, consistent with a NAND boot.

**Correction after tracing the actual kernel-load function, not just the
struct definitions it uses**: an earlier version of this section said the
per-file table was "consistent with the kernel specifically going through
`load_lastk`/`bcm_image_verify`." That causal claim was checked directly
against the real kernel-loading code (`Image_Load_And_Start()`, the Linux
build's variant, `bootloader.c:1812`, distinct from a same-named
Android-boot variant at `bootloader.c:1732` gated by a separate `#if`) and
does not hold up as written. It is weaker than previously stated, not
stronger, and the correction is recorded here rather than quietly folded in.

**Documentation, newly traced**: `Image_Load_And_Start()` reads a header
(`Image3_Attr`, global `img3_hdr`, declared at `bootloader.c:278`) starting
at the NAND address given by a version-table entry, `vt_img3`, that this
function's own comment block identifies by name: the Linux-path variant of
this function is introduced by the comment "For Linux ... Android needs to
load SM from bootimgs" (`bootloader.c:1808-1811`), and the Android-path
variant carries the matching comment "For Android not need to load SM from
bootimgs ... save the space of flash memory of bootimgs & bootimgs-B"
(`bootloader.c:1724-1729`). `vt_img3` itself is populated earlier by matching
a constant, `IMG3_NAME`, against each version-table entry's stored name
(`bootloader.c:2235`, exact call: `UtilMemCmp(IMG3_NAME, vt_entry->name,
sizeof(IMG3_NAME))`). `IMG3_NAME`'s literal string value is not defined in
`bootloader.c` itself — it comes from one of two dozen included headers not
fetched — so this is not a byte-grepped proof that `IMG3_NAME == "bootimgs"`.
It rests on the file's own comment vocabulary, in the exact function that
consumes `vt_img3`, consistently naming the thing it reads "bootimgs" both
times the function is discussed. That is strong internal corroboration, not
a gap-free proof; it is recorded as documentation-level, not
verified-on-this-unit.

Once found, `Image3_Attr` is used to compute the kernel (`cpu0`) sub-image's
NAND address by chaining forward from the header through an SM sub-image,
using each sub-image's own recorded size (`get_next_img_addr`,
`bootloader.c:1881-1884`; the header itself is read starting at
`vt_img3.part2_start_blkind * iBlockSize`, confirmed at
`bootloader.c:1846-1850`, via `nand_read_generic(..., &img3_hdr, ...)` at
`bootloader.c:1859`, so `Image3_Attr` sits at the very start of whichever
region `vt_img3` designates). Whether the kernel image is treated as
encrypted at all is decided by `if(cpu0_hdr->bcpu0_image_encrypt)`
(`bootloader.c:2023`, full conditional block `2023-2046`) — **a flag inside
`Image3_Attr`, not inside the three-slot table measured above.** Only when
that flag is set (and only in the `#if BG2CDP` branch already confirmed
active for this chip family; a sibling `#else` branch at `2037-2044` calls a
different function, `VerifyImage()`, on non-`BG2CDP` builds) does the code
call `load_lastk(g.lastk)` followed by
`bcm_image_verify(BCM_IMG_KERNEL_TYPE, ...)` (`bootloader.c:2025,2031`).
Either way, once past that point, the resulting buffer is read as
`linux_hdr_t` (fields `kernel_size`, `ramdisk_size`, `ramdisk_addr`, used
directly at `bootloader.c:2051-2060`, outside and after the encrypt-flag
block, so this step always runs) — a **third** header type, distinct from
both `MV_KEY_STORE_HEAD` and `Image3_Attr`.

Neither `Image3_Attr` nor `linux_hdr_t` is defined in the one file fetched
(`bootloader.c`); both are used as already-declared types, so their field
offsets and total sizes are not available from this source alone, and were
not guessed at.

**What this leaves unresolved, stated plainly**: three things that were
previously read as one connected story turn out to be three separate,
imperfectly-connected facts.

1. `load_lastk()` populates `g.lastk` from the version-table area (last 4 KiB
   of NAND blocks 1 through 8, confirmed exactly:
   `UtilMemCpy(g.lastk, &g.partition_info_buff[3*1024], 1024)`,
   `bootloader.c:2227`) — **not from `bootimgs`.** Even on the branch where
   `load_lastk`/`bcm_image_verify` do run, they do not read the specific
   bytes measured at `bootimgs+0x20000`.
2. Whether `cpu0_hdr->bcpu0_image_encrypt` is set on this unit is
   **unmeasured** — `Image3_Attr`'s layout is unknown, so its value cannot be
   read from the capture without guessing at offsets, which was not done.
   If it is unset, `load_lastk`/`bcm_image_verify` never run for the kernel
   at all, and the three-slot table found by entropy scanning would be
   consulted by no traced code path.
3. The three-slot table's byte-for-byte match to real `MV_KEY_STORE_HEAD`/
   `MV_LASTK_IMAGE` struct definitions is still a fact, verified against the
   capture and cross-checked across five files. What is no longer supported
   is the stronger claim that this is *the* mechanism gating whether this
   unit's kernel boots. It could be that; it could equally be inert
   signing-tool metadata that no on-device code reads back, or read by a
   still-untraced function. `bcm_image_verify()`'s own logic remains a
   mailbox call into the closed BCM co-processor regardless of which story
   is true, so this does not reopen the payload-cipher question below —
   it only weakens confidence in *why* the three-slot table is where it is.

**What this changes**: the entropy-measured layout (three 1,024-byte slots,
`+0x000` to `+0xc00`, body afterward) stands as a measured fact about the
file's contents. Treating it as *the* header that gates kernel boot,
strong enough to guide what a replacement image must reproduce, is
downgraded from "well-supported inference" to "one of at least two
open possibilities," pending either the missing `Image3_Attr`/`linux_hdr_t`
definitions or a direct, non-destructive way to read `cpu0_hdr`'s value on
this unit.

## The two failed attempts

| Candidate | What it wrote to `bootimgs` | Result |
| --------- | --------------------------- | ------ |
| 06.0 | a bare uImage at offset 0, both slots | no boot |
| 6.1 | vendor descriptor kept, uImage spliced at `+0x20000`, `bootimgs_B` left vendor | no boot |

06.0 destroyed the descriptor and left nothing at `+0x20000`. 6.1 corrected
the placement and still failed. 6.1's splice point overwrote the three-slot
table found at `+0x20000` with raw kernel bytes either way, which is a defect
on its own regardless of where the real kernel sub-image turns out to start:
if the vendor loader reads that table for anything on the kernel path, 6.1
destroyed it; if it does not, 6.1 still placed kernel bytes at a NAND offset
that is at best a guess (see the correction above — the real kernel
sub-image address, `cpu0_addr`, is computed at boot time from a
not-yet-measured header, not fixed at `+0x20c00`). Either way, 6.1 did not
demonstrate a correctly-targeted write; it is evidence a guessed offset
failed, not evidence about which offset is correct.

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

Measured directly at `bootimgs+0x20c00` (the offset where the three-slot
table's low-entropy structure ends, not confirmed as the real loader's
`cpu0_addr` — see the correction above), re-run at that corrected offset
rather than assumed carried over from the old `+0x2002c` figure:

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

1. `Image3_Attr` (the header `Image_Load_And_Start()` reads at the start of
   `bootimgs`, containing the real per-component encryption flags including
   `cpu0_hdr->bcpu0_image_encrypt`) and `linux_hdr_t` (the header immediately
   preceding kernel+ramdisk content in memory) are both used but not defined
   in the one source file fetched. Neither's field layout is known, so
   neither can be read from the capture without guessing at offsets.
2. Whether `bcpu0_image_encrypt` is set on this unit is unmeasured. If unset,
   the three-slot table and `bcm_image_verify()` are never consulted for the
   kernel at all, and the real blocker is simply "the loader computes
   `cpu0_addr` dynamically and a replacement image must land there," not a
   cipher question.
3. If it is set, the three-slot table (`0xc00` bytes: customer AES key, RSA
   key record, a per-file third slot) is understood well enough to generate
   one, not just the 44-byte customer-key record that begins it — and the
   transform applied to the body after it is identified, or shown optional.
4. Either branch is demonstrated without a flash cycle, because each guess
   currently costs one non-booting boot, and two guesses (06.0, 6.1) have
   already been spent without resolving which branch applies.

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
