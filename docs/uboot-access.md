---
title: Historical U-Boot console access over Micro-USB
description: Previously verified procedure and evidence limits for reaching an interactive U-Boot prompt
ms.date: 2026-09-12
ms.topic: how-to
---

Status: verified on hardware and retained as the recovery path for the native
NAND runtime. This page does not authorize a powered procedure, reboot, or
write. The demonstrated complete vendor installation erased all good NAND
blocks; any repeat requires a separately reviewed owner-approved scope. See
[NAND write evidence and decision gates](nand-write-decision.md) and the
[current product and architecture contract](current-product-contract.md).

An interactive U-Boot prompt was reached over the Micro-USB port without opening
the enclosure and without issuing an intentional NAND write command. The served
boot chain ran from RAM. Autonomous behavior before the prompt was not proved
write-free. At the time, a power cycle returned the unit to normal
Bluetooth-speaker operation.

## What was reached

```text
U-Boot 2013.04 (Apr 11 2016 - 10:10:25)
arm-marvell-eabi-gcc (Marvell GCC 201106-257.a1ba7f96) 4.4.5
MV88DE3100|>
```

## The previously verified procedure

> [!CAUTION]
> Do not interrupt the microphone session's live device. These historical
> access steps are not NAND-write approval.

The original proprietary tool must already be polling before attachment. The
separately pinned open-source helper can also attach to the observed existing
`FF` endpoint; that later path is described below.

1. Start the capture and boot tool with `08_IMAGE` absent:

   ```bash
   INVOKE_USBMON_INTERFACE=<usbmonN> INVOKE_CAPTURE_LIMIT_SECONDS=3600 \
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
| NAND geometry | 128 KiB blocks and 2 KiB pages; U-Boot exposes 32 B OOB, Linux declares 64 B, and the Toshiba ID prefix maps to 128 B physical OOB upstream |
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

A later controlled read-only investigation changed the safety interpretation.
The five historical pages still increment the uncorrectable-ECC counter, and the
current Linux driver returns only 32 meaningful OOB bytes despite declaring 64.
Upstream Linux maps the Toshiba ID prefix to a 128-byte physical-OOB part.
Separately, one early-region erase block differs between the 2026-09-02 and
2026-09-07 logical images for an unknown reason. See
[NAND write evidence and decision gates](nand-write-decision.md).

### Observed content map

Sampled with `nandrd` at 32-byte granularity.

| Offset | Content |
|---|---|
| `0x00000000` | Blank (`0xFF`) |
| `0x00020000` | Structured 12-byte header, then high-entropy data |
| `0x00100000` | Byte-identical header to `0x00020000` |
| Selected samples from `0x00400000` – `0x01000000` | Blank at the sampled bytes only |
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

This was verified on the closed test unit. The replacement PID 1 configured root ADB,
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

The original Marvell `usb_boot` session cannot generally be resumed by
restarting that tool alone. It waits
for a hotplug event, and a `USBDEVFS_RESET` ioctl is not sufficient because the
device keeps its address. Reattaching mid-session lands in the tool's
request-serving state machine, which reports `img transfer status 1?` and shuts
its poll thread down.

Repeat the yellow sequence from step 3 instead. It is reproducible.

### Later observed attachment to an existing FF device

On 2026-09-09, the separately retained open-source helper at revision
`63444e82cc5274abe31ec49ad55ee552b50b64b3` attached to an already-present `FF`
endpoint without a physical reset or USB replug. Unlike the original helper,
it enumerates existing devices during startup. It supplied the RAM bootstrap,
observed `FE`, served the `09_IMAGE` recovery chain and reached U-Boot.
The known corrected kernel and RC12 RAM runtime were then loaded successfully.

This is verified for the observed persistent `FF` state, not arbitrary
mid-session attachment or proof that the original yellow sequence was never
needed. `FE` by itself is still a download stage, not Linux or ADB.

The loader's initial ADB timeout was host-side: its selected server on port
5038 had no transport, while the existing server on port 5037 had the working
RAM device. Inspect the actual USB product and both existing server contexts
before requesting another reset. No new NAND erase/program command was used
for this recovery.

Evidence: sibling archive
`evidence/nand-restored-ram-inspection-20260909/`.

### Recovery after the 12.2134.0 early-ADB trial

On September 11 at 16:31 UTC, the pinned open-source helper again attached
to an existing FF endpoint without Reset or a USB replug. This followed the
owner's normal power cycle of the independently verified 12.2134.0 early-ADB
NAND diagnostic. A fresh `version` command returned the helper-loaded U-Boot
banner and prompt. The known RAM kernel and RC12 handoff diagnostic returned
ADB at 16:34:40 UTC.

Read-only `help` and `printenv bootcmd bootargs` showed that this recovery
U-Boot's `nandrd` displays NAND bytes, while its default `bootcmd` uses
development TFTP/NFS placeholders. Neither is an established method of launching
the installed NAND kernel/container. No generic `boot`, broad memory dump or
NAND erase/program command was issued.

The later RAM inspection verified the persisted NAND filesystem and selected
boot-region bytes, without mounting NAND or executing its startup scripts.
It does not establish which stage failed during helper-free startup.
Evidence: sibling archive
`evidence/nand2134-helper-inspection-20260911T1632Z/`.

## What this does not establish

* The Marvell boot stages do not expose ADB. The custom RAM initramfs does.
* The active rootfs is readable, but the installed normal-kernel carve remains
  a high-entropy signed or encrypted container.
* Nothing here demonstrates that a modified persistent image will pass the
  secure boot chain.
* Absence of device-side writes is argued from the command set used, not proven
  by before-and-after storage imaging.
