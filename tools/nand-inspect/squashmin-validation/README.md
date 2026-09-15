---
title: Offline minimal SquashFS diagnostic
description: Build and verify the two-block startup probe without accessing the speaker
ms.date: 2026-09-11
ms.topic: how-to
---

## Scope

This tool is a **candidate builder, not a NAND writer**. It never executes vendor
init, accesses a device, mounts a filesystem, or claims boot/signature acceptance.
It changes only the hash-pinned installed `init.rc`, retaining its 8,862-byte size:

* Line 85: USB product `MRVL USB SDK` becomes `reInvoke-min` (both 12 characters).
* Line 87: unused ACM comment becomes `start adbd`, followed by 49 spaces.
  This is in `on post-fs`, after selecting `adb` and before enabling the gadget.
* No RAM marker; no other commands, services, properties, or files change.

This is an early-connectivity diagnostic, **not the standalone speaker software**.
The existing `service adbd /sbin/adbd` is retained. The installed `default.prop`
already contains `ro.secure=0` and `ro.debuggable=1`; runtime reachability is untested.

### Original-firmware ADB variant

The optional builder flag `-stock-adb` selects a separate, exact transformation:

* Line 85: USB product becomes `reInvoke-ADB`.
* Line 135: uncomment the existing on-boot `start adbd`, retaining line length.
* Line 235: replace only the adbd service's `disabled` flag with a comment.

This changes 29 byte positions in three lines, not the earlier 69-byte
post-fs probe. All other files, metadata, image size and allocation-tail bytes
remain identical. The changed fragment still occupies physical blocks 357 and
358. The original kernel, libraries, ADB executable and services are retained;
this is a stock-firmware diagnostic, not the complete reInvoke runtime.

The variant's filesystem SHA-256 is
`597b869df383f13b7e129d031c2e884b0b707d23b8279e4affdb40b9eaf3499a`,
and its init SHA-256 is
`db83188488d5a8bc73f26dbd9333dd6479c51ec50d1c80cfd97822da8ad8ee62`.
Pass `stock-adb` as the verifier's sixth argument:

```bash
tools/nand-inspect/squashmin-validation/verify-offline.sh \
  "$source_image" "$candidate" "$kernel_source" "$new_evidence" "$capture" \
  stock-adb
```

The default transformation and its pins remain unchanged. The independent
bundle checker replays the two allowed transformations and accepts only an
exact candidate and its corresponding metadata; the manifest cannot select an
arbitrary edit. Neither offline result proves the device reaches that startup.

## Implementation and source basis

New Go package: `internal/squashmin`; command: `cmd/squashfs-min-probe`.
No external Go modules. Go 1.18's best compressed result was 49,936 bytes, exceeding
the original 48,200-byte fragment. The explicit existing-host GNU gzip fallback
is therefore necessary. With GNU gzip 1.10, level 8 produces 48,100 bytes.
Level 9 produces 48,051, which this deliberately narrow filler does not accept.

The parser follows the archived
`sources/harman/invoke-kernel/Invoke-kernel/fs/squashfs/` source:

* `squashfs_fs.h:241-261,270-428`: little-endian superblock, inode references,
  basic/extended root directory records, directory entries, regular file and
  fragment records. `inode.c:141-183` resolves the basic regular-file fragment.
* `dir.c:122-199`: directory size includes three synthetic dot-entry bytes;
  headers contain count-minus-one and names contain length-minus-one.
* `fragment.c:48-66`: fragment indexes address compressed metadata containing
  16-byte entries; the data's compression flag is bit 24 (`squashfs_fs.h:122-127`).
* `block.c:103-126`: the unchanged fragment entry determines input length and
  the device-buffer range.
* `zlib_wrapper.c:64-138`: requires `Z_STREAM_END` and all input buffers consumed;
  **ordinary trailing-data padding is rejected**.
* `lib/zlib_inflate/inflate.c`, `case STORED`: byte alignment, LEN/NLEN check,
  zero-length copy, then next block. `case CHECK` checks Adler-32; `case DONE`
  returns stream end.

The replacement adds twenty legal non-final empty DEFLATE stored blocks
(`00 00 00 ff ff`) **inside** the zlib stream, immediately after its two-byte
header, before the compressor's deflate data. The original Adler-32 trailer
remains last. These blocks consume 100 bytes and emit none. This is not a
concatenated stream or unused trailing bytes. The stored-size field is unchanged.
GNU gzip's no-name framing is parsed/CRC-verified with Go `compress/gzip`, then
reframed as zlib with Adler-32 and checked again with `compress/zlib`.

The available community `hk-invoke-opensource-speaker/scripts/hk-invoke/parse_ota83.py`
was consulted for prior-art context, but it parses OTA container metadata, not
SquashFS. No `parseFS` file was found in the installed archive. Community assertions
about signatures are not treated as verification.

## Reproduce without touching hardware

Run from the repository. All generated artifacts and work directories must be
outside Git, in a new archive directory. The builder refuses existing output
directories, nonregular input files, and incorrect image/capture hashes.

