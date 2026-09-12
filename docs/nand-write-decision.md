---
title: NAND evidence and write decisions
description: NAND geometry, backup limitations, withdrawn methods, and the native-startup milestones
ms.date: 2026-09-12
ms.topic: reference
---

## Current result

Native startup is established. The [native platform](native-nand-platform.md)
owns installed artifact pins and service acceptance. The evidence here defines
the storage, backup and recovery limits behind that installation.

> [!CAUTION]
> These are engineering decisions and results, not a flash recipe. Recovery
> worked after observed failures, not arbitrary boot-chain corruption.
> Native bundles used `83_IMAGE`. The excluded vendor filename is `99_IMAGE`;
> its damaging mechanism is not established.

## Unit facts to preserve

September 2026 measurements identified 256 MiB NAND, 512 MiB DRAM, NAND ID
`98 DA 90 15 76 16`, 2 KiB pages and 128 KiB erase blocks: 2,048 blocks total.
Linux exposed one unpartitioned `mv_nand` device. Spare-area sizes differ:

| Layer                        | OOB bytes/page | Meaning                                       |
| ---------------------------- | -------------- | --------------------------------------------- |
| Physical part identification | 128            | Upstream ID-prefix match to `TC58NVG1S3H`     |
| Vendor Linux MTD declaration | 64             | Live `MEMGETINFO` and kernel report           |
| Controller-visible window    | 32             | Meaningful returned bytes in Linux and U-Boot |

The package marking was not inspected. Upstream identification specifies
8-bit-per-512-byte ECC; the retained controller source configures 48-bit BCH per
2 KiB. Live ECC layout reports parity at bytes 80-127 and free bytes 2-79,
inconsistent with the 64-byte declaration. A 64-byte OOB request returned
32 meaningful bytes followed by 32 zero bytes, not the hidden parity area.

Eight identical CRC-valid version tables, corroborated by both held multipart
packages, established this end-exclusive allocation map:

| Allocation        | Start        | End          |
| ----------------- | ------------ | ------------ |
| `block0`          | `0x00000000` | `0x00020000` |
| `pre-bootloader`  | `0x00020000` | `0x00120000` |
| `post-bootloader` | `0x00120000` | `0x00320000` |
| `postbootloaderB` | `0x00320000` | `0x00520000` |
| `factory_setting` | `0x00520000` | `0x00a20000` |
| `tz_en`           | `0x00a20000` | `0x00f20000` |
| `tz_en-B`         | `0x00f20000` | `0x01020000` |
| `bootimgs_B`      | `0x01020000` | `0x01a20000` |
| `bsl`             | `0x01a20000` | `0x01f20000` |
| `bootimgs`        | `0x01f20000` | `0x02920000` |
| `rootfs`          | `0x02920000` | `0x08320000` |
| `app`             | `0x08320000` | `0x0fe20000` |
| `fw_stat`         | `0x0fe20000` | `0x0ff20000` |

Physical bad blocks are `0x0c000000` and `0x0c020000`, in `app`.
Mirrored BBTs were found at pages 131008 and 130944 in the remaining tail.
Linux's additional rejected blocks were BBT reservations, not four more defects.
The rootfs allocation is 90 MiB; both boot allocations are 10 MiB.
The early `0x00a20000` "kernel" carve was actually `tz_en`.
Neither the vendor 512 MiB example nor its compact 256 MiB example defines
this unit's layout.

## Backup fidelity

The September 2 capture held exactly 268,435,456 main-data bytes. Its complete
length and hash authenticate saved bytes, not successful ECC on every page.
Two September 7 reads matched in main data and all 4,194,304 exposed OOB bytes,
including `Bbt0`, `1tbB` and their version byte. They are logical captures,
not physical programmer images.

Five pages independently incremented the uncorrectable-ECC counter:

