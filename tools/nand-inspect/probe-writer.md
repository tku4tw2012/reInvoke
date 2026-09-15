---
title: Fixed NAND writer contracts
description: Executed scopes, retained recovery tools, and limits of the current writer
ms.date: 2026-09-11
ms.topic: reference
---

## Start with the current decision

Use [NAND startup status](../../docs/nand-write-decision.md) for the current
device state, next experiment and approval boundary. This page is a technical
reference, not a queue of commands to run.

The complete pilot, BSL v2 and compact BSL installations passed independent
readback. The compact profile is the latest forward variation; its normal-boot
tests returned FF without ADB. Both BSL profiles leave the main rootfs unchanged. Do not
repeat completed installations using their old starting-state files.

## Executables and exact scopes

The default inspector has no write command. The separately built
`cmd/nand-probe-write` uses compile-time profiles:

| Profile/action | Boundaries | Status |
|---|---|---|
| Default two-block install/restore | `[0x02ca0000,0x02ce0000)` | Historical probe and approved restoration completed |
| `nandpilot` install/restore | `[0x02920000,0x04fa0000)` | Complete pilot installation completed across the initial write and resume |
| Pilot uniform `resume` | Same pilot extent; write only 137..307, then 0 | Completed; its required mixed starting state no longer exists |
| `nandbsl` install | `[0x01a20000,0x01f20000)`; 40 blocks / 5 MiB | Forward-only BSL v2; restore and resume unsupported |
| `nandbslcompact` install | Same 40-block BSL allocation | Compact image installed/read back; normal boot returned FF without ADB |

The current rootfs must not be passed to the two-block restore as though that
were rollback of the complete pilot. Whole-pilot rollback requires the exact
40,370,176-byte original extent and separate owner approval.

Files, proposal JSON, kernel names and offset flags cannot override compiled
target pins. There are no arbitrary CLI offsets. A confirmation literal is a
software guard, not owner authorization.

## Forward-only BSL v2 profile

Build the existing `cmd/nand-probe-write` with `-tags nandbsl`; it is not a new
writer. The default profile excludes that tag. Combining `nandpilot,nandbsl`
fails compilation rather than silently selecting a target.

The approved artifact directory is
`build/artifacts/reinvoke-bsl-v2-20260911-slim02/` in the sibling archive.

| Input or sentinel | Bytes | SHA-256 |
|---|---:|---|
| `payload.bin` | 5242880 | `91a9dde15fea2959232fc9a0cdafdb739e08a7498c4e8b57d867889c8f358163` |
| `current-bsl.bin` | 5242880 | `238187a1d490d43dc9e44c658f88735900d3b6dfc7ed312313b9d267156dd92c` |
| `bsl.squashfs` (validation only) | 5111808 | `16921ec74f3f88f19bba741319f9a60c2adefbf6da2e655b418f0ebc564d3194` |
| Adjacent block before BSL | 131072 | `b5a41c3758763bbec72769fab4a2533bf2db0b6312d93d25a695f9e4b9e02260` |
| Adjacent block after BSL | 131072 | `75c62f48adf907f820d0568cc1966ca5dfa157633851e6556f9adbb42064e6bc` |
| Each of eight version-table reads | 848 | `e25ca94fac7c1fac5df425246b9ea147751a6fb1be252e322e547c807f737083` |

`--original current-bsl.bin` binds the required preflight starting state; it
does **not** request or authorize restoration. Both the CLI and write engine
reject restoration for this profile before accessing a device. The old profiles
retain their old table hash and separately approved restore capability.

Install confirmation: `INSTALL-reinvoke-bsl-v2-01a20000-01f20000`.
Read-only mapping confirmation: `MAP-ONLY-reinvoke-bsl-v2-01a20000-01f20000`.
Changed blocks 1..39 precede changed header block 0; equal blocks are preserved.
The pinned artifact's actual plan changes blocks 1..38, then 0 (39 erases);
block 39 is identical erased padding and is preserved, not erased.
The erase/page geometry remains 131072/2048 bytes. No whole-chip vendor erase,
rootfs writes, new ECC allowance, or continuation policy is introduced. The
rootfs payload pin remains `c580a8ff440fee68bd001777a16e24b6feb4446e84b1e7d44d34cd1fd3f37e17`.

Offline validation uses the regular files only:

```sh
nand-bsl-write-host plan --image payload.bin --original current-bsl.bin
```

Run existing Go tests for `./internal/probewrite ./cmd/nand-probe-write` with
`-tags nandbsl` and `NAND_BSL_ARTIFACT_DIR` set to that directory to include the
actual artifact checks. Generic bounds, mapping, cleanup, failure-detection and
journal tests also run under `nandbsl`; only explicitly legacy restore/artifact
tests are excluded. The full-span sparse fixture is shared with pilot tests.

## Compact BSL profile

Build with `-tags nandbslcompact`. The artifact directory is
`build/artifacts/reinvoke-bsl-compact-20260911/`. Mixed BSL profile tags fail
compilation. The engine, geometry, sentinel hashes and strict ECC policy are
unchanged; the input pins and confirmation identify this separate transition:

