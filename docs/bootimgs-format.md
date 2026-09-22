---
title: The bootimgs record and why a custom kernel does not boot from NAND
description: Measured layout of the NAND boot records, two failed kernel-replacement attempts, and what would have to be true to succeed
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
sizeof(IMG3_NAME))`).

**`IMG3_NAME` upgraded from corroboration to a confirmed literal.** A later
fetch of `bootloader/berlin_tools/bootloader/include/version_table.h`, from
the same source tree as `bootloader.c`
(`nest-open-source.googlesource.com`'s lineage, mirrored on GitHub as
`chromecast-mirrored-source.sdk`; checked byte-identical across two
independent mirrors before trusting it), contains the literal `#define
IMG3_NAME "bootimgs"`. A second, incompatible `version_table.h` also exists
in the same tree (a `Common/include` variant with different field names);
the `bootloader/include` copy used here was selected by matching its field
names against real usage already traced in `bootloader.c`
(`dump_version_entry`'s `part1_start_blkind` etc.) before being trusted. What
this document previously called "strong internal corroboration, not a
byte-grepped proof" is now a confirmed literal constant.

**`Image3_Attr` and `linux_hdr_t` are now known**, from the same tree
(`bootloader/berlin_tools/Common/include/image3_header.h`, also
byte-identical across two independent mirrors). `Image3_Attr` is
`sm_image_attr sm_param` (40 bytes, `+0x00`) + `cpu0_image_attr cpu0_param`
(40 bytes, `+0x28`) + `cpu1_image_attr cpu1_param` (48 bytes, `+0x50`) +
`recovery_ou_attr recou_param` (4 bytes, `+0x80`) + `Mem_Layout mem_layout`
(16 bytes, `+0x84`) + `unsigned char linux_bootargs[4096]` (`+0x94`) — 4,244
bytes total. `sm_image_attr` and `cpu0_image_attr` share the same ten-field,
40-byte shape (`..._active`, `..._load_addr`, `..._ori_size`,
`..._final_size`, `b..._encrypt`, `..._encrypt_image_size`,
`..._encrypt_image_body_size`, `b..._bss_init`, `..._bss_start_addr`,
`..._bss_length`), so `bcpu0_image_encrypt` sits at `cpu0_param+0x10`, i.e.
`Image3_Attr+0x38`. `linux_hdr_t` is `kernel_size` / `ramdisk_size` /
`ramdisk_addr` / `reserved[20]` — 32 bytes, matching its own "32 bytes
aligned" comment in the header.

Read directly from the capture at `img3_start` (the version-table-derived
address; see below): **`bcpu0_image_encrypt = 1`.** This resolves what this
document previously listed as unmeasured. The flag is set, on the
vendor-stock image, for the specific copy this unit actually boots (see
below) — so `load_lastk(g.lastk)` and
`bcm_image_verify(BCM_IMG_KERNEL_TYPE, ...)` (`bootloader.c:2025,2031`) are
confirmed to run on this unit's real kernel-boot path, not merely capable of
running. Once past that point, the resulting buffer is read as `linux_hdr_t`
(`bootloader.c:2051-2060`, outside and after the encrypt-flag block, so this
step always runs) — a **third** header type, distinct from both
`MV_KEY_STORE_HEAD` and `Image3_Attr`.

## Where `img3_start` comes from, and which copy this unit boots

The version table (found redundantly in the last 4 KiB of NAND blocks 1
through 8 — `iVT_OFFSET = nand_data.szofblk - 4096` under `#if BG2CDP`,
`bootloader.c:2201-2205`, read by the loop at `2208-2227` — all eight read
back byte-identical on this unit: magic `0xd2ada3f1`, 13 entries, and the
CRC32 self-check the loader itself performs
(`bootloader.c:2222`, `crc32(0, buf, vt_size+4) == 0xffffffff`, `vt_size`
per `2220`) reproduced independently with the standard `zlib.crc32` and
confirmed exactly `0xffffffff`) contains an entry named `bootimgs` (index 9
of 13, every entry's name matching the already-published mtdparts partition
list exactly): `part1 = {major 20170622, minor 721, start block 249, 80
blocks}`, `part2 = {major 0, minor 0, start block 249, 80 blocks}`.