| Page index | Main-data offset |
| ---------- | ---------------- |
| `0x1fc81`  | `0x0fe40800`     |
| `0x1fc86`  | `0x0fe43000`     |
| `0x1fc8a`  | `0x0fe45000`     |
| `0x1fc8b`  | `0x0fe45800`     |
| `0x1fc8c`  | `0x0fe46000`     |

Adjacent controls at `0x0fe40000` and `0x0fe46800` had no failed increment.
The probe used the odd read-only MTD minor and rejected the writable even minor
before MTD ioctls. Repeatable returned bytes did not resolve those errors.

Between the September 2 and 7 captures, only `[0x00660000,0x00680000)` changed:
13,040 non-`0xff` bytes across 14 pages became erased-value `0xff`.
This is factory storage, not spare capacity. The original bytes lacked an
independent reread; capture error, real erase and autonomous early-stage action
remain possible. No retained console record attributes it to an operator write.

Normal and MTD RAW reads returned identical bytes and both incremented ECC
failures: this driver wires `read_page_raw` to the hardware-ECC reader.
RAW mode therefore supplies neither unprocessed main data nor missing physical
OOB. Logical reconstruction regenerates ECC and inherits uncertain source pages.
The separately sampled SPI NOR was not fully backed up.

## Withdrawn methods

The failures below changed the implementation or the interpretation of results.
Detailed payload manifests, traces and per-attempt timing remain in Git history
and the private archive rather than duplicated as a public experiment diary.

### Mapping lifetime and the two-block startup probe

Before any write, removing a temporary MTD mapping disconnected RAM Linux.
The retained `mtdblock_remove_dev` contained a double-free; no crash trace proved
it caused that particular disconnect. The
[cleanup patch](../patches/invoke-kernel/0005-fix-mtdblock-removal-lifetime.patch)
passed immediate/deferred ownership tests, then live mapping creation/removal.
The old-kernel broad mapping writer was withdrawn.

A 69-byte `init.rc` edit to enable ADB changed one compressed fragment spanning
two erase blocks, `[0x02ca0000,0x02ce0000)`: 256 KiB had to be rewritten.
Fragment decoding with the retained inflater, metadata checks and corrected
payload-binding negative controls passed. Installation and exact two-block
restoration each passed independent readback and cleanup, yet ordinary startup
remained persistent FF without diagnostics. Subsequent full main/exposed-OOB
comparison matched September 7, including the five uncertain pages.
Restored logical bytes were not evidence of restored physical OOB or stock boot.

### Startup and filesystem variations

| Experiment                          | Result and engineering consequence                                       |
| ----------------------------------- | ------------------------------------------------------------------------ |
| 38.5 MiB rootfs pilot               | Exact storage readback, but no native diagnostics or identified runtime  |
| Kernel-only and replacement-init    | No early/fallback ADB; failure stage untraced, wholesale init withdrawn  |
| Host-gated RC12 NAND handoff        | Real PID 1/runtime execution, but RAM had already initialized USB/nodes  |
| First eight-record multipart bundle | Vendor success and rootfs match, but no useful native startup            |
| BSL v2 and compact BSL              | Both verified on storage, neither booted usefully; size was not the fix  |
| September 7 stock-plus-ADB rebuild  | Main/OOB reconstruction matched, but no native ADB or responding BT name |
| Matching 12.2134.0 early-ADB update | Files and selected boot bytes persisted; no checkpoint or native shell   |
| Bounded published StockRoot install | No useful modified behavior; did not reproduce its whole-chip procedure  |

The handoff replaced only a hook after proven ADB startup. A legacy BusyBox
loop command silently ignored its requested offset; explicit offset/size ioctls
and readback fixed the mount test. Filesystem execution under a prepared RAM
kernel did not establish cold native startup or native kernel selection.

