---
title: Native settings persistence
description: Verified app-partition storage, saved station profiles and bounded settings snapshots
ms.date: 2026-09-12
ms.topic: reference
---

Candidate 04 stores selected settings in `/persist/reinvoke/state.json` on the
existing app allocation. System images remain read-only; runtime state,
logs and audio remain in RAM. This preserves settings across ordinary boots,
not the vendor installer's whole-good-block erase.

## Storage gate

The donor scripts and Berlin configuration identify YAFFS2. The helper requires:

* Kernel `3.8.13-yocto-standard` or `3.8.13-reinvoke-audio-sd8887`, with YAFFS2
* Exactly one named `app` partition: 123 MiB, 128 KiB erase blocks, 2048-byte
  pages and the expected 64-byte MTD OOB declaration
* Matching procfs/sysfs NAND and block-device identities; offset `0x08320000`
  when the kernel publishes an offset
* An unmounted partition, empty private `/persist`, and a successful
  `rw,nosuid,nodev,noexec,noatime` mount whose identity is rechecked

The current Bluedroid runtime also binds `/persist/reinvoke/bluedroid` at
`/home/galois_rwdata/misc/bluedroid` (the resolved `/data/misc/bluedroid`
path). The guard permits that exact subdirectory bind alongside the primary
mount, with the same device, filesystem and restrictive flags. A bind without
the primary mount, duplicates, and unexpected aliases remain errors.

If startup removed generic MTD nodes, the helper creates only a private block
node for the already verified app device. It never adds partitions, formats,
erases, marks bad blocks, selects the whole-chip device, or runs vendor startup.
Unavailable or invalid storage is reported explicitly; Bluetooth and physical
setup continue without durable settings.

## Retained state

| Data        | Policy                                                                        |
| ----------- | ----------------------------------------------------------------------------- |
| Wi-Fi       | Last successfully associated SSID, derived WPA2 PSK, security and hidden flag |
| Bluetooth   | Selected BlueZ peer `info` and `attributes` files; no discovery cache         |
| Preferences | microphone mute and music-volume preference                                |

One versioned, checksummed envelope limits selected data to 1 MiB and the
encoded snapshot to 2 MiB. Symlinks, special files, unsafe ownership/modes,
invalid values and corrupt snapshots are rejected.

Changed settings are captured every 30 seconds; identical snapshots are not
rewritten. Commits use a same-filesystem private temporary file, file fsync,
rename and directory fsync. Orderly shutdown flushes after writers stop.
Abrupt power loss can discard recent changes. Actual NAND power-cut behavior
remains untested; checksums are corruption detection, not encryption.

Music restoration retains the existing 12-percent reconnect ceiling and never
raises a quieter transport. Zero stays zero; transport mute does not overwrite
the volume preference. A previously louder setting remains recorded but is
not automatically restored at that loudness.

## Wi-Fi resume and seed

Applyd saves a profile only after `wpa_state=COMPLETED`, not the parser's earlier
HTTP 202. Association and durable save have separate status results; DHCP
remains networkd's responsibility.

`reinvoke-wifi-applyd --resume --connect-timeout 20s` attempts the saved profile
once before physical setup starts. A private immutable seed is eligible only
when the store verifies that no saved profile exists. It never overrides a
saved profile or substitutes for corrupt/unavailable storage. Seed persistence
is an atomic absent-only commit after association succeeds.

The installer accepts either a validated provisioning request or a derived-PSK
seed. Only derived credentials enter the image. The seed and resulting image
remain private.

## Build integration

`PILOT_PERSISTENCE_CONFIG` names a private JSON manifest with `persist`, `applyd`
and `mcu` entries, each `{ "path": "<absolute-artifact>", "sha256": "<digest>" }`.
Optional `seedProfile` has the same wrapper and identifies a private `0600`
JSON file. [The installer](../persistence-config.js) validates all inputs before
replacing two existing binaries and adding `/bin/reinvoke-persist`.

Build the new helper from this directory with the retained Go 1.18 toolchain:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 GOMAXPROCS=2 \
  "$GO" build -p 1 -trimpath -buildvcs=false \
  -ldflags="-s -w -buildid=" -o "$OUTPUT" .
```

Build the changed [Wi-Fi adapter](../../provisioning/README.md) and
[MCU service](../../mcu-interface/build.sh) with their existing builders.
The [runtime patch](../patch-runtime.js) composes:

1. Bounded `prepare` before BlueZ/MCU, then independently supervised `serve`
2. Serialized saved-profile resume before the provisioning window
3. MCU `--music-volume-state /run/reinvoke/music-volume`
4. Writer-first shutdown followed by a bounded persistence flush

ADB is independent of this service and makes no persistence claim.

## Diagnostics and tests

`/run/reinvoke/persistence-status.json` records version, phase, snapshot/profile
presence and a named result. `/run/reinvoke/wifi-persistence-status` distinguishes
association, save, resume and failure. Neither exposes profiles or setting
values. These are last-operation records, not process-liveness or DHCP proof.
The [status implementation](status.go) defines the exact schema.

Run `"$GO" test -p 1 .` here for storage, corruption, mount-selection and
snapshot tests. Installer tests are in
[persistence-config-test.js](../persistence-config-test.js); Wi-Fi and music
regressions remain beside their implementations. Tests use synthetic storage
and command runners, never live NAND or radio operations.
