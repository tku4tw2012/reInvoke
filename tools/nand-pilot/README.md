---
title: Offline NAND image builders
description: Native candidate composition, private inputs and verification boundaries
ms.date: 2026-09-12
ms.topic: how-to
---

These builders create regular files from private RC12, vendor, BSL, compiler
and QEMU inputs. They do not discover hardware, mount filesystems or flash.
`PROPOSAL.json` carries `authorization: false`. Deterministic composition from
held artifacts is not a complete fresh-clone rebuild.

Candidate 03 is installed; its power-only startup, Bluetooth connection,
provisioning and SSH-listener results are recorded in the
[native NAND guide](../../docs/native-nand-platform.md). SSH authentication,
USB/ADB and native microphone acceptance remain unverified. Candidate 02's
broader acoustic/control acceptance is not transferred to 03.

> [!CAUTION]
> The bundle's vendor `l2nand 83` path erases all good blocks before programming
> listed records. Omitted factory, `fw_stat`, app or tail content is not
> preserved. Retained bootloader/TrustZone/encrypted-kernel bytes are
> reprogrammed, not untouched NAND regions. `99_IMAGE` is an excluded vendor
> filename, never a reInvoke image number.

## Composition

The main artifact is a read-only gzip SquashFS containing `/init`,
`/sbin/init -> /init`, unchanged reviewed BusyBox, RC12 `adbd-root` and its
soft-float loader closure, status/identity files and the compressed owned
runtime. Its bootstrap:

1. Mounts RAM/pseudo filesystems and records bounded startup evidence.
2. Attempts USB/ADB diagnostics without making gadget or PTY availability a
   runtime prerequisite.
3. Verifies payload size/SHA-256 and gzip, then streams cpio with pipe-failure
   propagation into a 160 MiB tmpfs; files occupy about 71 MiB.
4. Exposes runtime devices/proc/sys/run/PTYs, keeps source read-only at
   `/nand-source`, and read-only binds release/manifests.
5. Executes `busybox chroot /runtime /bin/busybox sh /init` as PID 1 rather
   than assuming `switch_root` can dispose of a SquashFS root.

ADB's single supervisor handles early/runtime child handoff when available.
Candidate 03 bounds asynchronous prerequisite retries to 30, checks legacy
misc-node metadata and actual open USB character-device major/minor,
preserves a matching gadget, and does not restart a potentially healthy daemon
on unknown procfs-read results. It removes BSL fallback resurrection.
These are implementation properties, not proof of native enumeration.

`/run` and scratch tmpfs are capped at 16 MiB each; `/dev` at 4 MiB.
Runtime logs rotate at 256 KiB with one backup, bootstrap logs at 32 KiB with
one backup. Fatal payload/bootstrap errors remain stopped and record bounded
failures; already initialized early ADB can continue. Caps do not prove
adequate free RAM under every workload.

## Kernel, storage and identity rules

Accepted releases are exactly `3.8.13-yocto-standard` and
`3.8.13-reinvoke-audio-sd8887`, each with its own gated `mlan`, `sd8xxx` and
`bt8xxx` modules. No release globs, forced vermagic, fallback modules or ignored
required-module failures are used. Unsupported releases stop runtime dispatch.
Stock built-in audio inventory does not prove card numbering or equivalent
capture/playback behavior.

The builder retains stock firmware/calibration/transmit-power/Bluetooth
parameters and the private RC12 pairing/AP contract. Settings and bonds remain
volatile; no persistence allocation or new station credentials are added.
The runtime does not mount other NAND allocations and removes ordinary
writable MTD nodes. It bundles no reInvoke installer, vendor `flash_custk`,
OTA/autoflash or automatic storage writer. Unchanged BusyBox still has generic
writer applets, including `nandwrite`; root can bypass policy or recreate
nodes. This is not a sandbox against arbitrary root code.

Status is available at `/usr/sbin/reinvoke-status` (also `/usr/bin`) with
default or `--json` output. Candidate 03 emits bounded facts/redacted named
failures, not command-line, mount, environment, serial or private-path dumps;
arbitrary arguments are rejected.

The strongest source label is `nand-squashfs-observed-unattested`: actual
read-only SquashFS, block major 31, sysfs `mtdblock` identity and NAND type are
required. Loop major 7 fails that classification even when NAND-backed.
Exact physical extent requires host attestation, and process presence is not
service health. Native shell output from this status command is not observed.
Identity files are excluded from the internal component manifest to avoid
self-reference; outer manifests cover them. No image embeds its own hash.

## Main, BSL and bundle pairing

`build-bsl.js` accepts an optional selected main directory, verifies that
rootfs and embeds its init/runtime hashes. `compact-bsl.js` accepts the matching
BSL directory and removes only permitted ELF debug/section-table material,
checking program-header/loadable-code equivalence and extracted-tree metadata.
`native-bundle.js` combines the main/compact-BSL pair with fixed native
12.2134.0 payloads and the complete vendor app seed.