`Image_Load_And_Start()` picks `part1` when its version is greater
(`bootloader.c:1835-1843`), falling back to `part2` otherwise
(`1844-1850`) — this document previously cited only the `part2` branch
unconditionally, which was imprecise. Applied to the real numbers above
(`part1` major `20170622` > `part2` major `0`), **`part1` wins**:
`img3_start = 249 * 0x20000 = 0x01f20000`, `img3_end = 329 * 0x20000 =
0x02920000`. `img3_end` lands exactly on the already-documented start of the
second SquashFS (`Barracuda_libre-12.2050.3` at `0x02920000`) — an
independent cross-check against a fact this document established
separately, not assumed to fit.

`bootimgs_B` is a **separate, distinctly-named** version-table entry (index
7, start block 129 = `0x01020000`), not a `part1`/`part2` alternate of
`bootimgs` within this scheme — the code never compares the two against
each other. Its own `Image3_Attr` (decoded the same way, at `0x01020000`)
differs from `bootimgs`'s in load address (`0x01108000` vs `0x02008000`)
and kernel size (4,910,330 vs 4,939,774 bytes), consistent with an older or
alternate build, but also reads `bcpu0_image_encrypt = 1`. No code path
traced in `bootloader.c` selects `bootimgs_B` in place of `bootimgs`; it is
recorded here only because an earlier draft of this section had not yet
ruled it out.

`bootimgs`'s decoded `uicpu0_image_ori_size` (4,939,774 bytes,
`Image3_Attr+0x28+0x08`) is an exact, independent match for the
already-established `CPU_IMG_OFFS_IMGSIZ` figure read from the three-slot
table's own third slot (`+0x828`, "A second, independent field match"
above, `0x004b5ffe` = 4,939,774 bytes) — two different structures, decoded
by two different methods in two different sessions, agreeing on the same
number. This is additional cross-validation that both decodes are reading
real fields, not artifacts of a wrong offset.

`sm_param` (`Image3_Attr+0x00`) reads **all zero** at `img3_start` — no SM
sub-image is active on this unit. `nand_read_generic()`
(`bootloader.c:1362-1405`) always consumes whole blocks: it rounds its
`data_size` argument up to `nand_data.szofblk` (128 KiB) before deciding how
far to advance, so reading the 4,244-byte `Image3_Attr` still consumes one
full block and returns `img3_start + 0x20000` as `sm_addr`
(`bootloader.c:1859`). `get_next_img_addr()` (`1614-1633`) advances by zero
further blocks when the size passed in is zero, so with `sm_param` inactive,
`cpu0_addr = get_next_img_addr(sm_addr, img3_end, 0) = sm_addr`
(`bootloader.c:1881`) — unchanged.

**`cpu0_addr = 0x01f20000 + 0x20000 = 0x01f40000` — the exact address of the
three-slot table measured above.** This was checked by tracing the address
arithmetic in the loader's own code and confirming every intermediate value
(`sm_param`'s zero fields, the version-table entry's real numbers) directly
against the capture, not by assuming the two figures would match. The
loader then reads `cpu0_hdr->uicpu0_image_final_size` bytes starting at
`cpu0_addr` into RAM as the start of the CPU0 (kernel) image
(`bootloader.c:2017`) — so the three-slot table is not a separate structure
sitting near the kernel image by chance; **it is the first `0xc00` bytes of
the on-NAND kernel sub-image itself**, for the copy this unit actually
boots. `+0x20c00`, where this document's entropy measurements were taken
(see "What the payload is, and is not" below), is therefore confirmed as
`cpu0_addr + sizeof(three-slot table)` — the loader's own boundary, not an
assumption carried over from an earlier, wrong offset.

## A second key structure, and a new open question: `g.lastk`

