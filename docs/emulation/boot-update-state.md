---
title: Boot and update state
description: Recovered persistent update markers and unresolved boot-slot behavior
ms.date: 2026-09-02
ms.topic: reference
---

Static disassembly of the final firmware establishes how its persistent update
marker changes. It does not yet establish active-slot selection.

## Evidence classification

Verified facts:

* `usr/bin/mtd_exec` in the held final firmware accepts `setbootflags`.
* The code reads a 2,832-byte `fw_stat` record and rewrites its first word
  between the two marker values shown below.
* The preserved recovery-side code also checks the `qeru` marker.

Artifact-backed findings:

* `fw_stat` is an update gate, not a normal file-backed setting.
* Any persistent edit must preserve the rest of the status record and go through
  the firmware's MTD write path.

Inference:

* The normal update cycle described below is inferred from the normal-firmware
  and recovery-firmware callers. Active-slot selection remains unresolved.

## Command surface

`usr/bin/mtd_exec` is a 11,696-byte stripped ARM EABI5 executable with SHA-256:

```text
2a29aa859b43aa900e912bad96db8e5eb8830ac8928f82cf7feb67dcea8ebef6
```

Its only accepted invocation is:

```bash
/usr/bin/mtd_exec setbootflags
```

`main` requires exactly one argument and dispatches `setbootflags` to
`fw_stat_init`.

## Persistent record

The state is stored in the MTD partition labelled `fw_stat`, not in a normal
file. `mtd_open` scans `/proc/mtd`, resolves the label, and opens
`/dev/mtd/mtdN` or the fallback `/dev/mtdN`.

`read_fw_status` reads 2,832 bytes (`0xb10`) from that partition.
`update_fw_status` changes the first 32-bit word:

| Existing bytes | New bytes | Meaning after change |
|---|---|---|
| `71 65 72 75` (`qeru`) | `70 75 6f 6e` (`puon`) | No update |
| Any other value | `71 65 72 75` (`qeru`) | Update required |

The meanings are confirmed by the adjacent messages `OTA: Setting to no
Update` and `OTA: Setting to Update required`. The secondary recovery loader
also compares `qeru` and prints `Update Required`.

## Write path

`write_fw_status` writes the complete 2,832-byte record to `/run/stat`, rewinds
the file, passes it to the MTD writer for partition label `fw_stat`, and removes
the staging file. `/run` is mounted as tmpfs by `etc/fstab`; `/run/stat` is not
the persistent source of truth.

This full-record staging matters for future safety analysis. A persistent
change is not a four-byte file edit. It enters the firmware's MTD erase/write
path and must preserve the rest of the status structure.

## OTA transition

`usr/bin/client` contains `/usr/bin/mtd_exec setbootflags` at file offset
`0x73170`. Its silent-upgrade path:

1. Checks `/lsync/rbua/run.sh` and `/data/upgrade/rb_ua`.
2. Runs `mtd_exec setbootflags`.
3. Waits 50 milliseconds.
4. Calls `sync`.
5. Enters the reboot path.

The embedded recovery updater has a matching completion flow that initializes
the firmware status, waits 50 milliseconds, and reboots. Taken together, the
likely normal cycle is:

```text
puon (no update)
  -> client toggles qeru (update required)
  -> recovery updater writes images
  -> updater restores puon
  -> reboot
```

The direction of the two markers is established. The complete transition
sequence is an inference from the normal and recovery-side callers.

## What remains unresolved

The marker gates recovery updating; no evidence shows that it directly changes
a U-Boot environment variable.

The recovery fstab names `bootimgs` and `rootfs` as MTD targets. Installer
strings mention IFS and IPL selection, but preserved state does not map those
operations to a numbered active or inactive slot. Rollback counters, the
physical MTD number of `fw_stat`, and secure-boot acceptance also remain
unknown.

These gaps are why NAND writing remains outside the current procedure. A safe
persistent change needs a verified readback, known active-slot behavior, and a
recovery path tested from RAM first.
