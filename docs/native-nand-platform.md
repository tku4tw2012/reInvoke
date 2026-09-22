---
title: Native NAND platform
description: Current native results, artifact identities, installation limits and build provenance
---

## Current result

Build `2.2.11` is installed and runs unattended from NAND on wall power with
no host attached. The boot log shows every supervisor started 34 seconds after
power-on, for seventeen supervised services plus the native SSH and USB ADB
helpers.

In service and observed on the unit: Wi-Fi association from credentials held
in durable storage, root SSH, USB ADB from a cold boot, MCU and DSP control,
microphone capture with a mute gate proven by measurement, the startup chime
and cue set, rotary volume on the donor's measured gain curve, and Bluetooth
A2DP playback.

Bluetooth is runtime-toggled and reports its state in `/run/reinvoke/bluetooth-state`.
An earlier revision of this document reported that the controller never
completed HCI initialisation, leaving `hci0` at version zero. That was three
packaging defects rather than a radio or driver fault, and it is fixed; see
[Bluetooth enable path](bluetooth-enable-path.md). While the stack is off,
`hci0` still reads an all-zero address, which is the stack being disabled and
not the old defect returning.

[Release validation](release-validation.md) records how each result was
observed and which checks only a person can make.

### Earlier native milestones

The two tables below record what was observed on the candidate builds that
preceded the current numbering. They are retained as evidence of when each
capability first worked, not as a description of the running build.
[Version history](versions.md) maps the candidate names to build numbers.

#### Candidate 03 startup

Verified on this unit, 2026-09-12:

| Observation              | Evidence and limit                                                                                                              |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------- |
| Host-independent start   | Wall power, USB disconnected, helper stopped; no RAM Linux loaded after flash                                                   |
| Fresh Bluetooth identity | Query at 12:10:02 UTC returned `reInvoke-NAND`, replacing candidate 02's `reInvoke-RAM`                                         |
| Startup indication       | Owner reported that the top swirl subsequently stopped; exact boot duration unknown                                             |
| Bluetooth connection     | Physical button pairing/connection reported; host A2DP connection observed, without repeated acoustic/rotary tests              |
| Wi-Fi provisioning       | Physical Mic-Mute long press; same-socket TLS fingerprint check, credentials delivered without logging, client profile restored |
| Network reachability     | Invoke joined the local network and answered ping                                                                               |
| SSH                      | TCP 22 reached Dropbear 2026.94 and negotiated the pinned ED25519 identity, then closed at the user-authentication request      |
| Other administration     | No USB enumeration; network ADB TCP 5555 explicitly refused                                                                     |

No native shell or trace was obtained. Account/NSS/toolchain incompatibility
is a hypothesis, not a diagnosis. The actual native kernel, PID 1, mounts,
firewall and PTY behavior remain unread. Provisioning is complete; native
administration is not.

#### Candidate 02 functional baseline

Candidate 02 demonstrated wall-power NAND startup, encrypted Bluetooth
pairing, owner-confirmed audible melody, rotary volume both directions, red
Mic-Mute on/off, blue top-tap feedback, physical provisioning and MCU/DSP calls.

On 2026-09-12, eight automated RawSocket groups passed on TCP 9999:
fresh MCU sessions and DSP events `25688`, volume `44 -> 43 -> 44`,
music mute/restoration, ten-session stability, unknown-procedure and
malformed-argument rejection. Initial music `44` unmuted and system `70`
state was restored. No heartbeat appeared during the 75-second observation.

TCP 9998 also passed an actual `wamp.2.msgpack` WebSocket WAMP handshake,
MCU `000116`/volume reads and unknown-procedure rejection.
These are protocol results, not port scans. Indicator changes do not establish
microphone data-path acceptance, and these tests do not transfer to candidate 03.

## Current operating model

Bluetooth carries audio and WAMP carries compatibility calls. Device ports
9999/9998 are RawSocket/WebSocket; neither is a shell. Host TCP 5037 is the
ADB server and host TCP 8141 is the USB-helper console, not device services.

