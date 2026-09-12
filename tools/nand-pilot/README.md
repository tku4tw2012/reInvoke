---
title: Offline self-contained NAND pilot
description: Reproducible RC12-derived rootfs proposal with independent diagnostics and exact rollback
ms.date: 2026-09-11
---

## Scope and acceptance boundary

Candidate 02 removes the NAND bootstrap's compulsory USB/ADB dependency.
Missing gadget support, failed setup, missing PTYs and an exited early daemon
produce explicit diagnostic failures, but the caller continues to payload
verification and the owned runtime. Working USB retains the early-to-runtime
ADB supervisor handoff and the ADB-only fallback when ACM is unavailable.
The base RC12 RAM image is unchanged. This fixes a source-level startup blocker;
it does not establish the cause of the separate StockRoot boot result or prove
native execution of candidate 02.

The BSL builder accepts an optional main-pilot artifact directory. It verifies
that artifact's rootfs and embeds the selected init/runtime hashes in a
read-only checksum file, rather than retaining pilot-01 target hashes. The
existing read-only loop helper still has a 38.5 MiB bound; an oversized pilot
is rejected. Pair a candidate-02 rootfs with a BSL built against that same
artifact, not an older launcher.

The compact BSL builder can likewise consume a new BSL artifact directory,
while retaining its existing load-equivalence and extracted-tree checks.
[native-bundle.js](native-bundle.js) combines a selected main/compact-BSL pair
with the unchanged native 12.2134.0 payloads and complete vendor app seed.
That bundle is for the vendor programming path, whose observed whole-good-block
erase is a different, broader operation than a bounded Linux code update.
The builder does not authorize that operation.

Current result: this complete pilot was installed, independently read back, and
executed from a read-only NAND view through a USB-loaded custom kernel and RAM
bridge on September 10. PID 1 reached its runtime and repeated real ADB shell
checks passed. The owner's normal host-independent power-on still remained at
the Marvell `FF` downloader. Leave the image installed; see
[NAND startup status](../../docs/nand-write-decision.md) for evidence and the
remaining boot-chain question. The commands below reproduce an artifact, not
permission to flash.

The subsequent vendor-stack attempt reported a whole-chip good-block erase
and failed normal-boot acceptance with spinning lights and no observed USB.
Yellow-mode U-Boot and RAM ADB survived; a fresh full rootfs readback still
matches this pilot. The new [BSL launcher](bsl-init.sh) is a forward variation
for the separate 5 MiB recovery-startup allocation, not a restoration or another
rootfs rewrite. Its [builder](build-bsl.js) retains the existing runtime in NAND,
adds the known coldplug/ADB dependencies, and checks its complete extracted
filesystem against the build tree. USB/ADB diagnostics are still attempted when
available, but their absence no longer withholds NAND rootfs handoff. The status
binary remains in the main runtime; the small launcher reports its own early
state in `/run/reinvoke-bsl`. Cold execution of that variation has not yet been
demonstrated.

The owner-approved BSL v2 installation subsequently passed full writer checks
and independent readback. Its 39 changed blocks were written header-last,
without changing the main rootfs, adjacent blocks or pre-bootloader prefix.
Normal-boot acceptance remains separate; the new BSL bytes being exact does
not establish that the boot chain selects them.

The attended BSL v2 normal-boot test returned the Marvell FF downloader, not
BSL/pilot ADB. The helper then recovered a usable U-Boot console without another
physical reset. The variation changed the observed boot result but did not
reach its intended diagnostic/runtime interface.

The [compact BSL builder](compact-bsl.js) derives a size-only variation from the
actual BSL v2 readback. It removes debug sections in libc and libpthread, while
[checking program headers and loadable code/data](elf-load-check.js), allowing
only ELF section-table bookkeeping fields to differ within the loaded header.
All other files and metadata, including the startup script, remain unchanged.
The filesystem is 2,445,312 bytes instead of 5,111,808, below the vendor BSL
payload's 2,715,648 bytes. The fixed `nandbslcompact` write changed 32 blocks
inside the same 5 MiB allocation and passed independent readback. Its first
normal boot and a second connected-USB early-boot observation both returned
the FF downloader without BSL/pilot ADB. Reducing size did not resolve the
observed failure; the actual failing boot stage remains unknown.

This pilot packages the actual RC12 reInvoke services, not a startup marker.
The builder operates on regular archive files only. It does not discover a
device, run ADB, mount a filesystem, program NAND, reboot, or authorize a writer.
`PROPOSAL.json` explicitly carries `authorization: false`.