`MV_LASTK_STORE`, `MV_LASTK_IMAGE` and `MV_KEY_STORE_HEAD` (used for the
three-slot table's field names above) are defined directly in
`bootloader.c` itself (`329-396`) — a search for them in the same GitHub
mirrors that supplied `Image3_Attr` and `version_table.h` found nothing,
because they were never missing; they are in the file already fetched, and
should have been checked there first. `MV_LASTK_STORE` is a *different*,
1,024-byte structure: `custk` (64 bytes, `+0x000`), `custk_kernel` (64
bytes, `+0x040`), `extrsak` (896 bytes, `+0x080`), each a `{ version; type;
}` pair followed by raw bytes. This document already established (before
this update) that `load_lastk()` populates `g.lastk` — this exact
1,024-byte structure — from `g.partition_info_buff[3072]`, i.e. the last
1,024 bytes of the same version-table block (`bootloader.c:2227`), **not**
from `bootimgs`.

Read directly from the capture at that exact location: `custk.type =
0xc237` (`AESK`, valid) but **`custk_kernel.type` masks to `0xf50a` against
`MV_KEY_STORE_TYPE_MASK` — this does not match `MV_KEY_STORE_TYPE_AESK`
(`0xc237`)**. `load_lastk()`'s only branch condition is
`(lastk->custk_kernel.type & MV_KEY_STORE_TYPE_MASK) == MV_KEY_STORE_TYPE_AESK`
(`bootloader.c:403`); when false, it prints `"no need to load kernel keys"`
and returns success without registering any key with the BCM co-processor
(`bootloader.c:426-429`). **On this unit, that branch is taken.** The
`custk_kernel` and `extrsak` fields' `version` values (3,137,833,675 and
1,494,944,394) are not small, plausible version numbers like `custk`'s `1`;
they read as high-entropy data, consistent with those two slots simply not
being populated for this product, not with a corrupted or unusual unit.

This is a new fact, not previously in this document, and it leaves a real
question open rather than closing one: `bcpu0_image_encrypt` is confirmed
set, so `bcm_image_verify(BCM_IMG_KERNEL_TYPE, cpu0_buff, cpu0_buff)`
(`bootloader.c:2031`; both arguments after the type code are the same
pointer — `mem_buff` is a `#define` alias for `cpu0_buff`, `bootloader.c:2007`,
not a second buffer) does run — but it does not run with a kernel-specific
key freshly registered by `load_lastk()` on this boot. Whatever key
material the BCM co-processor actually uses for this call — a
previously-provisioned key already resident in the co-processor, a
device-wide key unrelated to `custk_kernel`, or something else — is
**unknown**. `bcm_image_verify()`'s own logic remains a mailbox call into a
closed co-processor either way (already established); this only changes
what feeds it, not whether it can be inspected.

One more caveat, found while reading the surrounding code, not measured on
this unit: `bootloader.c:101` shows a build-time macro,
`CONFIG_FORCE_ENCRYPTION`, commented out (`//#define CONFIG_FORCE_ENCRYPTION`)
in this exact source revision. If defined, it unconditionally sets
`bsm_image_encrypt`, `bcpu0_image_encrypt` and `bcpu1_image_encrypt` to `1`
in RAM regardless of what a header on NAND contains
(`bootloader.c:1874-1878`). This source file's own default is off, but
whether this device's actual compiled bootloader binary was built with an
equivalent flag supplied externally (e.g. a Makefile `-D`) cannot be
determined from source alone. If it is active on this device, a replacement
image could not bypass the encrypt-gated path by clearing the flag in a
hand-built header even in principle, because the running code would
override the clear regardless of what is on NAND.

## The two failed attempts

| Candidate | What it wrote to `bootimgs` | Result |
| --------- | --------------------------- | ------ |
| 06.0 | a bare uImage at offset 0, both slots | no boot |
| 6.1 | vendor descriptor kept, uImage spliced at `+0x20000`, `bootimgs_B` left vendor | no boot |

06.0 destroyed the descriptor and left nothing at `+0x20000`. 6.1 corrected
the placement and still failed.

