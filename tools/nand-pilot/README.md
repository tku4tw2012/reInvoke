---
title: Offline NAND image builders
description: Native candidate composition, private inputs and verification boundaries
---

These builders create regular files from private RC12, vendor, BSL, compiler
and QEMU inputs. They do not discover hardware, mount filesystems or flash.
`PROPOSAL.json` carries `authorization: false`. Deterministic composition from
held artifacts is not a complete fresh-clone rebuild.

The builder produces the current `2.2.x` series, which boots from NAND
unattended and serves a root SSH login and USB ADB from a cold boot. Results
are recorded in the [native NAND guide](../../docs/native-nand-platform.md)
and the evidence behind them in
[release validation](../../docs/release-validation.md). Persisted settings and
station resume, corrected local-account SSH lookup and optional bounded TCP
ADB all shipped in this series. Earlier candidate builds are history; the
[decision record](../../docs/journal.md) covers them.

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
parameters and the private RC12 pairing/AP contract.
Candidate 04 mounts only the verified existing app allocation for selected
settings; all ordinary writable MTD nodes remain removed. The persistence
helper can recreate a private node only for the verified app partition.
It bundles no reInvoke installer, vendor `flash_custk`,
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

The BSL helper exposes a read-only, erase-block-rounded view of the selected
main image from fixed offset `0x02920000`, with allocation/ioctl checks. This is within the
existing 90 MiB rootfs allocation `[0x02920000,0x08320000)`, not a layout
change. The helper's native execution has not been traced through a shell.
An older BSL bound rejected an oversized main; pair by exact artifact hashes,
not similar name or size.

Use final `complete/MANIFEST.json`, never a retired intermediate. Candidate 04
outputs are `83_IMAGE.reinvoke-04`, `07_IMAGE.for-83` and `MANIFEST.json`.
Build identity is `reInvoke-NAND-04-20260912`; Bluetooth/USB names are
`reInvoke-NAND-04`. Builders reject main/BSL manifests from a different
candidate. Immutable `OFFLINE_CANDIDATE_NOT_NATIVE_BOOT_VERIFIED` records
build-time qualification, not current installation status.
The default main output is
`<archive>/build/artifacts/reinvoke-native-04-20260912/main`.

## Private build inputs

### Optional voice candidate

The installed `2.2.11` composition patches a pinned RC12 `/init`; changing the
RAM init source alone does not change this image. These optional environment
inputs add the same voice bundle/startup hooks to that composition:

* `PILOT_VOICE_DONOR_BUNDLE` names the private unchanged donor bundle accepted
  by `tools/voice-endpoint/build.sh`.
* `PILOT_VOICE_CONFIG` names the private opaque voice JSON, mode `0600`.
  Supply it together with the donor bundle.
* `PILOT_HOSTS_FILE` names an optional private complete hosts seed.
  It can also be used without voice.
* `PILOT_BUILD_ID` sets the explicit candidate identity, for example
  `reInvoke-2.3.0-20260928`.

A voice build rejects the installed `2.2.11` identity. Without an override,
ordinary non-voice builds retain their existing identity; nothing here changes
the accepted-build record. The owner-approved next build is `2.3.0`, pending
complete-firmware boot verification. Earlier private iterations, including
ones labelled `2.3.1`, were experiments rather than approved releases. Their
files are retained separately and must not be confused with the approved build.

Use the current [voice endpoint configuration](../voice-endpoint/README.md)
without the obsolete `post_ms` field, which the endpoint rejects. The packager
copies it unchanged; backend controls determine capture and turn completion.
For a distinct offline voice candidate, reuse the existing private base inputs
and give the output a matching date:

```bash
export PILOT_PRIVATE_CONFIG="<private-base-build-config.json>"
export PILOT_PERSISTENCE_CONFIG="<private-base-persistence-config.json>"
export PILOT_BLUEDROID_CONFIG="<private-base-bluedroid-config.json>"
export PILOT_BUILD_ID="reInvoke-2.3.0-20260928"
export PILOT_VOICE_DONOR_BUNDLE="<private-donor-voice-bundle>"
export PILOT_VOICE_CONFIG="<private-voice.json>"
export PILOT_HOSTS_FILE="<private-hosts-seed>"
tools/nand-pilot/build.sh "${REINVOKE_ARCHIVE}" \
  "<private-output>/reinvoke-2.3.0-20260928-approved/main"
```