```bash
archive="$(realpath ../reinvoke-archive)"
work="${archive}/derived/YOUR-NEW-DIRECTORY"
mkdir -m 700 "$work"
mkdir -m 700 "$work/go-work"
export GOROOT="${archive}/toolchains/ubuntu-go-1.18.1/extracted/usr/lib/go-1.18"
export GOMAXPROCS=2 GOPROXY=off GOSUMDB=off GOWORK=off CGO_ENABLED=0
export GOFLAGS=-mod=readonly GOTMPDIR="$work/go-work" TMPDIR="$work/go-work"
source_image="${archive}/hardware/dumps/20260902T215700Z-native-ram/installed-rootfs.squashfs"
capture="${archive}/hardware/dumps/20260907T140416Z-nand-readonly/invoke-nand-data-reread.bin"
(
  cd tools/nand-inspect
  SQUASHMIN_SOURCE="$source_image" nice -n 10 "$GOROOT/bin/go" test -p 1 -v \
    ./internal/squashmin ./cmd/squashfs-min-probe
  nice -n 10 "$GOROOT/bin/go" build -p 1 -trimpath \
    -o "$work/squashfs-min-probe" ./cmd/squashfs-min-probe
)
nice -n 10 "$work/squashfs-min-probe" -source "$source_image" \
  -capture "$capture" -out "$work/candidate"
bash tools/nand-inspect/squashmin-validation/verify-offline.sh \
  "$source_image" "$work/candidate" \
  "${archive}/sources/harman/invoke-kernel/Invoke-kernel" "$work/verification" "$capture"
```

The validation script fully extracts **both** filesystems under fakeroot,
compares all regular-file hashes (allowing only `init.rc`), file types, numeric
UID/GID, modes, mtimes, link counts, symlink targets, and canonical hardlink groups.
It then removes the private extracted trees. Metadata tables, including inode
numbers and original timestamps, remain byte-identical on disk independently
of host extraction. The integration test verifies this and the unchanged bytes
of other files sharing the fragment.

The verifier requires the pinned NAND capture as its fifth argument. Its Go
`bundlecheck` helper hashes that complete capture, binds the original image to
its rootfs allocation, replays the canonical patch, and independently scans
image byte differences to reconstruct **every** proposal field and the ordered,
complete original/candidate erase-block payloads. This includes unchanged block
padding, full allocation/tail hashes, all indices/offsets, counts, first/last
differences, per-block hashes, patch metadata and safety flags. Missing, unknown,
duplicate or conflicting JSON fields fail closed. A checksum snapshot must also
remain unchanged throughout extraction and kernel-source verification.

Canonical replay uses the existing host GNU gzip and deliberately rejects a
bundle it cannot reproduce; it does not trust the proposal's compressor claims.
No candidate or original artifact is rewritten. To run the binding regression
tests against the private fixture, also set `SQUASHMIN_CAPTURE="$capture"` and
`SQUASHMIN_BUNDLE="$work/candidate"` with `SQUASHMIN_SOURCE="$source_image"`.

The C harness compiles the **actual archived inflater** and an exact extracted
`zlib_uncompress` function, with userspace buffer/lock mocks only. It checks both
1,024- and 4,096-byte input blocks, 4,096-byte output pages, original and candidate
streams, exact input consumption, identical output, and one release per input
buffer. Appended trailing data, corrupt Adler-32, and truncated trailers must fail.
This is host execution of target source, not target execution or a kernel build.
Compiler fall-through warnings originate in the unchanged archived inflater.

## Verified 2026-09-09 result

Evidence: sibling archive `derived/squashmin-20260909T1320Z/`.

* Image length and `bytes_used`: **48,831,891**, unchanged.
* Inodes: **3,886**, unchanged. Extracted entries: **3,887**, including root and
  one hardlink alias. Regular-file paths: **2,596**. Exactly one file differs.
* `init.rc`: **69 byte positions** differ, only lines 85 and 87.
* Inode 799, fragment 14, expanded fragment 122,555 bytes, file offset 85,888.
* Compressed slot: filesystem `[0x3980e2,0x3a3d2a)`, **48,200 bytes**.
* Whole-image differing byte count: **48,015**.
* Changed erase blocks: rootfs-relative **28,29**; capture-absolute **357,358**.
* Full erase footprint, including all unchanged bytes around the fragment:
  **262,144 bytes**, capture `[0x2ca0000,0x2ce0000)`.
* The entire remaining 90 MiB allocation, including bytes after `bytes_used`,
  is preserved. No fill assumptions (`00` or `ff`) are used.

| SHA-256 | Value |
|---|---|
| Original filesystem | `717041d874bba6a16cda6578101ab1b7e1ff7737ade1b0921b77c9e4e65f6170` |
| Candidate filesystem | `1d7b26012a9feed017439b030c1965315e39464d2044e98c50aa3ef9017d3594` |
| Original init.rc | `2b2a189751d3a2d7c9c5dcfba55da2ab5374cc3d0d1bbc03e809db1289442d14` |
| Candidate init.rc | `329259c33cdcdba7ad064333e962ebaee3997928eddf23545e3bdea3dc7485e3` |
| Original full 90 MiB allocation | `373b1bd10c9064a5d5c107eb56fa6559cf2a8b5a07118065f3a53ed18a8b2951` |
| Candidate full allocation | `192db5d541416892e3f90a039264f2d52d9afd8f0d8c39267fd8b50baf29f676` |
| Original two erase blocks | `005be33d9ebaf5c698ebf26c93e8509c24d6e1e2e775bfb9f8a960b237a093a0` |
| Candidate two erase blocks | `a64b76e8de7bc2c1471dd40668d627d3ddc9726abc0d09be264518e9c7e497e9` |

`candidate/PROPOSAL.json` contains exact per-block offsets, first/last differences,
byte counts, individual hashes and tail hash. The two `changed-blocks-*.bin`
files concatenate complete erase blocks in that manifest's order, **not** a
filesystem image and **not** an automatically approved write plan.
`write_approved=false`, `runtime_verified=false`, and signature status unknown.