Candidate 03's final BSL helper exposes a read-only 40,894,464-byte bound from
fixed offset `0x02920000`, with allocation/ioctl checks. This is within the
existing 90 MiB rootfs allocation `[0x02920000,0x08320000)`, not a layout
change. The helper's native execution has not been traced through a shell.
An older BSL bound rejected an oversized main; pair by exact artifact hashes,
not similar name or size.

Use final `complete/MANIFEST.json`, never a retired intermediate. Candidate 03
outputs are `83_IMAGE.reinvoke-03`, `07_IMAGE.for-83` and `MANIFEST.json`.
Build identity is `reInvoke-NAND-03-20260912`; Bluetooth/USB product names are
`reInvoke-NAND`/`reInvoke-NAND-03`. Only Bluetooth identity was observed
natively. Immutable `OFFLINE_CANDIDATE_NOT_NATIVE_BOOT_VERIFIED` records
build-time qualification, not current installation status.
The default main output is
`<archive>/build/artifacts/reinvoke-native-03-20260912/main`.

## Private build inputs

`PILOT_PRIVATE_CONFIG` names an existing private JSON file:

| Field             | Requirement                                                      |
| ----------------- | ---------------------------------------------------------------- |
| `authorizedKey`   | Path to one plain ED25519 operator public key; comments stripped |
| `sshHostKey`      | Unique server private host-key path, no group/other permissions  |
| `sshBinary`       | Reviewed static ARM Dropbear multicall path                      |
| `sshBinarySHA256` | Exact lowercase SHA-256                                          |
| `sshLicense`      | Retained Dropbear LICENSE with companion dependency notices      |
| `sshCIDRs`        | One to eight restricted explicit IPv4 `/24`-`/32` entries        |

Optional `runtimeConfig`, `apSSID` and `apPSK` name private files.
Optional `wifiMAC` supplies a deliberate private address; absent it, vendor
identity is retained. The image includes the server host key and operator
public key, never the operator private key. Keys are installed `0600`,
private directories `0700`; notices remain packaged.

Dropbear disables password/PAM authentication, TCP/agent/X11 forwarding and
SFTP. `ssh-start.sh` installs port-22 filtering before the listener.
Offline QEMU authentication controls passed; native authentication did not.
Account/NSS/toolchain incompatibility remains a hypothesis. There is no network
ADB fallback. Clients use dedicated private `known_hosts`/`HostKeyAlias`
pinning and `StrictHostKeyChecking=yes`.

## Offline build

Run from the repository root against the intended frozen revision, with fresh,
distinct private outputs:

```bash
bash tools/nand-pilot/build-ssh.sh "${REINVOKE_ARCHIVE}" "<new-ssh-output>"
PILOT_PRIVATE_CONFIG="<private-config.json>" \
  bash tools/nand-pilot/build.sh "${REINVOKE_ARCHIVE}" "<new-main-output>"
fakeroot node tools/nand-pilot/build-bsl.js \
  "${REINVOKE_ARCHIVE}" "<new-bsl-output>" "<new-main-output>"
fakeroot node tools/nand-pilot/compact-bsl.js \
  "${REINVOKE_ARCHIVE}" "<new-compact-output>" "<new-bsl-output>"
node tools/nand-pilot/native-bundle.js \
  "${REINVOKE_ARCHIVE}" "<new-main-output>" "<new-compact-output>" "<new-complete-output>"
```

Before the main build, update the private configuration to the reviewed new
SSH binary and digest; `build-ssh.sh` does not do so automatically.
Outputs are private mode-`0700` trees. Build limits use `GOMAXPROCS=2`,
Go `-p 1`, `make -j1`, single-processor SquashFS and `nice -n 10` where
applicable. This pipeline performs no kernel build or hardware operation.

## Offline verification and historical boundary

The pipeline double-builds the main SquashFS and independently extracts it,
checking bytes, ownership, modes, symlinks, hardlinks and device metadata.
GNU find/stat provide authoritative fakeroot metadata; Node stat bypasses it.
ELF interpreters and dependencies must resolve within the correct loader
family. QEMU checks loaders/commands without starting daemons; packaged ARM
BusyBox extracts and verifies the runtime. Negative controls cover corrupt
payload/cpio, pins, module selection, read-only paths and false NAND origins.
The complete bundle checks all nine records' CRC/SHA and manifest agreement.

`verify-artifacts.js <main-output>` independently checks `payload.bin`
(SquashFS plus `ff` erase padding), `rollback.bin` (the original capture slice,
not repacked stock), changed-block hashes and adjacent blocks.
Those bounded rootfs-only proposal semantics must not be confused with the
later vendor whole-good-block erase.

Earlier pilot/BSL images passed RAM-assisted execution or readback without
host-independent startup. Their failed boot experiments and withdrawn write
methods are consolidated in the
[NAND decision](../../docs/nand-write-decision.md#withdrawn-methods).
The [flash wrapper](../usb-boot/README.md#offline-tested-native-flash-wrapper)
consumes the final bundle manifest; programming verification remains separate
from native runtime acceptance.
