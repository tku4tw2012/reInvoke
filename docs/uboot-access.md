---
title: U-Boot console access over Micro-USB
description: Verified non-destructive procedure for reaching an interactive U-Boot prompt on the Harman Kardon Invoke
ms.date: 2026-09-02
ms.topic: how-to
---

Status: verified on hardware, 2026-09-02, on unit `myInvoke-1`. Reproduced twice
in the same session.

An interactive U-Boot prompt is reachable over the Micro-USB port without
opening the enclosure and without writing to the device. The boot chain runs
entirely from RAM. The unit returns to normal Bluetooth-speaker operation after
a power cycle.

## What was reached

```text
U-Boot 2013.04 (Apr 11 2016 - 10:10:25)
arm-marvell-eabi-gcc (Marvell GCC 201106-257.a1ba7f96) 4.4.5
MV88DE3100|>
```

## The procedure

The host tool must already be polling before the device attaches. It reacts to a
USB hotplug event, so a device that is already connected will not be picked up.

1. Start the capture and boot tool with `08_IMAGE` absent:

   ```bash
   INVOKE_USBMON_INTERFACE=usbmon3 INVOKE_CAPTURE_LIMIT_SECONDS=3600 \
     tools/usb-boot/capture-attempt.sh uboot-session original-absent
   ```

2. Wait for `READY: original usb_boot is polling for the device.`
3. Power the unit off.
4. Hold the Reset pinhole.
5. Restore power while still holding Reset.
6. Press MicOff four times within five seconds.
7. Confirm the amber/yellow indicator.

U-Boot reaches its prompt roughly 25 seconds later.

## Why earlier attempts stopped short

Earlier sessions concluded that the `0xFE` device subclass blocked progress
because the reviewed host tools branch to their iROM bootstrap only on `0xFF`.
That framing was incomplete. The decisive variable is the image-request
sequence the device issues, not the subclass alone.

| Condition | Requests observed | Result |
|---|---|---|
| Ordinary power-on | `08_IMAGE` | No console |
| Yellow service mode | `09_IMAGE`, `sysinit.img`, `bootloader.img`, `drm_erom.img`, `79_IMAGE` | U-Boot prompt |

In the successful runs the device moved through subclass `254`, `255`, then
`254` again. `08_IMAGE` was never requested. Serving the four-stage chain is
what produces the console; the host tool was already capable of it.

## Board and storage facts read from the prompt

All values below were read with non-destructive commands.

| Property | Value |
|---|---|
| SoC family | Marvell MV88DE3100 / BG2CD, chip revision a0 |
| DRAM | 512 MiB, bank base `0x00000000`, size `0x20000000` |
| SPI NOR | M25P128, 16 MiB, 64 sectors of 256 KiB, mapped at `0xF0000000` |
| NAND | Chip ID `98DA90157616`, 256 MiB |
| NAND geometry | 128 KiB blocks and 2 KiB pages; U-Boot reports 32 B OOB while Linux reports 64 B |
| NAND randomizer | Not enabled |
| eMMC / SD | No card responds on `MV_SDIO` |
| Console | Serial only, no Ethernet detected |

The SPI NOR reads as `0x00` at every offset sampled, and U-Boot reports
`environment in SPI flash is invalid`. The environment shown by `printenv` is
therefore Marvell's compiled-in default, not a value stored by Harman. It still
carries Marvell development defaults such as `rootpath=/home/galois/galois-rootfs`
and `serverip=10.38.54.88`.

## NAND condition

A full `nandbad 0 2048` scan reported two bad blocks, at `0x0C000000` and
`0x0C020000`, plus one uncorrectable ECC page at `0x0FE40800`. Two bad blocks in
2048 is within normal manufacturing tolerance for this class of NAND. The chip
responds correctly to identification and reads.

This does not support the earlier hypothesis that NAND degradation explains the
device's USB behavior.

### Observed content map

Sampled with `nandrd` at 32-byte granularity.