**Revisited, now that `cpu0_addr` is measured, not guessed** (see "Where
`img3_start` comes from" above): `+0x20000` is exactly `cpu0_addr` on this
unit. 6.1 placed its uImage at the *correct* address — the offset was not
the problem. What 6.1 wrote there was a bare, unencrypted uImage, into a
slot the loader reads with `bcpu0_image_encrypt` confirmed set, expecting to
hand the bytes at that address to `bcm_image_verify(BCM_IMG_KERNEL_TYPE,
...)` (a mailbox call into the closed BCM co-processor) before treating the
result as `linux_hdr_t` + kernel + ramdisk. A bare uImage is not that
input. This is a more definite explanation than this document could offer
before: 6.1's failure is now attributable to content format at a confirmed
address, not to an unresolved address guess. It does not, on its own, rule
out every other explanation (a malformed `linux_hdr_t` substitute, a size
field mismatch, or something in `cpu1`/`recou`/`en_addr` reads that were
never reached could each independently prevent boot) but the offset itself
is no longer a live suspect.

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

**This is a distinct mechanism from the encryption gate measured above,
and the two should not be conflated.** The fuse readout and the two
header-cut comparisons in this section are about the SoC boot ROM's own
first-stage signature enforcement (`pre-bootloader`, `bootloader.img`,
etc.) — confirmed off. `bcpu0_image_encrypt`, `load_lastk()` and
`bcm_image_verify()` are a separate, later, kernel-specific mechanism
inside this bootloader binary itself, confirmed **on** for the kernel path
(see "Where `img3_start` comes from" above). Both can be true at once: the
SoC does not require a signed `bootloader.img` to boot, and the bootloader
it runs still refuses to treat an unencrypted kernel image as valid. The
title of this document ("not signature enforcement") refers to the first
fact; it does not claim the second mechanism is absent.

## What the payload is, and is not

Measured directly at `bootimgs+0x20c00` — confirmed above as `cpu0_addr +
0xc00`, i.e. the loader's own boundary between the three-slot table and
whatever follows it in the same on-NAND `cpu0` image, not an assumed offset:

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

Two of the four items this section previously listed are now resolved by
direct measurement. They are kept here, marked resolved, so the record of
what changed is not lost; the items that remain open are what actually
gates a future attempt now.

1. ~~`Image3_Attr` and `linux_hdr_t` undefined~~ — **resolved.** Both fetched
   from `image3_header.h` and cross-verified across two independent
   mirrors; full field layout known (see "Where `img3_start` comes from"
   above).
2. ~~Whether `bcpu0_image_encrypt` is set is unmeasured~~ — **resolved.**
   Measured `1` (true) on this unit's actual boot copy (`bootimgs`) and,
   separately, on `bootimgs_B`. `load_lastk()`/`bcm_image_verify()` are
   confirmed to run on the real kernel-boot path, not merely capable of
   running.
3. **Still open, and now the central question**: the transform
   `bcm_image_verify()` applies to the buffer at `cpu0_addr` is unknown. It
   is a mailbox call into the closed BCM co-processor; nothing in
   `bootloader.c` describes what happens on the other side of that mailbox.
   A newly-found wrinkle sharpens this rather than resolving it: the key
   material `load_lastk()` would normally register for this specific call
   (`g.lastk`'s `custk_kernel` field) does **not** match the expected type
   tag on this unit, so that registration step is skipped — meaning
   whatever key the co-processor actually uses for
   `bcm_image_verify(BCM_IMG_KERNEL_TYPE, ...)` is not visible in anything
   read from NAND this session. Reproducing this transform without knowing
   the key, the algorithm, or even whether it is confidentiality-encryption
   or integrity-only, is not something this document can currently spec out.
4. **Still open**: whether this device's actual compiled bootloader binary
   was built with `CONFIG_FORCE_ENCRYPTION` (or an equivalent) supplied
   externally. The fetched source has it commented out by default; a build
   flag could re-enable it invisibly to anyone reading only the source. If
   active, it would foreclose "ship a header with the flag cleared" as a
   bypass even in principle.
5. Either open branch above is demonstrated without a flash cycle, because
   each guess currently costs one non-booting boot, and two guesses (06.0,
   6.1) have already been spent — 6.1 is now known to have targeted the
   correct address (`cpu0_addr = bootimgs+0x20000`) with the wrong content,
   which narrows what a third guess would be testing, but does not license
   one without new evidence about the co-processor's transform.

Until then, keep `bootimgs` at the vendor image.

## External research: does anyone else know the co-processor's transform

Four AI research agents (GPT-6 Astra, Claude Opus 5, Grok 4.6, Claude Sonnet 5)
searched independently and in parallel for prior art on this exact question.
Opus 5 returned zero content across five distinct attempts (fresh research,
retry, reworked prompt, critique of the other reports, reframed peer review) —
a content-filter block on this topic, on this model, not a research result.
The other three succeeded and converged with each other and with the findings
above. Their most consequential citations were then independently re-verified
directly against live sources (not taken on the reporting model's word),
per this project's standing rule to ground every claim.

* **`coggy9/HKHacking`** (real, public, dormant since 2022) independently
  corroborates this project's own yellow-mode entry sequence and confirms
  "Podium" as Invoke's internal codename. Its README links two archive.org
  items — `HK-Invoke-source-disclosure` and `invoke-kernel` — confirmed real
  via `archive.org/metadata`, downloaded, and MD5-verified against the
  published metadata. The disclosed kernel (Linux 3.8.13, exact
  `berlin2cdp-a0-acast` board match, firmware vintage `Barracuda_libre-11.1842.0`,
  older than this unit's own `bootimgs`) was grepped end-to-end for
  keystore/AESK/`bcm_image_verify` terms: **zero real hits**. This confirms,
  rather than merely assumes, that the verification logic is entirely
  bootloader-side and never touches Linux driver code, in a second real
  source tree independent of the one already traced in this document.
* **`senarytech/ubuntu`** is a later Synaptics VS680-era source tree (not
  proven to be what built this unit's BG2CDP image; corroborating context,
  not device-specific proof). Its `bcm_verify.c` independently confirms the
  same mailbox-call shape already traced in `bootloader.c` (opcode, type/src/dst
  arguments, register-level polling) but reveals nothing about the
  co-processor's actual cryptographic transform or key — the same conclusion
  reached from a second, independent, more recent source tree. That file
  carries an explicit Synaptics NDA/confidentiality header; its contents are
  described here at the mechanism level only and were not saved to this
  repository or the evidence archive. Its `encryption.sh` build script shows
  that when `ROM_KEY_DISABLE=1`, encryption runs unconditionally regardless of
  the "disable encryption" config flag — by analogy, not proof, this is
  circumstantial support that this chip family's own build tooling treats
  kernel encryption as close to mandatory in real deployments.
* **Google Home Mini** (`courk.cc`, two-part public writeup) uses a sibling
  SoC in the same family (Marvell/Synaptics Armada 1500 Mini Plus) and a
  similar NAND part. This is by-analogy evidence from a related product, not
  a finding about this unit. Four points are worth carrying over, the last
  of them a direct observation rather than an analogy:
  1. The author achieved full physical NAND read/write via a custom
     BGA-desoldering rework and FPGA-based interposer board ("NandBug",
     open-sourced) — hardware capability far beyond hobbyist reach, consistent
     with this project's own stated tool/skill limits.
  2. With that privileged access in hand, the author states plainly: "an
     extended secure boot implementation makes executing arbitrary code using
     naive methods impossible." Kernel and bootloader partitions are
     cryptographically verified there too, and no way was found to skip that
     verification — the same wall this document has been tracing from the
     other direction, now independently reached by a different researcher
     starting from full hardware access instead of source study.
  3. The actual successful exploit (part 2) was **not** a verification bypass.
     It was a memory-corruption bug found by fuzzing the kernel's YAFFS2
     filesystem driver, reachable only through `cache`/`factory_store` — NAND
     regions explicitly outside that device's chain of trust — and it grants
     code execution under the already-booted stock kernel, not a booted custom
     kernel. This is a different strategy from anything this document
     evaluates (it never replaces the kernel), unproven to have any analog on
     this unit (this project has not established that any mounted filesystem
     here uses YAFFS2, or that its driver carries a comparable bug), and would
     be a multi-week fuzzing/vulnerability-research effort on a kernel this
     project does not have exact matching source for, not a documentation task.
  4. Google's own `kernel` partition ships as a plain,
     unencrypted Android bootimg, while `rootfs` is dm-verity-protected
     instead. This shows kernel encryption on this chip family is a
     per-product build choice, not a hardware-forced universal — sharpening,
     not contradicting, this unit's own measured `bcpu0_image_encrypt=1`.

None of this external research surfaced the co-processor's transform or key.
It closes off, rather than opens, the remaining avenues: the mechanism is now
confirmed absent from two independent Linux/vendor source trees, one further,
more sensitive NDA-marked tree, and a sibling product's own independent
hardware-level research effort that had far greater physical access than is
available here. A third flash guess (`6.2`) would still be exactly what it
would have been before this research: an unlicensed guess against a
transform nobody has found described anywhere. Its empirical cost is
separately bounded — `6.0` and `6.1` both left the bad-block list and
yellow-mode recovery unaffected — so the choice is a real trade-off between a
low, evidence-based probability of success and a low, evidence-based cost of
trying, not a hidden risk of further research.

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