The [published StockRoot release](https://github.com/coggy9/HKHacking/releases/tag/StockRoot)
was an independently reported custom-firmware success. The local bounded trial
preserved app/status state and used a different programming path from the
published whole-chip method. Its failure did not isolate rootfs contents as
the cause or contradict that community result.

Reconstruction required combined single-page `MEMWRITE` PLACE mode:
2,048 main bytes plus 32 exposed OOB bytes, preserving app tags. RAW, AUTO and
a separate OOB-only write were not equivalents. Independent data/tag checks
were necessary because the legacy driver can lose NAND FAIL status.
This qualified the recorded reconstruction, not a general raw restore.

The first multipart operation exposed two incorrect assumptions: omitting
descriptors did not preserve their regions, and stale `07_IMAGE` length metadata
was not harmless preflight practice. Transport proved the complete transfer;
the vendor erased all 2,046 good blocks. `fw_stat` became all `0xff`;
factory was already all `0xff` in the September 7 reference.

### Unsafe read probes and recovery assumptions

`imls` aborted U-Boot. A later successful 64-byte resident-memory read did not
qualify a 64 KiB `md.b` request, which printed only 1 KiB before a prefetch abort.
The large-dump collector was withdrawn. A host timeout cannot cancel an
already-issued device command, and packaged-image size does not bound resident
memory.

The proposed full-erase recovery test and claims of guaranteed stock-`83_IMAGE`
undo were withdrawn. Avoiding intentional boot-chain writes does not prove
recovery independent of NAND. FE/FF descriptors do not diagnose signature
failure, and negative string scans of opaque loaders prove no such independence.

## Autonomous native milestones

### September 11-12: candidate 02

One nine-record vendor operation completed 2,046 good-block erases and all
program/read loops, then returned a fresh U-Boot response. No RAM Linux
intervened before a helper-free power-only start. The inherited `reInvoke-RAM`
Bluetooth name did not make this a RAM-loaded boot.

Candidate 02 established encrypted Bluetooth/A2DP, attended audible melody,
rotary volume both ways, indicators, physical Wi-Fi provisioning and MCU/DSP
WAMP control. Eight RawSocket test groups covered fresh sessions/events,
state changes and invalid arguments. A real `wamp.2.msgpack` WebSocket
handshake passed; no heartbeat appeared during 75 seconds. Indicators were
not microphone privacy tests.

Several variables changed together: vendor programming, default app seed,
cleared prior state and startup that no longer withheld core services when USB
diagnostics failed. The cause of the breakthrough was not isolated.
Retained vendor bootloader, TrustZone and encrypted kernel payload bytes were
identical, but their NAND blocks were erased/reprogrammed, not untouched.

### September 12: candidate 03

The 63,352,864-byte bundle completed nine address sets, exact transfer length,
the same good/bad-block coverage and a fresh prompt. Wrapper time was
36.819 seconds; command-to-verified-coverage was 32.440 seconds, not pure NAND
write speed. Its power-only start advertised `reInvoke-NAND` and stopped swirling.

Physical Bluetooth connection and Wi-Fi provisioning completed, including
same-socket TLS fingerprint verification, credentials posted without logging,
host network restoration and device ping. TCP22 reached pinned Dropbear but
closed before login; TCP5555 refused. No candidate-03 acoustic/rotary/microphone
acceptance campaign or native shell trace was obtained. Candidate 02's broader
results and RAM safety/firewall tests remain separately scoped.

## Recovery and further work

The [recovery sequence](uboot-access.md#verified-recovery-sequence) worked after
the observed trials, including attachment to an existing FF endpoint.
A recovered RAM shell cannot recover a prior failed session's volatile logs.
Recovery after corrupt `block0`, pre-bootloader or hidden OOB remains unproved.

Future writes require exact artifact/bounds, a fixed per-read ECC policy,
independent readback and explicit stop conditions, not automatic restore/reboot.
Current credentials, bonds and settings are volatile. Native administration,
remaining acceptance, storage design and assistant integration belong to the
[remaining work](revival-roadmap.md#remaining-work), not another speculative
partition recipe.