| Offset | Content |
|---|---|
| `0x00000000` | Blank (`0xFF`) |
| `0x00020000` | Structured 12-byte header, then high-entropy data |
| `0x00100000` | Byte-identical header to `0x00020000` |
| `0x00400000` – `0x01000000` | Blank |
| `0x02000000` | High-entropy data |
| `0x04000000` | High-entropy data |
| `0x08000000` | Blank |

The header at both `0x00020000` and `0x00100000` begins:

```text
01 00 00 00 37 c2 00 13 01 00 00 00
```

The duplicate copy is consistent with a redundant boot-image slot. The payload
that follows shows no recognizable structure at this sampling density, which is
consistent with signed or encrypted images. That is an observation about
entropy, not a demonstration that a specific cipher is in use.

## Safety boundary observed

Only read-only commands were issued: `version`, `help`, `printenv`, `bdinfo`,
`coninfo`, `speed`, `mmcinfo`, `flinfo`, `md.b`, `nandinit`, `nandbad`, and
`nandrd`.

No command from the following set was sent, and none should be sent against a
working unit:

```text
nanderase  nandwr    nandmarkbad  nandverify
erase      protect   saveenv      editenv
b2nand     u2nand    l2nand       tftp2nand   usb2nand
emmcerase  emmcwrite emmcbootpart emmcrsten   burnsd
fatwrite   mkext4    img2sd       mw  mm  nm
```

`99_IMAGE` and `83_IMAGE` must never be staged. `79_IMAGE` must stay
comment-only; the launcher refuses to start otherwise.

The `imls` command was also attempted as a read-only image-listing probe, but
this customized build raised a data abort and reset the CPU. Do not repeat it
without first understanding the command's assumptions and memory accesses.
"Read-only" describes intended storage effects, not guaranteed stability.

The documented OTA2 `mtdparts` layout places `rootfs` at `0x10700000`, but
`nandrd 10700000 400` returned `Invalid address...` on this unit. The layout
therefore cannot be applied to this U-Boot NAND address space without further
reconciliation; no larger or speculative address probes should follow.

## RAM-native Linux handoff

The prompt can load the reviewed recovery kernel and a sanitized initramfs into
DRAM without writing NAND:

```text
usbload 0x81 0x0c400000
usbload 0x82 0x08000000
set bootargs console=ttyS0,115200 loglevel=8 debug root=/dev/ram rdinit=/init init=/init initrd=0x08000000,<generated-size>
bootm 0x0c400000
```

This was verified on `myInvoke-1`. The replacement PID 1 configured root ADB,
left NAND unmounted, loaded native SD8887 Wi-Fi, and ran selected hardware
adapters under its own lifecycle. A full 268,435,456-byte logical NAND data
image was then read through a fresh read-only MTD node. Its SHA-256 is
`edf38ef2af48d249c9925ebb6a94c716cfdb2c1ce575fb704283918cdd0e53be`.

The active rootfs extracted from that image identifies as
`Barracuda_libre-12.2050.3`, commit
`6c36464edbac87c01fcba0f81c86293f554acf50`, built 2021-02-04. See
[native-ram-platform.md](native-ram-platform.md) for the component audit and
replacement architecture.

## Reconnecting to a live session

A session cannot be resumed by restarting the host tool alone. `usb_boot` waits
for a hotplug event, and a `USBDEVFS_RESET` ioctl is not sufficient because the
device keeps its address. Reattaching mid-session lands in the tool's
request-serving state machine, which reports `img transfer status 1?` and shuts
its poll thread down.

Repeat the yellow sequence from step 3 instead. It is reproducible.

## What this does not establish

* The Marvell boot stages do not expose ADB. The custom RAM initramfs does.
* The active rootfs is readable, but the installed normal-kernel carve remains
  a high-entropy signed or encrypted container.
* Nothing here demonstrates that a modified persistent image will pass the
  secure boot chain.
* Absence of device-side writes is argued from the command set used, not proven
  by before-and-after storage imaging.
