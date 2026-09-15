---
title: Faster USB boot helper
description: Poll and attach timing that fits inside the measured iROM window
ms.date: 2026-09-15
ms.topic: reference
---

The pinned helper at `jryruegas92/hk-invoke-arm-flasher` commit `63444e82`
waits for the device with a one-second poll and then pauses another second
before its first transfer.

```c
#define POLL_INTERVAL_S 1
#define ATTACH_DELAY_US 1000000
```

The iROM window on this unit was measured at 1.8 to 3.5 seconds, so the
upstream timing can consume most of it and occasionally all of it. On the night
of 2026-09-14 the helper recorded twenty-two device sightings without a single
iROM catch.

This copy changes only those two constants:

| constant | upstream | here |
| --- | --- | --- |
| poll gap | 1000 ms | 20 ms |
| attach delay | 1000 ms | 150 ms |

150 ms remains far longer than the roughly 19 ms iROM exchange.

Measured with `strace -e trace=clock_nanosleep` over five seconds:

| build | sleeps |
| --- | --- |
| upstream `63444e82` | 5 |
| this copy | 238 |

Candidate 05.8 was caught eleven seconds after the first sighting and flashed
on the first attempt after this change.

Build it with:

```bash
gcc -O2 -o usb_boot_arm usb_boot_arm.c $(pkg-config --cflags --libs libusb-1.0)
```

Point the flash wrapper at the result:

```bash
INVOKE_USB_BOOT_BIN=<path>/usb_boot_arm \
  tools/usb-boot/arm-flash.sh <staging> <83_IMAGE-sha256> <evidence>
```
