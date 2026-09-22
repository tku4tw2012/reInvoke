---
title: Offline Invoke NAND inspector
description: Validate Marvell image records and archived NAND version tables without accessing hardware
---

## Scope

For the current device state and a minimal operating sequence, start with
[NAND startup status](../../docs/nand-write-decision.md). The completed pilot
has not passed normal-boot acceptance; these tools do not change that fact.

The default Linux host executable reads regular files only. It has no flashing command,
image repacker, extraction operation, ADB invocation, or MTD ioctl. It refuses
devices, pipes, symlinks, and directories, and opens accepted inputs read-only.
Output is metadata-only JSON on stdout.

An accepted report proves the stated structural checks, not that an image
will boot or that a writer will leave other regions unchanged.

## Run with a pinned Go toolchain

The build uses Go 1.18.1, held in the external archive rather than installed
system-wide. Point `GOROOT` at whichever copy you have; the offline flags
matter more than the location.

From this directory:

```bash
export GOROOT=/path/to/go-1.18
export GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly GOWORK=off
"$GOROOT/bin/go" test ./...
"$GOROOT/bin/go" run . container /absolute/path/to/83_IMAGE
"$GOROOT/bin/go" run . capture /absolute/path/to/invoke-nand-data.bin
"$GOROOT/bin/go" run . compare /absolute/path/to/83_IMAGE /absolute/path/to/invoke-nand-data.bin
```

Keep generated reports and binaries in the external archive. Do not put
firmware, NAND captures, or extracted credentials in Git.

## Checks

The container command:

* validates the 64-byte fixed header and 64-byte descriptors;
* distinguishes NAND blocks/chip from the payload offset;
* derives concatenated payload offsets from each 64-bit stored size;
* checks every descriptor's payload CRC32;
* rejects invalid counts, truncated input, out-of-device allocations,
  arithmetic overflow, duplicate names, and unsupported fields;
* identifies the trailer separately from the last payload;
* reports SquashFS superblock metadata where present.

The capture command:

* assumes a data-only Invoke capture with 2 KiB pages and 128 KiB erase blocks;
* reads version tables from the final 4 KiB of blocks 1 through 8;
* checks the source-defined CRC residue over each table including its trailer;
* reports absent copies and rejects corrupt or disagreeing recognized copies;
* reports both part addresses and versions without guessing the active one.

The compare command requires matching geometry and matching allocations for
every named container record in both captured part fields. Capture-only records
remain visible in the report. It does not compare image payloads or assign
permissions to the reported regions.

CRC validation is not authentication. A ZIP signature only identifies the
trailer prefix; the ZIP contents are not validated or displayed. A parsed
SquashFS superblock does not validate every file in that filesystem.

## Source and prior art

The format is documented in Marvell's
[version_table.h](https://nest-open-source.googlesource.com/manifest_repos/bootloader/+/836ad32e08388e0e4ce8d03fe4f14d2c3ea8ba13/berlin_tools/bootloader/include/version_table.h)
and the table-discovery/CRC code in
[bootloader.c](https://nest-open-source.googlesource.com/manifest_repos/bootloader/+/836ad32e08388e0e4ce8d03fe4f14d2c3ea8ba13/berlin_tools/bootloader/bootloader.c).

The retained community `parse_ota83.py` established the concatenated payload
interpretation. It reports CRC booleans but exits successfully even on a failed
payload CRC, does not decode captured version tables, and leaves several fields
unnamed. This implementation follows the source-defined structures and makes
validation failure an error exit with no success report.

Relevant interpretation:

* `data_type=0` is normal, `1` is OOB, and `2` is raw. OOB is not a sparse flag.
* `partition_type=0/1/2` is the source MLC/SLC/ESLC enum, not independent
  measurement of the physical chip's cell technology.
* The image header's OOB field is container metadata. Zero there does not mean
  the physical NAND has no OOB.
* A matching `part1` and `part2` address is an alias, not two independent slots.

## Tests

Tests use synthetic image and version-table bytes, not redistributed firmware.
They exercise corrupted CRCs, valid-but-conflicting mirrors, same-name
different-address records, truncation, overflow, missing copies, unsupported
fields, and prohibited input types. A test also attempts to write through an
opened input descriptor and verifies rejection and unchanged input.

## Historical diagnostic rootfs builders

The earlier two-block diagnostic was produced by the separate
[squashfs-min-probe](cmd/squashfs-min-probe) command. It replaces only the
compressed fragment containing `init.rc`, retaining its exact stored length.
Offline analysis finds two changed erase blocks rather than 367.

The [validation procedure](squashmin-validation/README.md) compares full
extracted contents and metadata and runs the original and modified streams
through host-compiled vendor kernel decompression code. These are offline
checks, not proof of normal boot, signatures or real NAND programming.

[build-startup-probe.sh](build-startup-probe.sh) builds a proposed diagnostic
filesystem from this unit's pinned capture. It never invokes ADB or writes
NAND. It requires the existing host `fakeroot`, SquashFS tools, `jq`, and
archived Go toolchain.

```bash
bash build-startup-probe.sh /absolute/path/to/invoke-nand-data-reread.bin \
  /absolute/path/outside/the/repository/new-probe-directory
```

This older full-recompression builder enables the existing USB ADB service early and writes a distinct
runtime marker. It verifies that only `init.rc` changed and independently
re-extracts the output to check all file hashes and metadata. It also saves
the exact data-area erase-block extent for a proposed rollback, and produces
`PROPOSAL.json` with `write_approved: false`.

See the [NAND write decision](../../docs/nand-write-decision.md).

The complete pilot uses [tools/nand-pilot](../nand-pilot/README.md) instead.
The historical diagnostic is not the current installation target.

## Separately built constrained writer

For a prebuilt sealed `nand2134` artifact, see the
[host trial runner](trial-runner.md): offline inspection by default, one
separately approved foreground apply, durable no-reissue evidence, then a
manual observation/power gate. It does not rebuild images or change the engine.
The historical 2134 write is complete; this is not permission to repeat it.

The default inspector is still offline-only. A separate executable under
`cmd/nand-probe-write` implements a fixed-image, fixed-extent write protocol.
Legacy profiles retain separately approved explicit rollback. The `nandbsl`
build profile is forward-only: a 40-block BSL extent at
`[0x01a20000,0x01f20000)`, with changed header block last and no restore or resume.
Its `current-bsl.bin` input authenticates preflight state, not restoration.
It is not included in the RAM launcher or run automatically.
The broad-image archive remains withdrawn after the temporary mapping cleanup
incident. The current two-block writer requires the identified corrected
kernel; its approved install and readback passed on hardware. Normal startup
did not expose the diagnostic interface, so do not repeat the installation or
run rollback automatically.
See [probe-writer.md](probe-writer.md) for tested limits, legacy-kernel details,
and commands reserved for a later attended session.
