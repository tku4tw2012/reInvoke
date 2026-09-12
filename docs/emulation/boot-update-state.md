---
title: Historical donor boot and update state
description: Vendor fw_stat markers, write semantics and unresolved boot-slot behavior
ms.date: 2026-09-12
ms.topic: reference
---

Static disassembly of the final firmware establishes the `fw_stat` update
markers, not active-slot selection. reInvoke does not run this updater.
Runtime settings remain volatile; native installation is a separate
[whole-good-block operation](../nand-write-decision.md).

## Command surface

The donor `usr/bin/mtd_exec` accepts exactly one argument, `setbootflags`,
dispatched to `fw_stat_init`. It is an 11,696-byte ARM EABI5 executable,
SHA-256 `2a29aa859b43aa900e912bad96db8e5eb8830ac8928f82cf7feb67dcea8ebef6`.

> [!CAUTION]
> `/usr/bin/mtd_exec setbootflags` rewrites persistent storage through the
> vendor MTD writer. It is not a status query or a reInvoke runtime command.

## Persistent record

`mtd_open` resolves partition label `fw_stat` through `/proc/mtd`, then opens
`/dev/mtd/mtdN` or `/dev/mtdN`. `read_fw_status` reads 2,832 bytes (`0xb10`);
`update_fw_status` changes the first 32-bit word:

| Existing bytes         | New bytes              | Meaning after change |
| ---------------------- | ---------------------- | -------------------- |
| `71 65 72 75` (`qeru`) | `70 75 6f 6e` (`puon`) | No update            |
| Anything else          | `71 65 72 75` (`qeru`) | Update required      |

The adjacent donor messages and recovery loader's `qeru` comparison confirm
these meanings. `write_fw_status` stages the entire record in `/run/stat`,
rewinds it, invokes the MTD writer for `fw_stat`, and removes the staging file.
`/run` is tmpfs; the persistent source of truth is the partition. A four-byte
marker change still enters an erase/write path and must preserve the other
record bytes.

## OTA transition

The silent-upgrade path in `usr/bin/client` checks `/lsync/rbua/run.sh` and
`/data/upgrade/rb_ua`, calls `mtd_exec setbootflags`, waits 50 ms, calls `sync`,
and reboots. The recovery updater has a matching status-initialization,
50 ms wait and reboot path.

The inferred cycle is `puon` -> client sets `qeru` -> recovery writes images
-> updater restores `puon` -> reboot. Marker direction is established; the
complete cycle is inferred from separate callers.

## Unresolved selection and retention

No evidence maps the marker to a U-Boot environment variable, numbered
active/inactive slot, rollback counter or complete secure-boot rule. Recovery
fstab names `bootimgs` and `rootfs`; installer IFS/IPL strings do not resolve
slot selection.

On the observed unit `fw_stat` occupies `[0x0fe20000,0x0ff20000)`. Linux MTD
numbering varies with the boot's partition view. Later vendor `l2nand 83`
installations erased all good blocks before programming listed records:
omitting `fw_stat` from a bundle did not preserve it, and those installations
do not resolve the donor update semantics.
