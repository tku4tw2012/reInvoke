---
title: Native NAND platform
description: Candidate 03 startup and flash timing, candidate 02 acceptance, private build prerequisites, and remaining gaps
ms.date: 2026-09-12
ms.topic: overview
---

## Current result

### Candidate 03 startup

Candidate 03 is installed. On 2026-09-12 at 12:10:02 UTC, a fresh Bluetooth
remote-name request to the known unit returned `reInvoke-NAND`; a fresh inquiry
also found it. Candidate 02 had answered `reInvoke-RAM`. The owner had performed
a normal wall-power-only start with USB disconnected, then reconnected USB.
The recovery helper was stopped before that start; no RAM Linux was loaded.
This is a verified-on-this-unit, image-dependent startup indicator. It is not
full acceptance of candidate 03's audio, controls, network, or administration.

USB still did not enumerate in the observed window. A strictly host-pinned SSH
attempt at the previous LAN address could not reach it; Wi-Fi configuration is
volatile and had not been provisioned again. That is not evidence that the SSH
server itself failed. Initial Bluetooth checks also returned no response;
the later positive query supersedes them without erasing that history. The
exact boot duration remains unknown.

The owner subsequently reported that the top swirl was off. This is a
physical startup-indicator observation, not acoustic or microphone acceptance.

The first live one-shot wrapper installed the selected 63,352,864-byte bundle
in 36.819 seconds including artifact checks, staging, vendor erase/program/read
verification, a fresh returned U-Boot response, and helper cleanup. From command
submission to verified program/read coverage was 32.440 seconds, not pure NAND
programming time. All nine record address sets, 2,046 good blocks, known bad
blocks, and exact transfer length matched. No image 99, automatic retry, or
independent Linux readback was used.

The initial helper attachment encountered a USB transition during the owner's
recovery sequence and exited before reaching U-Boot. No flash command was sent
in that attempt. Attaching again to the now-stable downloader reached U-Boot
without another physical request. Both attempts remain in the private
`evidence/native03-flash-20260912T1155Z/` record, alongside
`programmer/RESULT.json` and `NATIVE-STARTUP.json`.

### Candidate 02 functional baseline

reInvoke candidate 02 starts from NAND after an ordinary wall-power cycle,
without a host supplying firmware. Verified behavior on the closed Invoke
includes:

* Bluetooth discovery, encrypted pairing, A2DP playback, and physical rotary
  volume control
* Mic-Mute indicator control and the top-button indication
* Physical entry into the isolated Wi-Fi provisioning window
* Authenticated transfer of a local Wi-Fi profile and subsequent local-network
  reachability
* MCU status `000116`, DSP version event `25688`, and volume calls through the
  WAMP compatibility bus

On 2026-09-12 from 02:43:39 to 02:44:54 UTC, all eight automated RawSocket
WAMP groups passed on port 9999:

* Unknown procedures and malformed arguments were rejected without changing state.
* Five fresh MCU sessions and three fresh DSP version events `25688` passed.
* Media volume `44 -> 43 -> 44` passed event and readback checks.
* Music mute toggled and was restored; music volume `44`, unmuted, and system
  volume `70` matched the baseline afterward.
* Ten fresh sessions completed the stability check.

At 02:46:14 UTC, port 9998 also passed an actual WebSocket WAMP handshake
using `wamp.2.msgpack`, MCU `000116` and volume `44` reads, and unknown-procedure
rejection. This supersedes the earlier evidence of an open TCP port only.
The private evidence record is
`evidence/reinvoke-native-02-fullflash-20260911/WAMP-WEBSOCKET-OVERNIGHT.json`
under the external archive.

These checks used no helper, reboot, playback, or microphone-state change.
No heartbeat was seen during the 75-second observation, so native heartbeat
delivery is not claimed.

Native USB enumeration and ADB are not working. Wi-Fi configuration and
Bluetooth bonds remain volatile and must be established again after power loss.
The native microphone capture and privacy data path has not yet repeated the
accepted RAM-platform measurements. Mic-Mute red on/off and the blue top-tap
feedback are indicator observations, not proof of that data path.

The helper was off before the owner's power-only start and supplied no
firmware afterward. Provisioning was absent before the physical Mic-Mute long
press and present after it. Public summaries omit live network bindings,
derived identifiers, addresses, and private configuration.

The [current product contract](current-product-contract.md) defines the
normative behavior. The [NAND decision history](nand-write-decision.md) retains
the successful and failed experiments that led here.