Rootfs allocation is `[0x02920000,0x08320000)`, with 128 KiB erase blocks and
2,048-byte pages. Only the erase-rounded candidate extent is proposed. Remaining
old rootfs bytes beyond that extent are untouched, as are `app`, boot selectors,
kernels, factory data and every other allocation.

Normal NAND entry and stock-kernel execution remain unproven. In particular,
rootfs-only replacement does not establish that persistent FF/FF downloader
selection is fixed. The corrected custom kernel remains RAM-only. An owner
power cycle without a host RAM download, followed by external observations and
readback, is still necessary for acceptance.

## Boot architecture

1. A read-only gzip SquashFS contains `/init`, `/sbin/init -> /init`, a static
   BusyBox, retained RC12 `adbd-root` with its soft-float loader closure, the
   status tool, immutable identity, and the complete compressed runtime.
2. PID 1 mounts only RAM/pseudo filesystems. It records entry mount, kernel,
   command-line and boot-ID evidence in RAM. A real SquashFS source stays
   read-only.
3. Before payload verification or hardware-heavy initialization, the bootstrap
   tries the running kernel's legacy Android USB gadget and misc-device ABI,
   configures product `reInvoke-NAND-pilot-02`, and starts an independent ADB
   restart loop when the diagnostic path is available. Missing gadget, PTY or
   early daemon failures are recorded and degraded, but they no longer block
   payload verification, extraction or the core runtime launch. A connected
   host is not required to proceed.
   The fresh device tree includes `/dev/ptmx`, character device `5:2`, mode
   `0666`. Its metadata preflight still runs before ADB starts. Process
   liveness alone does not prove PTY allocation or a working shell. The actual
   ARM allocation test and reviewer closure passed before the write; normal
   NAND-startup shell availability has not been observed.
4. The bootstrap checks the embedded payload's size and SHA-256, validates gzip,
   then extracts cpio with pipe-failure propagation into a 160 MiB-capacity
   tmpfs. The actual runtime files occupy about 71 MiB. It does not materialize
   a second uncompressed archive in RAM.
5. RAM devices, proc, sys, run, scratch storage and PTYs are exposed inside the
   runtime. The source root is retained at `/nand-source` as a read-only bind.
   Release/manifest files are read-only binds from that source.
6. PID 1 uses `exec busybox chroot /runtime /bin/busybox sh /init`. It does not
   assume `switch_root` can dispose of a real SquashFS root. ADB supervision
   remains outside the runtime service lifecycle and restarts its daemon inside
   the new root once ready.
7. The RC12 init functions and service commands are reused through an exact
   source-hash/context-checked patch. Microphone/DSP/MCU, privacy, mute-first
   shutdown, Bluetooth, playback ownership, WAMP filtering, provisioning and
   network service binaries remain unchanged.

`/run` is capped at 16 MiB, `/tmp` at 16 MiB and `/dev` at 4 MiB. The original
runtime logger rotates at 256 KiB with one backup. Bootstrap logs rotate at
32 KiB with one backup; failure records retain one bounded message per subsystem.
Fatal bootstrap failures print to console/kernel log and remain stopped rather
than reporting a successful boot. Early ADB continues when already initialized.
These memory ceilings are not proof that the stock kernel has adequate free RAM
for every service workload.

## Kernel and network policy

Only `3.8.13-yocto-standard` and `3.8.13-reinvoke-audio-sd8887` are accepted.
Each has exactly its own `mlan`, `sd8xxx` and `bt8xxx` modules, verified against
the original source and vermagic. There are no release globs, fallback modules,
forced vermagic or ignored insertion failures. Stock inventory records the
Berlin ASoC, WM8904 and ALSA loopback built-ins; this does not prove equivalent
audio behavior, sound-card numbering, DSP access or microphone functionality.

The stock startup's firmware, calibration, transmit power and Bluetooth
parameters are retained. RC12's `mlan` interface naming, locally administered MAC
and STA/uAP mode are explicit choices for its existing provisioning contract.
An unsupported kernel or failed required module load still stops runtime
dispatch. An unavailable gadget ABI is a recorded diagnostic degradation, not
a claim that USB works and not a reason to withhold the other services.

Private RC12 runtime/AP configuration is retained inside the private archive and
candidate, never copied into Git. The first pilot does not mount a persistence
partition or embed new station credentials. The existing physical-button
provisioning flow can supply station configuration for that boot. Wi-Fi
credentials, Bluetooth bonds and subsequent configuration changes are RAM-only;
automatic station connectivity across power cycles is not claimed.

## BusyBox and diagnostic tools

