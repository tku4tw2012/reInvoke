---
title: Native NAND platform
description: Current reInvoke NAND result, reproducible build path, recovery boundary, and remaining gaps
ms.date: 2026-09-12
ms.topic: overview
---

## Current result

reInvoke candidate 02 starts from NAND after an ordinary wall-power cycle,
without a host supplying firmware. Verified behavior on the closed Invoke
includes:

* Bluetooth discovery, encrypted pairing, A2DP playback, and physical rotary
  volume control
* Mic-Mute indicator control and the top-button indication
* Physical entry into the isolated Wi-Fi provisioning window
* Authenticated transfer of a local Wi-Fi profile and subsequent local-network
  reachability
* MCU status, DSP version, and volume calls through the WAMP compatibility bus

Native USB enumeration and ADB are not working. Wi-Fi configuration and
Bluetooth bonds remain volatile and must be established again after power loss.
The native microphone capture and privacy data path has not yet repeated the
accepted RAM-platform measurements.

The [current product contract](current-product-contract.md) defines the
normative behavior. The [NAND decision history](nand-write-decision.md) retains
the successful and failed experiments that led here.

## Installed architecture

The installed image deliberately keeps the vendor's native 12.2134.0
bootloaders, TrustZone image, and encrypted kernel containers byte-identical.
The owned changes are:

1. A read-only SquashFS bootstrap that starts the reInvoke runtime from NAND.
2. A paired BSL launcher that verifies the selected bootstrap and runtime.
3. The owned MCU, DSP, Bluetooth, audio, microphone, network, provisioning,
   logging, and compatibility services.
4. Optional USB diagnostics. Missing USB, PTY, or ADB prerequisites are
   recorded but no longer prevent the core runtime from starting.

The complete vendor-format bundle also includes the vendor-default app seed.
No firmware binary is stored in Git.

## Reproducible build

The source is under [tools/nand-pilot](../tools/nand-pilot/). Build outputs must
remain in the sibling private archive.

Build the main rootfs:

```bash
tools/nand-pilot/build.sh <archive> <main-output>
```

Build a BSL launcher bound to that exact main image, then compact it without
changing its loadable code or data:

```bash
fakeroot node tools/nand-pilot/build-bsl.js \
  <archive> <bsl-output> <main-output>

fakeroot node tools/nand-pilot/compact-bsl.js \
  <archive> <compact-bsl-output> <bsl-output>
```

Create the complete vendor-format bundle:

```bash
node tools/nand-pilot/native-bundle.js \
  <archive> <main-output> <compact-bsl-output> <bundle-output>
```

The builders validate pinned source artifacts, deterministic rootfs output,
ARM executable dependencies, paired main/BSL hashes, all vendor-record CRCs,
the 256 MiB allocation map, and a deliberately corrupted CRC control.

These checks establish reproducibility and structure. They do not prove native
boot or authorize flashing.

## Installation boundary

The demonstrated installation used the vendor unified-image programmer after
an explicit owner approval. On this unit it:

* Erased all 2,046 good NAND blocks
* Skipped the two known bad blocks
* Programmed and read back every listed record
* Installed all eight pre-bootloader copies
* Returned an interactive U-Boot prompt after reporting success

That process is not a sparse update. It erases unlisted state, including
settings and boot-status data, and replaces the app filesystem with the bundle's
default seed. The logical backups are not a programmer-grade raw restore image.

> [!CAUTION]
> Never treat a built bundle or acknowledgement string as write approval.
> Image 99 is excluded. No NAND operation, retry, reset, reboot, push, or release
> is implicit in the build commands above.

## Recovery

The open-source host helper can attach to the observed Marvell downloader state
without opening the enclosure. It supplies a recovery U-Boot and can load the
known RAM Linux environment. That recovery path has remained usable after the
observed writes.

Recovery Linux is a new host-supplied boot. It cannot retrieve volatile logs
from a failed native session and is not proof that the NAND image ran.
See [U-Boot access](uboot-access.md) for the exact distinction and evidence
limits.

## Current operating model

The useful native product does not require ADB:

* Bluetooth carries audio.
* The physical controls own local volume, pairing, privacy, and provisioning.
* WAMP provides the compatibility control surface for registered MCU and DSP
  operations.

ADB or a separately authenticated administrative channel is still valuable for
kernel logs, processes, mounts, USB diagnostics, and bounded maintenance.
WAMP is unauthenticated and is not an arbitrary command shell.

## Remaining work

The next image scope is intentionally small:

1. Use consistent NAND-specific product identity.
2. Repair native USB ADB with bounded prerequisite retry and accurate health
   reporting, without blocking the working runtime.
3. Add a key-authenticated Wi-Fi administrative fallback.
4. Keep deployment-specific peers, keys, network identifiers, and secrets
   outside the public source tree.

Persistence for Wi-Fi profiles, Bluetooth bonds, and preferences is deferred.
It needs an explicit storage, power-loss, and update-preservation design because
the demonstrated vendor installation erases the whole good-block set.

Acceptance remains appropriate for a personal DIY project: deterministic
builds, targeted failure controls, one owner-controlled power boot, and a
bounded automated service check. It is not a certification campaign.