| Input | Bytes | SHA-256 |
|---|---:|---|
| `payload.bin` | 5242880 | `606923f27236a1bf86807c19354c772dea65ddefca73a0ba075a59f68c4ffec1` |
| `current-bsl.bin` | 5242880 | `91a9dde15fea2959232fc9a0cdafdb739e08a7498c4e8b57d867889c8f358163` |
| `bsl.squashfs` | 2445312 | `d23e1844df71cf58b5f1313038116bc1055db9432cfb726a074e43de52bfee88` |

The actual plan writes blocks 8 through 38 and then header 0; blocks 1 through
7 and 39 remain equal and untouched. Install confirmation is
`INSTALL-reinvoke-bsl-compact-01a20000-01f20000`; the read-only mapping
confirmation is `MAP-ONLY-reinvoke-bsl-compact-01a20000-01f20000`.
The 67 targeted tests include retained artifacts when `NAND_BSL_ARTIFACT_DIR`
points to that directory. No restoration or automatic retry is implemented.

## Mechanism worth keeping

1. Validate exact-size payload and original-state file, then pin their open regular-file
   descriptors. Verify block buffers against the authenticated plan.
2. Require the identified cleanup-fixed ARM RAM kernel, adequate RAM, expected
   device topology, no competing MTD users and no NAND mounts.
3. Check every target block, exposed OOB, ECC counters, version tables and
   adjacent-block sentinels before mutation.
4. Create a bounded temporary MTD partition through the read-only master.
   Compare its complete contents with the corresponding master range. Only
   then acquire and validate the same partition's writable descriptor.
5. Program full pages only within changed erase blocks; skip all-FF pages.
   For a full filesystem, write its changed header block last.
6. Verify data/ECC/OOB, remove the owned mapping, close descriptors and report
   the actual operation result. Preserve evidence on the host.

Header-last is not atomicity. A stop during programming leaves a partial
filesystem; do not reboot or automatically retry.

The exact fixed kernel banner is checked independently of `uname -r`, because
the corrected and old kernels share a module ABI. The old kernel contains the
MTD removal double-free; do not use it for these mapping operations.

## ECC and continuation

Ordinary `install`/`restore` still require zero corrections on fresh block
readback. Their preflight can tolerate corrected original data; explicit
restore can also accept damaged current data only inside the intended changed
blocks. Neither action runs automatically.

The owner-approved uniform pilot resume permits at most one controller-reported
correction per actual 2 KiB candidate-page main read and separately per actual
32-byte OOB read. Exact bytes, all-FF exposed OOB and zero new failed-ECC
increments remain mandatory. Original pending blocks retain the existing
corrected-read preflight policy until programmed.

The lifetime failed-ECC counter may include historical reads outside the target.
Capture its starting value, reject every new increment or reset, and never
clear or silently rebase it. Bad-block/BBT inventory stays fixed throughout
the operation. No bad-block marking, skipping into new space or raw OOB
fabrication exists.

The resume accepted only candidate blocks 1..136 and original blocks
0,137..307. It never rewrote 1..136. **Do not use that fixed-state resume on a
different interruption or the now-complete image.** A future stable installer
should have its continuation policy prepared before writing, rather than
generating another special-case tool during an interrupted installation.

## Logs and completion

Events are synced to the device-side RAM journal before being mirrored to
host stdout. They include block offsets/hashes, correction counts and phases.
`read-checks-complete` explicitly precedes cleanup.

Require the full extent verification, successful cleanup and zero returned
process status. `closed-no-reboot` can accompany an earlier error; inspect its
detail and the exit code. A surviving process, a green host test, or a USB name
is not the same as operation success.

The kernel can block inside a syscall; Go cancellation cannot undo that.
A signal before erase stops mutation; once erased, a block is finished and
verified before the stop is honored. Do not start a competing writer.

## Retained artifacts

Paths below are relative to the sibling archive:

* Final image/rollback:
  `build/artifacts/reinvoke-nand-pilot-01-20260909-pty01/`
* Initial full-pilot writer:
  `build/artifacts/nand-pilot-writer-20260909/resume-20260909T2224Z/`
* Executed uniform resume:
  `build/artifacts/nand-pilot-resume-20260909/uniform-page-ecc1/`
* Completed write and independent readback:
  `evidence/nand-pilot-uniform-resume-20260909/INSTALL-COMPLETE-SHA256SUMS`
* Original two-block tool:
  `build/artifacts/nand-two-block-install-20260909/`

The final payload is
`c580a8ff440fee68bd001777a16e24b6feb4446e84b1e7d44d34cd1fd3f37e17`;
whole-pilot original rollback is
`3bb7f29b7961424c9b27d3e908477643840a6cf3967f65a0e7ac33990c26fc6d`.
The executed uniform ARM binary is
`e8767ed51366e6daa4cabd6cd2246309696472f433670484f04f1f485828d21e`.

Keep obsolete binaries/evidence for audit, but do not select them by a filename
such as "latest." The old broad probe, pre-PTY candidate, BusyBox-patched
candidate and superseded resume builds are not current execution instructions.