The static BusyBox binary is exactly the reviewed RC12 original, SHA-256
`5fc83ab6cd37841b8d73e07bf3cd8af47ae5af56c93fe085b2db91e0d1f4207b`.
Its generic storage applets, including `nandwrite`, remain available. There are
no binary dispatch patches or claims that root's writer capabilities have been
removed. `busybox-provenance.json` records unchanged source and packaged hashes.

No reInvoke installer, `flash_custk`, OTA/autoflash script or automatic
storage-writing startup command is bundled. Ordinary writable MTD nodes are
removed by the retained RC12 startup policy. The source root stays read-only;
the runtime does not mount other NAND allocations.

Dormant recovery init entry points are replaced. Unused inherited vendor
utilities with missing soft-float dependencies are removed instead of linking
them to incompatible hard-float libraries. All owned RC12 runtime binaries,
firmware and private configuration remain byte-identical.

Working BusyBox applets are linked into PATH, and `/bin/sh` directly invokes
BusyBox rather than the obsolete recovery profile. This is not a security
sandbox against arbitrary root code: root still has ordinary diagnostic tools,
can create device nodes and can bypass mount policy. ADB is deliberately an
unauthenticated root diagnostic interface; restrict physical USB access.

## Identity and status

`/usr/sbin/reinvoke-status` and `/usr/sbin/reinvoke-status --json` report the
following. `/usr/bin/reinvoke-status` remains available as the executable target.

* Build/release identity and source/component manifest hashes
* Actual running kernel, boot ID, command line and mount table
* Bootstrap entry evidence, boot phase and retained failures
* Service/supervisor PIDs, process presence and executable paths
* Network-persistence policy and outstanding acceptance requirements

The maximum positive source classification is
`nand-squashfs-observed-unattested`. It requires an actual read-only SquashFS
mount, MTD block major 31, a sysfs-confirmed `mtdblock` device, and NAND type.
A RAM/loop-mounted rehearsal cannot pass merely because its compiled product
name contains NAND. The exact physical source extent still needs host
attestation. Process presence is explicitly not functional service health.

The September 10 on-device experiment used a read-only loop backed by NAND at
`0x02920000`, not a regular image file. The classifier still returned
`non-nand-squashfs-unattested` because loop major 7 is not MTD major 31.
Separate host records verified the backing device, bounds, payload hashes,
PID 1 handoff and real runtime shell. Do not change the label to make that
experiment appear to prove host-independent boot.

`/etc/reinvoke-release` and `/etc/nand-pilot` are read-only binds during runtime.
The component manifest excludes those identity files to avoid self-reference;
the independent outer artifact manifests cover their final contents. No image
SHA is embedded within the image it would identify.

## Rebuild and validate offline

Use the existing sibling archive and its Go 1.18/ARM-emulation tools. The default
output is `archive/build/artifacts/reinvoke-nand-pilot-01-20260909/`. Existing
completed outputs are never overwritten. Each output directory is mode `0700`
because both images and extracted trees contain private RC12 configuration.

```bash
bash tools/nand-pilot/build.sh ../reinvoke-archive \
  ../reinvoke-archive/build/artifacts/reinvoke-nand-pilot-01-independent-rebuild

node tools/nand-pilot/verify-artifacts.js \
  ../reinvoke-archive/build/artifacts/reinvoke-nand-pilot-01-independent-rebuild
```

The script applies `nice 10`, `GOMAXPROCS=2`, Go `-p 1`, and single-processor
SquashFS operations. Work directories are within the chosen archive output.
No new package or Python environment is needed.

The build verifies all three pinned inputs, freshly extracts and builds twice,
and requires identical complete SquashFS bytes. Each result is independently
extracted; file content, ownership, modes, symlink targets, hardlink groups and
device metadata must match. GNU find/stat supply authoritative fakeroot
metadata because Node stat bypasses fakeroot. Every packaged ELF interpreter
and `DT_NEEDED` dependency must resolve within its declared loader family.
QEMU checks the retained ADB loader and active dynamically linked runtime
commands without executing daemons. Each build also extracts the entire runtime
using the packaged ARM BusyBox and checks every byte, mode and link. ARM BusyBox
tests cover payload corruption,
bad cpio, input pins, module selection, read-only paths, unchanged BusyBox and
shell syntax. Go tests include false NAND-origin controls.

`payload.bin` contains the complete SquashFS plus `0xff` padding to the exact
erase boundary. `rollback.bin` is the original capture slice for that same
extent, not a re-packed stock filesystem. `verify-artifacts.js` independently
checks byte correspondence, source-capture equality, padding, every proposed
changed-block hash and both adjacent-block hashes. `PROPOSAL.json` carries the
exact current pins and tool versions; build logs, extraction, loader, module,
source and validation manifests are retained beside it.
