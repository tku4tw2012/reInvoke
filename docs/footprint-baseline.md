---
title: Footprint and runtime baseline
description: Measured comparison between the stock Harman image and reInvoke
ms.date: 2026-09-15
ms.topic: reference
---

Measured on candidate 05.8, 2026-09-15, against the stock rootfs preserved in
the build tree as `source-stock`.

## Disk footprint

| | stock Harman | reInvoke 05.8 |
| --- | --- | --- |
| rootfs size | 119 MB | 83 MB |
| files | 2,596 | 396 |
| ELF binaries | 761 | 145 |
| supervised services | 11 | 10 |

reInvoke is roughly 30 percent smaller on disk and carries about a seventh of
the files. The service count is almost unchanged because the runtime still
manages the same hardware; the difference is in what supports those services.

## What stock carries that reInvoke does not

The ten largest absent components:

| size | component |
| --- | --- |
| 10 MB | `libwamp_framework.so` |
| 3 MB | `libgstlibav.so` |
| 3 MB | `dhclient` |
| 2 MB | `libxml2` |
| 2 MB | `libstdc++` |
| 2 MB | `libsamplerate` |
| 2 MB | `libpython2.7` |
| 2 MB | `libperl` |
| 2 MB | `libgstreamer-1.0` |
| 2 MB | `libgio-2.0` |

By category:

| component | stock files | reInvoke |
| --- | --- | --- |
| Python | 9 | 0 |
| Perl | 6 | 0 |
| GStreamer | 5 | 0 |
| Cortana | 3 | 0 |

Two interpreters, a full media framework and the voice assistant are absent.
The WAMP bus remains, but through `bonefish` rather than the 10 MB vendor
framework.

## Runtime measurements

Taken three minutes after a cold boot, idle, with Bluetooth connected:

| | value |
| --- | --- |
| total memory | 462,308 kB |
| used | 197,892 kB |
| used excluding buffers and cache | 160,492 kB |
| free excluding buffers and cache | 301,816 kB |
| processes | 103 total, 8 reInvoke services |
| load average | 1.97, 0.89, 0.34 |

About 35 percent of memory is in use with roughly 295 MB available. The
one-minute load reflects the Bluetooth pairing work during the measurement; the
fifteen-minute figure of 0.34 is closer to the idle state.

## What is not measured

No comparable figures were taken from the stock firmware running on hardware.
Every runtime number here describes reInvoke only, and the disk comparison is
between file trees rather than two live systems. A direct comparison would mean
flashing stock back and repeating the measurements, which has not been done.

Audio has been verified as negotiated at 44,100 Hz stereo, not as heard, so no
claim is made about playback performance.

## The duplicate module tree is not waste

Two module trees ship:

| tree | size | modules |
| --- | --- | --- |
| `3.8.13-yocto-standard` | 1,084 KB | 3 |
| `3.8.13-reinvoke-audio-sd8887` | 1,148 KB | 3 |

The vendor kernel boots, so only the first is loaded. Loading a module from the
second fails outright:

```text
insmod: init_module 'bt8xxx.ko' failed (Exec format error)
```

The files differ in size and each declares its own `vermagic`, so this is not a
duplicate copy of the same build.

It was recorded here first as recoverable waste. It is not. The second tree is
staged for the custom kernel: `/etc/reinvoke-release` names it as the
replacement module set, and `module-manifest.json` lists its members. It
becomes the live tree the moment a reInvoke kernel boots, and removing it would
make that kernel unable to bring up Wi-Fi or Bluetooth.

The operator was right to question the original claim.

## Actually recoverable

| item | size | note |
| --- | --- | --- |
| `reinvoke-provision-windowd` unstripped | 1,097 KB | ours, safe to strip |

`mount_part` and `ethconfig` carry 5,680 KB of symbols between them, but they
are vendor binaries and are deliberately left untouched.

Every Go service this project builds is already stripped with `-s -w`. The
remaining size is Go runtime rather than debug information, so it cannot be
reduced without changing language.