The default identity in [build-lib.js](build-lib.js) remains `2.2.11`.
[build.sh](build.sh), [build-bsl.js](build-bsl.js),
[compact-bsl.js](compact-bsl.js) and [native-bundle.js](native-bundle.js) all
inherit it. Keep `PILOT_BUILD_ID` exported for every candidate composition step.
This builds regular files only, not an installation. The rootfs allocation
remains 90 MiB and the runtime tmpfs 160 MiB; measure the completed candidate
rather than assuming its fit or device memory use. No donor voice libraries
are installed into the global or existing audio loader families.

Voice builds supplement the RC12 lights with checksum-gated original
`L_101_c_listening.bin`, `L_104_c_thinking.bin` and
`L_105_c_cortanaspeaking.bin`, taken from
`<archive>/extracted/phase3/stockroot/rootfs/usr/share/lights/`.
The three files are copied unchanged to `/opt/reinvoke/share/lights`; the
composer records their sizes and hashes in `voice-lights-manifest.json`.
Non-voice builds do not read this additional source.

The config is installed at `/etc/reinvoke-voice/voice.json` with mode `0600`.
Hosts remain in the existing RAM `/etc/tmpfs/hosts`, reached through
`/etc/hosts`. The new hook reads an existing regular `/persist/hosts` after the
existing persistence service prepares storage; it does not create or change
that persistent file. Creating or updating the override is a separate NAND
write, not authorized by this build. Voice starts after the capture owner and
is stopped before MCU/capture/DSP/router teardown. See the
[RAM build options](../usb-boot/README.md#optional-personal-voice-endpoint)
for isolation flags and synthetic host-only integration tests.

### Existing base inputs

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
Candidate 04 sets account NSS databases to `files`: the extracted 03 root
reproduced a pre-authentication libc-loader abort with `compat`, and the fixed
root authenticated and executed the packaged ARM shell. This is not yet a
native login result. Clients use dedicated private `known_hosts`/`HostKeyAlias`
pinning and `StrictHostKeyChecking=yes`.

Optional `adbNetwork` is `{enabled:true, windowSeconds:300, peers:["<IPv4>/32"]}`.
Exactly one private RFC1918 peer is accepted; 300 seconds is the maximum.
Default is disabled. A host-configured gadget with an open USB descriptor is
preserved. Otherwise TCP takes over the sole ADB owner, installs filtering
before listening, and closes sessions at expiry.
ADB is unencrypted root access without SSH authentication.

`PILOT_PERSISTENCE_CONFIG` identifies the pinned helper, changed applyd and
MCU binaries, plus an optional private Wi-Fi seed.
[Persistence](persistence/README.md) defines its schema, saved-profile
precedence, 30-second snapshot window and failed-storage behavior.

## Offline build

Run from the repository root against the intended frozen revision, with fresh,
distinct private outputs:

```bash
PILOT_PRIVATE_CONFIG="<private-config.json>" \
PILOT_PERSISTENCE_CONFIG="<private-persistence-artifacts.json>" \
  bash tools/nand-pilot/build.sh "${REINVOKE_ARCHIVE}" "<new-main-output>"
fakeroot node tools/nand-pilot/build-bsl.js \
  "${REINVOKE_ARCHIVE}" "<new-bsl-output>" "<new-main-output>"
fakeroot node tools/nand-pilot/compact-bsl.js \
  "${REINVOKE_ARCHIVE}" "<new-compact-output>" "<new-bsl-output>"
node tools/nand-pilot/native-bundle.js \
  "${REINVOKE_ARCHIVE}" "<new-main-output>" "<new-compact-output>" "<new-complete-output>"
```

Candidate 04 reuses the reviewed Dropbear binary. If rebuilding it with
`build-ssh.sh`, update its private path and digest explicitly.
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

`verify-artifacts.js <main-output> <archive>` independently checks `payload.bin`
(SquashFS plus `ff` erase padding), `rollback.bin` (the original capture slice,
not repacked stock), changed-block hashes and adjacent blocks.
Those bounded rootfs-only proposal semantics must not be confused with the
later vendor whole-good-block erase.

Run the admin regressions against the composed runtime:

```bash
node tools/nand-pilot/ssh-test.js "${REINVOKE_ARCHIVE}" \
  "<main-output>/build-a/runtime" "<new-private-ssh-evidence>"
node tools/nand-pilot/adb-network-test.js "${REINVOKE_ARCHIVE}" \
  "<new-private-adb-evidence>" "<main-output>/build-a/runtime"
```

Earlier pilot/BSL images passed RAM-assisted execution or readback without
host-independent startup. Their failed boot experiments and withdrawn write
methods are consolidated in the
[NAND decision](../../docs/nand-write-decision.md#withdrawn-methods).
The [flash wrapper](../usb-boot/README.md#offline-tested-native-flash-wrapper)
consumes the final bundle manifest; programming verification remains separate
from native runtime acceptance.
