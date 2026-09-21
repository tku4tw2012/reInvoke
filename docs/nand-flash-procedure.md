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
| `08_IMAGE.stock` | Source of truth. Keep it, but do **not** copy it to `08_IMAGE`: see "Do not serve `08_IMAGE`" below |
| `09_IMAGE` | Opaque record |
| `79_IMAGE` | Comment-only script; must stay comment-only |
| `81_IMAGE`, `82_IMAGE` | Recovery payloads |
| `bcm_erom.bin.usb` | The iROM bootstrap sent at FF |
| `bootloader.img`, `drm_erom.img`, `sysinit.img` | Boot chain |

Verify every time:

```bash
# 08_IMAGE itself must be absent; only the .stock copy is carried.
for f in 06_IMAGE 07_IMAGE 08_IMAGE.stock 09_IMAGE 79_IMAGE 81_IMAGE 82_IMAGE \
         bcm_erom.bin.usb bootloader.img drm_erom.img sysinit.img; do
  cmp -s "<known-good>/$f" "<staging>/$f" && echo "same  $f" || echo "DIFF  $f"
done
```

## Host setup

Catching the device, deciding a prompt is live, and writing are three separate
steps. They used to be one command, `arm-flash.sh`, whose console driver waited
for a prompt and then immediately sent `l2nand 83`. That coupling is what most
of the older guidance on this page was written around. It was removed on
2026-09-21 rather than left beside the replacement, because two flash paths
is how the wrong one gets used.

**Seizing and flashing are not the same operation, and the difference is the
point.** `arm-seize.sh` catches the device and then *holds* the prompt,
sending nothing. There is no window to hit and no timer running. Entry can be
attempted as many times as it takes, and once one lands the prompt stays held
until a command is written to it. The write is a separate command issued
afterwards, at whatever pace the work needs.

```bash
REINVOKE_ARCHIVE=... tools/usb-boot/arm-seize.sh <staging> <evidence-dir>
# ... operator enters service mode, as many attempts as needed ...
tools/usb-boot/prompt-control.sh                 # does a prompt answer?
tools/usb-boot/flash-nand.sh <evidence-dir>      # only then, the write
```

Wait for `READY` before touching the speaker. `arm-seize.sh` refuses to start
if `08_IMAGE` is present, so a staging mistake is caught before any hardware
interaction rather than after a run of failed entries. It starts exactly one
helper and one console relay, then leaves both waiting and restarts either if
it exits. Nothing else should watch, claim or reset the USB device. The helper
matches the Invoke by vendor and product identifiers, not a host port path.

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

Leave USB connected throughout and cycle mains power only. Unplug mains, hold
the Reset pinhole, restore mains while still holding it, then press MicOff
exactly four times within five seconds. Yellow indicates the mode is armed.

**Keep holding Reset until the U-Boot console appears, then release.** This is
the vendor instruction in `Instructions.pdf` (Process 2, step 7) shipped in the
`invoke-flashing` bundle, and it agrees with [U-Boot access](uboot-access.md).

An earlier revision of this file said to release at yellow and claimed that
holding longer "makes no difference; measured on this unit". That claim was
introduced in a tooling commit that contains no such measurement. It is
withdrawn: nothing here ever tested it, and it contradicts the vendor source.

If the device instead disconnects repeatedly for more than ten seconds, the
vendor remedy is to unplug mains, wait ten seconds and restore mains, leaving
USB untouched.

Entry is genuinely unreliable and often needs several attempts. With the
seizer running there is no window to miss, so simply repeat. Many failed
attempts before one takes is normal and not a fault.

### Telling a working attempt from a failed one, while it is happening

The advice above is the vendor's and is about the operator's hands. It says
nothing about how to know, within seconds, whether the attempt took. Five
runs recorded under the two-stage tooling give a signal that does, and it is
counted from the helper's own log rather than judged by feel.

| run | `0x08` refusals | `subclass=0xFF` | outcome |
| --- | --- | --- | --- |
| seize-228-1623 | 2 | yes | seized |
| seize-229-2326 | 2 | yes | seized |
| seize-2210-1014 | 2 | yes | seized |
| seize-228-1608 | 6 | never | fell through to a normal boot |
| seize-2210-0959 | 6 | never | fell through to a normal boot |

Every attempt that worked refused `0x08` **exactly twice** and then saw the
device reappear at `subclass=0xFF`, which the helper logs as
`Device is in iROM mode. Starting Phase 1.` Every attempt that failed kept
being asked for `0x08` and never reached `0xFF`.

So a third request for `0x08` means that attempt is already lost. There is
nothing to wait for and nothing to fix on the host: release, let it boot, and
try again. Watch for it with:

```sh
grep -ac 'type=0x08' <evidence>/seize.log     # 2 is the working number
grep -ac 'subclass=0xFF' <evidence>/seize.log # 0 means it never reached iROM
```

What the operator did differently between those runs is **not established**.
The counts are what was observed; the cause is not. This is a way to tell a
lost attempt quickly, not an explanation of why entry is unreliable.

## Why attempts fail

| Observed | Meaning |
| -------- | ------- |
| Only `Image request 0x08`, repeating | Entered at `FE`; iROM was missed. Check `08_IMAGE` is withheld, then retry the entry. |
| `Cannot open image file ...08_IMAGE` | Expected. The file is withheld on purpose and the device continues to iROM |
| `Cannot open image file ...` for any other image | Staging incomplete |
| Duplicated log lines | More than one helper running |
| `No device found within 120 seconds` | The bare helper timed out; the catcher does not |

Do not serve `08_IMAGE`. Keep it in staging as
`08_IMAGE.withheld-for-uboot-access`; `arm-seize.sh` refuses to start if the
plain name is present.

An earlier revision of this file said to "feed the device what it asks for".
That is wrong and it cost a long run of failed entries. A device that has not
entered recovery asks for `0x08` and resumes its normal boot once it is
answered, so answering helps it leave the state we are trying to catch. The
staging that caught iROM on every attempt withheld the file and recorded zero
`0x08` requests; staging that served it logged repeated `0x08` at subclass
`FE` and never reached Phase 1. [U-Boot access](uboot-access.md) has always
required "recovery-only staging: `08_IMAGE` absent".

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
here: a missing `07_IMAGE`, duplicate helpers contending for interface 0, an
unsupervised helper
whose 120-second timeout expired before the operator could act, and a
persistent helper that claimed the device at `FE` before iROM ever appeared.
None of these were device faults.

This list previously also blamed "a wrapper that deleted `08_IMAGE` before
every attempt". That attribution is withdrawn. Withholding the file is what
makes the device reach iROM, and serving it is what kept candidate 05.8.11
stuck: twelve consecutive enumerations at `FE` with no iROM, then a successful
flash on the first attempt after the file was withheld. What is genuinely
unsafe is destroying `08_IMAGE.stock`, which is the only copy of the record.
Rename rather than delete.