Wi-Fi credentials and Bluetooth stack configuration are held in `/persist`, a
yaffs2 volume on the `app` partition. The running `wpa_supplicant.conf` is
generated from that store rather than from a fresh provisioning exchange, so
credentials are read from durable storage at boot. Survival across abrupt power
loss has been exercised on earlier builds but not on this one, and is not
listed as verified. User preferences and the update, recovery and reset
semantics for `/persist` are not yet settled. The
[roadmap](revival-roadmap.md#remaining-work) owns remaining gates.

## Installed architecture

Composition retains vendor `12.2134.0` bootloader, TrustZone and encrypted
kernel payloads byte-identically, replacing rootfs/BSL with the paired owned
runtime/bootstrap. The complete bundle includes the vendor-default app seed.

Owned PID 1 starts MCU/DSP, the donor Bluedroid stack, capture, network, provisioning,
logging and compatibility services. Missing USB, PTY or ADB prerequisites do
not gate core startup. These are composition facts, not native shell
introspection. See the [service contract](current-product-contract.md#accepted-runtime-architecture).

## Installation boundary

The vendor operation erased 2,046 good blocks, skipped two known bad blocks
and covered all nine record address sets, including eight pre-bootloader
copies. Transfer length, program/read coverage and a fresh returned U-Boot
response were verified.

| Candidate 03 interval        | Duration | Meaning                                                           |
| ---------------------------- | -------: | ----------------------------------------------------------------- |
| Complete wrapper             | 36.819 s | Validation, staging, vendor operation, verification and cleanup   |
| Command to verified coverage | 32.440 s | Erase/program/read and coverage checks, not pure NAND write speed |

Neither accepted candidate received an independent Linux readback between
flashing and its first native start. Earlier readbacks belong to earlier
installations; vendor program/read coverage is not independent readback.

> [!CAUTION]
> This erased/reprogrammed the whole good-block set, including unlisted
> settings and boot-status data. Byte-identical payloads do not mean untouched
> boot-chain regions. Logical main-data and exposed-OOB backups are not
> programmer-grade raw restores.

The approved native bundle was served as `83_IMAGE`; `l2nand 83` is the
historical vendor command convention. `99_IMAGE` is the exact excluded vendor
filename, not a reInvoke candidate number. The observed wrapper used no
automatic flash retry or `99_IMAGE` operation.
Use [recovery tooling](../tools/usb-boot/README.md), not a recipe inferred here.

## Artifact identities

These are private candidate artifacts, not public release downloads. The
selection record is `complete/MANIFEST.json` in each private output
directory.

Candidate 05.4, built and staged, awaiting an owner-performed flash
(`build/artifacts/reinvoke-native-05.4-20260914/`):

| Component                        | Bytes      | SHA-256                                                            |
| -------------------------------- | ---------: | ------------------------------------------------------------------ |
| `complete/83_IMAGE.reinvoke-05.4` | 64,090,144 | `2ddbba563ca1309a4ce82399844184ac0757f70402fac9935ca8af8e49dee131` |
| `main/rootfs.squashfs`           | 41,578,496 | `73bffed62df26b56ed869a8d5f3354fe7a1a9c3b8c2929183ddd8731b9a963aa` |
| `compact/bsl.squashfs`           |  2,420,736 | `66dced0e54dc469ea0ae30220aede452e71d6a7ad03373e5a7576ea13b91d9de` |

Candidate 03, retained for comparison
(`build/artifacts/reinvoke-native-03-20260912/`):

| Component                       | Bytes      | SHA-256                                                            |
| ------------------------------- | ---------: | ------------------------------------------------------------------ |
| `complete/83_IMAGE.reinvoke-03` | 63,352,864 | `8a26ac4e2160802fb0a5451c8d856bb7d70177e70a07991326ac65c4c159988f` |
| `main/rootfs.squashfs`          | 40,841,216 | `c813311f07f89eafa06c12b5c7f6e3ef245aeced9809497d8b56ae2d32b3fce5` |
| `bsl-compact/bsl.squashfs`      | 2,420,736  | `027eee4136d8f193b682f0b3f18a964e283ae11dd642955c5215bec704b47d22` |

## Build and reproducibility boundary

[NAND builders](../tools/nand-pilot/README.md) compose regular files from
pinned donor inputs, accepted private RC12 artifacts, toolchains and source
revisions. Use the selected manifest's revision, not current builder defaults,
to reproduce an existing identity. Public acquisition sidecars do not supply
every input; some C dependencies require retained sysroots, libraries or tools.

Checks cover deterministic rootfs output, ARM dependencies, paired main/BSL
hashes and vendor-record CRCs. The final candidate-03 build passed double-build,
extraction and metadata checks; independent extraction verified nine records,
CRC/SHA values, unchanged vendor payloads outside rootfs/BSL and the auxiliary
`07_IMAGE` length. Nineteen existing audio/control/provisioning binaries were
preserved. These offline results are narrower than native acceptance or a
complete reproducible build from a clean public clone.

## Implementation notes

Packaged identities for the candidate-03 build were Bluetooth `reInvoke-NAND`,
USB `reInvoke-NAND-03` and build `reInvoke-NAND-03-20260912`. Only the
Bluetooth name was observed live; the others were composition evidence. The
running build derives its USB and Bluetooth identity from the unit's own MAC
instead. See [release validation](release-validation.md).

### USB and status

USB startup retries at most 30 times, checking the legacy misc node's
major/minor, root ownership and mode `0600`. Daemon descriptor checks compare
character-device identity, not bind-mount-sensitive paths.
Known wrong/missing descriptors trigger recovery; unknown procfs health does
not reset a potentially healthy daemon. One supervisor owns early/runtime
handoff, with no BSL fallback resurrection or PTY prerequisite.

The status utility emits bounded facts and redacted named failures, not raw
logs, command lines or environments. Offline controls cover wrong descriptors,
unknown health, finite retries and degraded PTY. They did not produce native
USB enumeration.

### Offline SSH implementation milestone

[build-ssh.sh](../tools/nand-pilot/build-ssh.sh) builds static ARM Dropbear
2026.94 with GCC 11, rather than vendor dynamic hard-float Dropbear 2016.72.

| Artifact                        | Bytes               | SHA-256                                                            |
| ------------------------------- | ------------------: | ------------------------------------------------------------------ |
| Dropbear 2026.94 source archive | See source manifest | `e098034a843699200c8c977a991fff73159735bf795d5f72ef672c41a6b1ae81` |
| Final static binary             | 720,224             | `fe24f709341c568fa422959530f799b0c91cc215a937f3572d6c8d3ab381299c` |

Password/PAM authentication and forwarding are compiled out.
`DROPBEAR_REEXEC=0` avoids an unavailable `execveat` dependency.
Host-loopback QEMU controls passed authorized-key acceptance,
password/unlisted-key rejection and strict host-key pinning. Native login
passed subsequently, once local-file account lookup replaced the failing
pre-authentication path; the listener answers on `0.0.0.0:22` and serves a
root shell on the running build.

[ssh-start.sh](../tools/nand-pilot/ssh-start.sh) requires private host-key,
authorized-key and source-CIDR inputs. It installs TCP-22 firewall policy before
the listener and explicitly sets `XTABLES_LIBDIR=/opt/reinvoke/lib/xtables`,
independently of WAMP startup. It adds no network ADB fallback.

The image contains a dedicated ED25519 host key and the operator's public
authorized key, not the operator's private key. Key files use `0600` inside
`0700` directories. Client pins remain private; do not disable strict host-key
verification. Native source-filter enforcement remains unverified: the policy
is installed before the listener, but no test has confirmed that a connection
from outside the configured CIDR is actually refused on the device.

## Settings, persistence and bounded network ADB

These behaviours were designed for the candidate-04 successor and are in the
shipping build:

* Local-file account lookup fixes the pre-authentication failure that closed
  earlier SSH sessions. The unchanged ARM Dropbear then logs in and executes
  the packaged ARM shell.
* The existing named app/YAFFS2 allocation stores successful Wi-Fi profiles,
  mic mute and volume preferences. No partition
  is added or formatted. Failed storage leaves explicit volatile operation.
* A private derived Wi-Fi seed can initialize a verified empty store.
  Saved profiles take precedence; association precedes durable saving.
* Host-configured USB with an open daemon descriptor is preserved.
  Otherwise optional network ADB replaces the USB owner for one window per
  boot, at most 300 seconds and one private `/32` peer. It is unauthenticated,
  unencrypted root access, not a substitute for SSH's trust model.
* USB startup handles the optional legacy enable node. Status distinguishes
  observed listeners from authentication and firewall acceptance.

The installer erases saved settings on reflash. Across ordinary boots,
abrupt power loss can discard the latest 30 seconds of bond/preference changes;
orderly shutdown flushes after the writers stop.
See [builder inputs](../tools/nand-pilot/README.md) and
[persistence](../tools/nand-pilot/persistence/README.md).

The candidate-04 private build that first carried these is identified by
`complete/MANIFEST.json`:

| Artifact               | Bytes      | SHA-256                                                            |
| ---------------------- | ---------: | ------------------------------------------------------------------ |
| `83_IMAGE.reinvoke-04` | 64,516,128 | `f7920a21e794f72f750e231da103687e8a7c4a03f645373a4d139818ac818689` |
| Main rootfs            | 42,004,480 | `6ee6ff6014715547c21f46b8cd57eef3a0516b5c914545c6e029f38036f00091` |
| Paired compact BSL     | 2,420,736  | `3faae521d19ca69c890cd3acb8252dd374806fc40769d218dcf2a3ded87dd7a9` |

## Recovery

The helper reached the observed Marvell downloader and supplied U-Boot/RAM
Linux after earlier experiments. This does not establish recovery from
arbitrary boot-chain corruption or retrieve a previous boot's volatile logs.

Passive USB observation can leave the cable attached with the helper stopped.
USB-disconnected starts prove host independence, not a universal unplugging
prerequisite; cable-only influence remains unknown.
See [U-Boot access](uboot-access.md) and [NAND history](nand-write-decision.md)
for the recovery path and the unisolated cause of candidate 02's breakthrough.