## Installed architecture

The installed image deliberately keeps the vendor's native 12.2134.0
bootloaders, TrustZone image, and encrypted kernel containers byte-identical.
The owned changes are:

1. A read-only SquashFS bootstrap that starts the reInvoke runtime from NAND.
2. A paired BSL launcher that verifies the selected bootstrap and runtime.
3. The owned MCU, DSP, Bluetooth, audio, microphone, network, provisioning,
   logging, and compatibility services.
4. Optional USB diagnostics. Missing USB, PTY, or ADB prerequisites are
   recorded but no longer prevent the core runtime from starting.

The complete vendor-format bundle also includes the vendor-default app seed.
Generated firmware packages remain private. This architecture is established
by image composition and native functional observations, not a shell dump of
the running native kernel, PID 1, or mounts; those remain unread.

## Build and reproducibility boundary

The source is under [tools/nand-pilot](../tools/nand-pilot/). Build outputs must
remain in the sibling private archive.

The commands below record the candidate 02 composition interface at source
checkpoint `68fe376`, not a promise that a later candidate 03 builder has the
same defaults or inputs. Use the source revision declared by the intended
artifact's manifest rather than silently rebuilding an old identity with new
source. They compose an image from an already prepared private archive.
They require the accepted RC12 artifacts, pinned donor files, toolchains,
and declared source revisions required by the builders. Public acquisition
sidecars do not supply all of those inputs. Deterministic composition from
held inputs is verified; a complete one-command rebuild from a public fresh
clone is not. Full reconstruction of every C dependency also remains open,
as detailed in the [product contract](current-product-contract.md#build-reproducibility).

Build the main rootfs:

```bash
tools/nand-pilot/build.sh <archive> <main-output>
```

Build a BSL launcher bound to that exact main image, then compact it without
changing its loadable code or data:

```bash
fakeroot node tools/nand-pilot/build-bsl.js \
  <archive> <bsl-output> <main-output>

fakeroot node tools/nand-pilot/compact-bsl.js \
  <archive> <compact-bsl-output> <bsl-output>
```

Create the complete vendor-format bundle:

```bash
node tools/nand-pilot/native-bundle.js \
  <archive> <main-output> <compact-bsl-output> <bundle-output>
```

The builders validate pinned source artifacts, deterministic rootfs output,
ARM executable dependencies, paired main/BSL hashes, and vendor-record CRCs.
Candidate 02 also passed a separate container/allocation check and a deliberately
corrupted CRC control. That recorded check is not automatically run by each
builder invocation.

These checks establish deterministic composition and structure from the held
inputs. They do not prove native
boot or authorize flashing.

## Installation boundary

The demonstrated installation used the vendor unified-image programmer after
an explicit owner approval. On this unit it:

* Erased all 2,046 good NAND blocks
* Skipped the two known bad blocks
* Programmed and read back every listed record
* Installed all eight pre-bootloader copies
* Returned an interactive U-Boot prompt after reporting success

The previously installed candidate 02 artifacts are:

| Artifact | Bytes | SHA-256 |
|---|---:|---|
| Complete vendor `l2nand 83` bundle | 62,812,192 | `80cc1e2f17f284f161333e31bfc50d3574c0677b233b06b653474254f3b7679b` |
| Main rootfs | 40,271,872 | `0540003370cd9473e54c193cb5e30ea642689bf5269bae50cad55f995c0be0a1` |
| Paired compact BSL v3 | 2,449,408 | `79b3d880f230d3bb205dd5a4828859952d3a2f39bfd73c06b4e65447fbb99c5e` |

All nine vendor record write/read loops were covered, including the eight
pre-bootloader copies. No independent Linux readback was performed between
this flash and the first native power boot. Earlier independent reads belong
to earlier installations, not candidate 02.

That process is not a sparse update. It erases unlisted state, including
settings and boot-status data, and replaces the app filesystem with the bundle's
default seed. The logical backups are not a programmer-grade raw restore image.

> [!CAUTION]
> Never treat a built bundle or acknowledgement string as write approval.
> Image 99 is excluded. No NAND operation, retry, reset, reboot, push, or release
> is implicit in the build commands above.

## Recovery

The open-source host helper can attach to the observed Marvell downloader state
without opening the enclosure. It supplies a recovery U-Boot and can load the
known RAM Linux environment. That recovery path has remained usable after the
observed writes.

Recovery Linux is a new host-supplied boot. It cannot retrieve volatile logs
from a failed native session and is not proof that the NAND image ran.
See [U-Boot access](uboot-access.md) for the exact distinction and evidence
limits.

During development, leave USB attached to passive capture with the helper
stopped. The first 03 power-only test deliberately excluded host firmware;
unplugging USB is not a demonstrated prerequisite for normal boot. Whether
USB power/presence itself affects the unit's boot behavior remains unknown.
Reserve USB-disconnected starts for explicit host-independence checks rather
than discarding early USB observations on every iteration.

## Current operating model

The useful native product does not require ADB:

* Bluetooth carries audio.
* The physical controls implement local volume, pairing, privacy policy, and
  provisioning; native microphone data-path acceptance remains separate.
* WAMP provides the compatibility control surface for registered MCU and DSP
  operations.

ADB or a separately authenticated administrative channel is still valuable for
kernel logs, processes, mounts, USB diagnostics, and bounded maintenance.
WAMP is unauthenticated and is not an arbitrary command shell.

Default network ADB on TCP 5555 explicitly returned `ECONNREFUSED` while WAMP
was reachable. Port 5037 belongs to the host ADB server; port 8141 is the host
USB-boot helper's console. USB ADB itself has no IP port. None of these is a
working native shell on candidate 02.

## Remaining work

Native administration, native microphone data-path acceptance, and persistent
user settings remain open. Candidate 03 has a startup indicator, not a native
SSH login or a repeat of candidate 02's broader functional acceptance.

### Offline successor appendix: candidate 03

This appendix preserves candidate 03's offline build and review history.
The selected image was subsequently installed and produced the limited
[native startup evidence](#candidate-03-startup) above. Its implemented changes
have a small scope and are not part of candidate 02's demonstrated behavior:

1. Use consistent NAND-specific product identity.
2. Repair native USB ADB with bounded prerequisite retry and accurate health
   reporting, without blocking the working runtime.
3. Add a key-authenticated Wi-Fi administrative fallback.
4. Keep deployment-specific peers, keys, network identifiers, and secrets
   outside the public source tree.

The offline image sets Bluetooth name `reInvoke-NAND`, USB product
`reInvoke-NAND-03`, and build identity `reInvoke-NAND-03-20260912`.
The Bluetooth name was subsequently observed live. USB and build-file
identities remain packaged facts, not native shell observations.

#### Offline USB and status implementation milestone

The candidate 03 implementation uses an asynchronous, finite 30-attempt
prerequisite retry. It validates the legacy USB misc node's kernel-reported
major/minor, root ownership, and mode `0600`, and checks whether `adbd`
actually holds the USB file descriptor rather than equating process liveness
with USB readiness. Matching uses the validated character-device major/minor,
not a procfs pathname that can differ through the `/runtime` bind mount.
Procfs-read errors leave readiness unknown rather than restarting a
potentially healthy daemon; only a known wrong or missing USB descriptor
triggers retries. An already-matching gadget is left configured.

One supervisor controls the early/runtime ADB child handoff. BSL fallback
resurrection is removed, and degraded PTY support no longer gates USB setup.
These changes must not be described as successful native USB enumeration:
the first candidate 03 observation still found no USB device.

The successor status surface reports bounded fixed facts and redacted named
failures. It accepts no arbitrary arguments and emits no raw command-line,
mount-table, log, serial, environment, or private-path dumps. Private pairing/AP
settings and the existing restrictive operator CIDR policy are retained; the
public summary does not expose their values.

The status command compares actual character-device numbers rather than
trusting an ADB-looking procfs pathname. It reports the Berlin-specific gadget
and PHY configuration names when the kernel exposes its configuration.
Neither a dangling link nor a mismatched device number is transport readiness.

#### Offline SSH implementation milestone

The successor selects separately built upstream Dropbear `2026.94`, rather
than the vendor's dynamically linked hard-float Dropbear `2016.72`.
The selected source archive has SHA-256
`e098034a843699200c8c977a991fff73159735bf795d5f72ef672c41a6b1ae81`.
The offline build uses the existing GCC 11 toolchain for static ARM output;
server password authentication is compiled out in favor of public-key-only
authentication.
The final static Dropbear binary is 720,224 bytes, SHA-256
`fe24f709341c568fa422959530f799b0c91cc215a937f3572d6c8d3ab381299c`.

Host-loopback tests under QEMU verified an authorized key, password rejection,
unlisted-key rejection, and strict host-key pinning. `DROPBEAR_REEXEC=0`
avoids an `execveat` dependency unavailable on the old kernel/emulator.
These are offline authentication tests, not native-device login acceptance.

The successor policy requires the TCP 22 firewall before starting the listener,
using the private WAMP CIDR allowlist. SSH is independent of USB ADB; the
successor does not add a TCP `adbd` fallback. This is an implementation
milestone, not an observed listener or successful native login on candidate
02 or 03.

Unique ED25519 host and operator credentials are generated offline and kept
in the private archive. The private image carries the server's host key at `/etc/native-admin/host-key`
with mode `0600`, inside a mode-`0700` directory. It authorizes the operator's
public key at `/root/.ssh/authorized_keys` with mode `0600` inside the
mode-`0700` SSH directory, not the operator's private key.
Host-key material, complete deployment images,
and private configuration remain outside Git and must not be published.
No live address, CIDR, USB path, or key is part of this public summary.

Keys are dedicated to this installation, not a shared fleet key or the
operator's personal SSH identity. Offline tests did not access personal
`~/.ssh` material. A future approved client uses its dedicated key and private
`known_hosts`/`HostKeyAlias` pinning with `StrictHostKeyChecking=yes`; do not
disable host verification or copy
the unit-specific fingerprint into public documentation.

#### Final-build checkpoint

At build completion, the definitive artifact set had the qualification
`OFFLINE_CANDIDATE_NOT_NATIVE_BOOT_VERIFIED` and build identity
`reInvoke-NAND-03-20260912`. Use **only `complete/MANIFEST.json`** under
`<archive>/build/artifacts/reinvoke-native-03-20260912/` to select the final
bundle. Independent file hashing, size checks, the component manifests, and
`OFFLINE-EVIDENCE.json` agree with these pins:

| Selected candidate 03 artifact | Bytes | SHA-256 |
|---|---:|---|
| `complete/83_IMAGE.reinvoke-03` | 63,352,864 | `8a26ac4e2160802fb0a5451c8d856bb7d70177e70a07991326ac65c4c159988f` |
| `main/rootfs.squashfs` | 40,841,216 | `c813311f07f89eafa06c12b5c7f6e3ef245aeced9809497d8b56ae2d32b3fce5` |
| `bsl-compact/bsl.squashfs` | 2,420,736 | `027eee4136d8f193b682f0b3f18a964e283ae11dd642955c5215bec704b47d22` |

The final main double-build is byte-identical and passes full extraction,
metadata, and ARM library checks. Independent bundle extraction verifies all
nine records and their CRC/SHA values, with vendor payloads unchanged except
rootfs and BSL, and correct auxiliary `07_IMAGE` length. Source hashes match
the finalized code; Markdown remains excluded. Nineteen original audio,
control, and provisioning binaries and the private deployment settings are
preserved.

Offline ARM shell controls cover bind-mounted USB descriptor identity, wrong
descriptors, unknown procfs health without reset, degraded PTY support, single
supervisor ownership, finite retry, and invalid payloads. SSH key acceptance
and password/unlisted-key/host-pin negatives passed under host-loopback QEMU.
Neither suite establishes native boot, kernel gadget/PTY/entropy/netfilter,
Wi-Fi, or loop-device behavior on the vendor kernel.

The private evidence root contains `OFFLINE-EVIDENCE.json`, `EXECUTION.txt`,
`verification.log`, and `verify-final.cjs`. Unit-specific key fingerprints and
configuration remain in the private handoff, not this public guide. The
`intermediate` directory is retired from candidate selection, not deleted.
At that offline checkpoint candidate 02 was untouched; no device operation,
kernel build, flash, or image 99 operation had been performed. The later
approved installation and its narrower startup evidence are recorded above,
not retroactively inserted into the preserved offline evidence files.

##### Preserved intermediate build history

The first code freeze started the two-build main rootfs, paired BSL, compact
BSL, and complete `83_IMAGE` pipeline. Markdown is excluded from the code-source
manifest. The private output root is
`<archive>/build/artifacts/reinvoke-native-03-20260912/`, with `main`, `bsl`,
`bsl-compact`, and `complete` subdirectories.

The initial main rootfs built byte-identically twice and passed extraction
checks. This is an **intermediate** artifact, not the final frozen-input pin:

| Intermediate candidate 03 artifact | Bytes | SHA-256 |
|---|---:|---|
| Initial main rootfs | 40,837,120 | `a20725a7bdf74fad50e50a9bb8c6c7bfeda3616c39ff3968b62469a4ab19c2b6` |

The paired BSL build then correctly rejected growth beyond its historical
fixed read-only loop length `0x02680000`. That failed build is part of the
record; the successful main build did not establish a complete usable bundle.
The freeze was reopened only for an owned BSL-helper correction: derive the
bounded loop length from the selected image while keeping the fixed NAND
offset, `O_RDONLY`, `LO_FLAGS_READ_ONLY`, allocation guard, and ioctl
set/get verification. The helper already used static GCC 11; this is not a
kernel change.

The corrected helper subsequently compiled and passed extraction checks.
The intermediate compact BSL is 2,420,736 bytes, below the vendor BSL's
2,715,648 bytes. The intermediate complete-image inventory retains nine
records with only rootfs and BSL changed.

The source manifest now includes C sources. The main was rebuilt to bind the
final pair to that finalized helper source. Earlier results remain in the
private output's `intermediate` directory; final paths remain `main`, `bsl`,
`bsl-compact`, and `complete`.

The resulting offline pipeline reports pair identity/extraction, all vendor
hashes and nine-record CRC/SHA checks, correct auxiliary `07_IMAGE` length,
private configuration preservation, and absence of the operator private key
from the image. Fifteen original core runtime binaries remain byte-identical.
The BSL helper's compiled bound is 40,894,464 bytes at fixed offset
`0x02920000`, guarded at compile time to stay within the 90 MiB allocation.
This is a bounded read-only view, not a partition redesign. Kernel/module
binaries and the existing stock/RC12 release dispatch are unchanged.
The helper has not been executed on the device.

The first final-pin handoff disagreed with an independent read of the artifact
files, manifests, and `OFFLINE-EVIDENCE.json`. Final self-check then found two
additional USB edge cases before any device operation: procfs path matching
was wrong for a bind-mounted descriptor, and procfs-read failures could
restart a healthy daemon. Character-device identity matching and
unknown-state preservation correct those cases. Added Go/ARM regressions
passed in 15 seconds.

The final bundle was rebuilt with those required corrections. The definitive
table above supersedes all earlier handoffs; the initial main hash and
conflicting first final-pin report remain historical, not installation pins.
SSH binary/configuration and the core runtime were not changed by that
correction. Final file, manifest, and evidence checks resolved the handoff
mismatch without any device action or native boot claim.

Integration review then found that the status utility still used pathname-only
FD matching, although the supervisor already used actual device identity.
The status path was corrected too, with a failing dangling-link/device-number
control and checks for the actual Berlin kernel configuration names. A final
paired rebuild completed at 03:41:34 UTC. The preceding `ffc3029a` bundle,
`b1ec5a25` main rootfs, and `24d95443` compact BSL are preserved under
`intermediate/before-status-device-identity-review/`, not selected for flashing.
The complete hashes in the final table above identify the reviewed artifacts.

One final startup dependency was corrected before selecting this bundle. SSH
starts before the WAMP initialization that creates the conventional xtables
module links. Its firewall command now explicitly sets the packaged
`XTABLES_LIBDIR`, so key-authenticated administration does not depend on that
later setup. A harmless ARM `iptables -m tcp -h` control fails without the
extension path and loads the TCP plugin with it; no firewall rules or device
state were changed by that test. The paired rebuild completed at 04:02:07 UTC.
The preceding `e8f77373` bundle remains in
`intermediate/before-ssh-plugin-path-review/`; the selected table above contains
the corrected image's hashes.

#### Offline installation wrapper

The separate [one-command host wrapper](../tools/usb-boot/README.md#offline-tested-native-flash-wrapper)
defaults to regular-file inspection. Its 30 focused tests, including archived
candidate 02 bundle/transcript checks and USB-binding/log-snapshot controls,
passed. The subsequent candidate 03 installation was its first successful live
use, with the measured program/read and cleanup result recorded above. Any
future invocation still requires explicit approval and an
already-ready recovery helper; it neither starts that helper nor crosses the
owner's power gate. One successful installation does not establish behavior
for different devices, layouts, or all failure/recovery cases.

Persistence for Wi-Fi profiles, Bluetooth bonds, and preferences is deferred.
It needs an explicit storage, power-loss, and update-preservation design because
the demonstrated vendor installation erases the whole good-block set.

Acceptance remains appropriate for a personal DIY project: deterministic
builds, targeted failure controls, one owner-controlled power boot, and a
bounded automated service check. It is not a certification campaign.
