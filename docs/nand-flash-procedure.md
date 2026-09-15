---
title: NAND flash procedure
description: The verified steps to flash a reInvoke bundle over USB recovery
ms.date: 2026-09-14
ms.topic: how-to
---

This is the procedure that successfully flashed Candidates 05.2 and 05.3 on
2026-09-14, written from the observed runs and cross-checked against the
preserved logs of Candidates 01, 02 and 03. It supersedes the 04/05-era notes,
which recorded a drifted procedure and several self-inflicted failures as if
they were device behaviour.

> [!IMPORTANT]
> Read "Why attempts fail" before starting. Nearly all lost time came from
> misreading a failed service-mode entry as a device fault.

## The sequence, from four successful flashes

Candidates 01, 02, 03 and 05.2 are identical in structure:

```text
USB subclass FF                     <- iROM, the only usable entry point
-> 24,576-byte iROM bootstrap
-> re-enumerate as subclass FE
-> 09_IMAGE
-> sysinit.img
-> bootloader.img
-> drm_erom.img
-> 79_IMAGE
-> U-Boot prompt
-> l2nand 83 requests 83_IMAGE, then 07_IMAGE
```

Each request appears exactly once. Anything else is a failed attempt.

## Prerequisites

Staging must byte-match the known-good set, whose authoritative copy is
`evidence/nand-restored-ram-inspection-20260909/firmware`:

| File | Note |
| ---- | ---- |
| `06_IMAGE` | Host USB path record |
| `07_IMAGE` | **4 bytes, little-endian image size. Omitting this is fatal.** |
| `08_IMAGE.stock` | Source of truth; copy it to `08_IMAGE`, never delete either |
| `09_IMAGE` | Opaque record |
| `79_IMAGE` | Comment-only script; must stay comment-only |
| `81_IMAGE`, `82_IMAGE` | Recovery payloads |
| `bcm_erom.bin.usb` | The iROM bootstrap sent at FF |
| `bootloader.img`, `drm_erom.img`, `sysinit.img` | Boot chain |

Verify every time:

```bash
for f in 06_IMAGE 07_IMAGE 08_IMAGE.stock 09_IMAGE 79_IMAGE 81_IMAGE 82_IMAGE \
         bcm_erom.bin.usb bootloader.img drm_erom.img sysinit.img; do
  cmp -s "<known-good>/$f" "<staging>/$f" && echo "same  $f" || echo "DIFF  $f"
done
```

## Host setup

Arm the complete path before entering yellow mode:

```bash
REINVOKE_ARCHIVE=... \
  tools/usb-boot/arm-flash.sh <staging> <83_IMAGE-sha256> <attempt-dir>
```

Wait for `READY` before touching the speaker. The command starts exactly one
helper and one console client, then leaves both waiting. Nothing else should
watch, claim or reset the USB device. The helper matches the Invoke by vendor
and product identifiers, not a host port path.

These controls exist because of measured failures:

* The pinned helper exits after its internal device wait. The wrapper restarts
  it while keeping exactly one instance active.
* Two helpers both log `Claimed interface 0` and the device drops immediately
  after every transfer.
* Descriptor-driven processes that killed helpers destroyed live sessions
  after Phase 1 returned the device to subclass `FE`.
* Hard-coding host port `3-1.2` reported that the device was absent while it
  was enumerating on `2-1.2`. Port paths are not part of the flash decision.

Candidate 05.7 used this sequence without an operator-timed command:

```text
helper and console client waiting
operator enters yellow mode
09 -> 02 -> 03 -> 05 -> 79 -> 83 -> 07
l2nand 83
u2nand succeed
```

## Service-mode entry

Power off, hold Reset, restore power, press MicOff four times within five
seconds, release when the top shows yellow.

Holding Reset past yellow makes no difference; measured on this unit.

Entry is genuinely unreliable and often needs several attempts. With the
catcher running there is no window to miss, so simply repeat. Many failed
attempts before one takes is normal and not a fault.

## Why attempts fail

| Observed | Meaning |
| -------- | ------- |
| Only `Image request 0x08`, repeating | Entered at `FE`; iROM was missed. Retry the entry. |
| `Cannot open image file ...` | Staging incomplete |
| Duplicated log lines | More than one helper running |
| `No device found within 120 seconds` | The bare helper timed out; the catcher does not |

Serving `08_IMAGE` does not rescue a failed entry. Feed the device what it
asks for, but a `0x08`-only loop means the entry itself must be repeated.

## Console

The helper proxies the console to TCP 8141:

```bash
mkfifo /tmp/uboot_cmd
python3 tools/usb-boot/uboot-console.py &
printf 'version\n' > /tmp/uboot_cmd
```

Require a **fresh** response, not an old prompt already in the log:

```text
U-Boot 2013.04 (Apr 11 2016 - 10:10:25)
```

The log is binary; read it with `strings /tmp/uboot.log`.

## Flash

Stage the image and its size record, verifying the digest first. The bundle
ships its own size record, so copy that rather than recomputing it:

```bash
sha256sum "<bundle>/83_IMAGE.<candidate>"     # must equal the reviewed digest
cp "<bundle>/83_IMAGE.<candidate>" "$STAGE/83_IMAGE"
cp "<bundle>/07_IMAGE.for-83"      "$STAGE/07_IMAGE"
printf 'l2nand 83\n' > /tmp/uboot_cmd
```

The vendor operation writes, reads back and CRC-checks nine records:
`block0`, `pre-bootloader`, `post-bootloader`, `tz_en`, `bootimgs`,
`bootimgs_B`, `rootfs`, `app`, `bsl`. It takes roughly 40 seconds and ends with:

```text
Congratulations! u2nand succeed!
```

Confirm a fresh post-flash `version`, then **stop the helper and remove
`$STAGE/83_IMAGE`** so nothing can re-serve it.

Block counts are a useful cross-check and are identical across candidates:

```text
block0 1, pre-bootloader 8, post-bootloader 2, tz_en 9, bootimgs 67,
bootimgs_B 67, rootfs 318, app 9, bsl 19
```

## First boot

Boot on wall power. USB may stay connected: host-independent startup is already
established, and leaving USB attached preserves the console and any ADB window
for observation. Do not reload RAM Linux before the first native boot; vendor
program/read checks are not independent readback.

## What this procedure fixed

The Candidate 04 and 05 attempts failed for host-side reasons, all corrected
here: a missing `07_IMAGE`, a wrapper that deleted `08_IMAGE` before every
attempt, duplicate helpers contending for interface 0, an unsupervised helper
whose 120-second timeout expired before the operator could act, and a
persistent helper that claimed the device at `FE` before iROM ever appeared.
None of these were device faults.
