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
+0x20000  the image the loader consumes, behind a 44-byte Marvell header
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

## The two failed attempts

| Candidate | What it wrote to `bootimgs` | Result |
| --------- | --------------------------- | ------ |
| 06.0 | a bare uImage at offset 0, both slots | no boot |
| 6.1 | vendor descriptor kept, uImage spliced at `+0x20000`, `bootimgs_B` left vendor | no boot |

06.0 destroyed the descriptor and left nothing at `+0x20000`. 6.1 corrected the
placement and still failed, because the image at `+0x20000` is not a bare
kernel either: it opens with the same 44-byte Marvell header carried by
`bcm_erom.bin.usb`, `sysinit.img`, `bootloader.img` and `pre-bootloader`.

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

## What the payload is, and is not

Measured on the image at `bootimgs+0x20000` and on the working staging files:

* not zlib, gzip, bzip2 or lzma at any tested offset
* no ECB structure: zero repeated 16-byte blocks across 4 MiB
* entropy 7.88 to 8.00, which does not separate encryption from compression,
  since `bsl` is plain SquashFS and also measures 8.00
* no uImage magic, no zImage magic and no ARM NOP sled anywhere in NAND

`81_IMAGE`, the recovery kernel, is a plain uImage and accepts a custom build.
That is why a custom kernel boots from RAM and not from NAND: the recovery path
takes a uImage, the NAND path does not.

## What would have to be true first

1. The 44-byte Marvell header is understood well enough to generate one.
2. The payload transform is identified, or shown to be optional.
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
