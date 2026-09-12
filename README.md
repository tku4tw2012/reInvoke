---
title: reInvoke
description: Native replacement platform for the Harman Kardon Invoke
ms.date: 2026-09-12
---

Preservation, hardware-research, and owned-runtime project for the **Harman
Kardon Invoke** (`HKINVOKE`, FCC ID `APIHKINVOKE`) and its Marvell 88DE3006
(BG2CDP) "Berlin" platform.

## What this repository is

This repository holds the **small, durable, high-value layer**: the current
reInvoke implementation and contract, research documents, evidence ledgers,
acquisition manifests, provenance metadata with cryptographic hashes, and the
extracted configuration/script layer of the firmware bundles.

It deliberately does **not** contain the multi-gigabyte binary payloads. Those are
preserved outside Git. See [Storage policy](docs/acquisition/storage-policy.md).

The governing rule, inherited from the corpus methodology:

> A claim must be traceable to evidence, or it remains explicitly unknown or hypothetical.

## Product generations

Three systems appear in this repository and must not be conflated:

1. the **2017 retail Invoke**, preserved as Cortana-era historical evidence;
2. Harman's **2021 final Bluetooth firmware**, version `12.2134.0`, used as a
   vendor comparison and donor source; and
3. the **reInvoke target**, an owned Linux runtime that now starts from NAND,
   with RAM recovery retained for development and repair.

Read the
**[current product and architecture contract](docs/current-product-contract.md)**
before treating older research notes as current behavior. See
**[PLAN.md](PLAN.md)** for completion status and remaining acceptance gates.

## Current reInvoke architecture

The current native runtime uses:

* a read-only NAND bootstrap and paired BSL launcher that enter the owned
  runtime without requiring USB diagnostics;
* `reinvoke-mcu-interface` for MCU input, LEDs, speaker mute/power safety, rotary
  volume, and the public compatibility Mic-Mute API;
* `reinvoke-dsp-interface` for DSP loading, SPI/GPIO/reset, seven public DSP
  WAMP procedures, and a root-only mode-`0600` microphone-control socket;
* the RAM-validated microphone privacy design with restart reconciliation,
  fail-safe remute, and a protected red indication; native data-path acceptance
  is still open;
* BlueZ 5.55 and patched BlueALSA 4.0.0 for local A2DP Sink playback; and
* supervised network and authenticated provisioning daemons.

Candidate 02 has started from NAND after a wall-power cycle and demonstrated
Bluetooth pairing, audible playback, rotary volume, physical provisioning, and
local-network MCU/DSP control without host-supplied firmware. Candidate 03 is
now installed and has returned its changed `reInvoke-NAND` Bluetooth name
after an owner-controlled power-only boot. That is a native startup indicator,
not a repeat of all candidate 02 acceptance. Native USB still does not enumerate;
03's key-authenticated SSH fallback awaits network provisioning and a native
login. Persistent settings and native microphone data-path acceptance remain open.
Start with the [native NAND platform](docs/native-nand-platform.md).
The [documentation index](docs/README.md) separates current guides from dated
experiments, vendor evidence, and private operator tooling.

## Layout

```text
reInvoke/
├── docs/
│   ├── corpus/            Hardware baseline, claim/evidence ledger, FCC inventory, cross-index
│   ├── acquisition/       Artifact manifest, retention ranking, storage policy
│   ├── bundle-contents/   Extracted text layer + full listings of firmware bundles
│   ├── emulation/         Donor evidence and current hardware-service boundaries
│   └── journal.md         Dated record of work, findings, and corrections
├── metadata/              Provenance sidecars: source URL, UTC time, SHA-256, size
└── tools/                 Acquisition, offline analysis, runtime builders, and recovery tooling
```

## Notable results

The device's control plane is a WAMP message bus routed by `bonefish`, an
open-source router that ships in the firmware. Because the router and every
service are ordinary ARM executables, the whole control plane runs on a
workstation under emulation, and a third-party client can call its procedures
and change state. See [control-plane emulation](docs/emulation/control-plane-emulation.md).

Harman's final firmware, `Barracuda_libre-12.2134.0`, removes Cortana and
Spotify and adds a Wi-Fi blocker, converting the vendor product into a local
Bluetooth speaker. That 2021 donor firmware is not reInvoke. See
[OTA2 analysis](docs/bundle-contents/invoke-ota2/ota2-analysis.md) for the
historical evidence and the
[current contract](docs/current-product-contract.md) for the owned target.

## Three-tier storage model

| Tier | Contents | Location |
|---|---|---|
| 1 | Authored source, docs, acquisition metadata, extracted text layer | This repository |
| 2 | Original firmware inputs and retained upstream sources | Private operator-managed archive; original public firmware source is [coggy9/HKHacking releases](https://github.com/coggy9/HKHacking/releases) |
| 3 | Working trees, captures, build products, and deployment manifests | Private operator-managed archive and cold storage |

This repository has **no GitHub releases**, verified through the release list
and API on 2026-09-12 UTC. The custom reInvoke image is not published.
[`metadata/`](metadata) indexes acquired evidence; it is not a complete index
of later private builds and captures. Those use their own private manifests
and evidence summaries.

## Why the split

The bulk payloads are already-compressed firmware images and are effectively
incompressible: recompressing them yields ~0%, and Git packing barely helps while
making the bytes permanent in history. Meanwhile the *engineering meaning*
concentrates in a tiny fraction of the bytes.

A worked example,
`docs/bundle-contents/invoke-flashing/marvell_flash_tool/gen-cmd.sh`, is
596 bytes and yields a generic 512 MiB vendor partition map, serial-console
parameters (`ttyS0,115200`), and a recovery boot path. Live identification and
full logical reads establish that this unit instead has **256 MiB NAND**. The
generic 512 MiB map does not apply to it.

That single file is worth more to reverse engineering than the 569 MB it shipped
alongside — which is precisely why the split exists.

## Provenance and integrity

Originals are preserved **byte-for-byte** and are never repacked or recompressed.
The SHA-256 values recorded in [`metadata/`](metadata) at acquisition time are the
integrity anchors for the corresponding acquired artifacts. Native build
manifests separately bind private inputs, source pins, and generated images.

Note that `83_IMAGE` exists in two distinct variants of identical length
(107,934,810 bytes) but different SHA-256: the standalone `StockRoot` release asset
is a patched variant, not a duplicate of the copy inside the flashing bundle.

## Safety

Acquisition tooling downloads, mirrors, archives, hashes, extracts, indexes,
and documents evidence. Native-image builders operate on regular files and do
not authorize hardware operations. NAND writes require a separately reviewed
scope and explicit owner approval; image 99 remains excluded. The
[U-Boot procedure](docs/uboot-access.md) is the recovery boundary, not ordinary
product startup.

Treat peer addresses, credentials, account names, serial numbers, machine names,
usernames, and host paths as operator-local data. Documentation examples use
placeholders such as `<allowlisted-peer>`, `<archive>`, and `<workspace>`.

## Licensing and attribution

The original work in this repository — documentation, research notes, and the
tooling under [`tools/`](tools) — is released under the [MIT License](LICENSE).

Third-party material is not covered by that licence and retains its own terms.
Material originates from multiple parties under differing terms — Harman, Google/Nest,
Valve, Kinoma, and community researchers. Provenance for each artifact is recorded in
[`metadata/`](metadata), and per-source attribution and status are documented in
[docs/acquisition/source-retention-ranking.md](docs/acquisition/source-retention-ranking.md).

### What MIT covers

| Path | Licence |
|---|---|
| Authored `tools/` source and `docs/` research and analysis, including authored bundle analyses | MIT |
| `metadata/` provenance sidecars authored here | MIT |
| `patches/invoke-kernel/` | GPL-2.0 — derivative of the Linux kernel |
| `patches/bluealsa/` | MIT — derivative of BlueALSA, which is MIT |
| Original vendor text, scripts, drivers, and PDFs in `docs/bundle-contents/` | Retain original vendor terms; not relicensed by this project |

Adding an MIT licence cannot relicense material this project does not own.
The vendor-derived and GPL-derived paths above are included as research
evidence under their own terms.

### Build-time dependencies

The runtime image uses upstream projects and private donor inputs. Generated
firmware images are not committed here. Rebuilding candidate 02 requires the
private accepted RC12 artifacts and declared source pins as well as upstream
sources and toolchains. A public fresh clone is not a one-command, from-source
reproduction of the whole image. See the
[build boundary](docs/native-nand-platform.md#build-and-reproducibility-boundary).

| Dependency | Version | Licence |
|---|---|---|
| [BlueALSA](https://github.com/arkq/bluez-alsa) | 4.0.0 | MIT |
| [BlueZ](https://www.bluez.org/) | 5.55 | GPL-2.0-or-later (daemon), LGPL-2.1-or-later (libraries) |
| [SBC](https://www.kernel.org/pub/linux/bluetooth/) | 2.0 | GPL-2.0-or-later |
| [D-Bus](https://dbus.freedesktop.org/) | 1.12.20 | AFL-2.1 OR GPL-2.0-or-later |

Recorded URLs, checksums, and build flags for each are in
[metadata/P1-045.json](metadata/P1-045.json).
These entries describe candidate 02's media-stack lineage. The offline
candidate 03 SSH dependency and its separate source pin are recorded in the
[successor appendix](docs/native-nand-platform.md#offline-ssh-implementation-milestone);
they are not candidate 02 functionality or a native login acceptance result.

`patches/bluealsa/` applies to BlueALSA, which is MIT, so the patch is MIT and
retains upstream copyright. The copyleft dependencies are used unmodified at
build time and reached over D-Bus at runtime; because this repository conveys
no binary built from them, their distribution obligations are not triggered
here. They would apply to anyone who chooses to distribute a built image.

### Firmware source and publication status

The preserved vendor Invoke firmware was obtained from
[coggy9/HKHacking](https://github.com/coggy9/HKHacking/releases).
Credit for locating and publishing those inputs belongs to that project and
its contributors. This repository does not currently mirror them in releases.

Git mirrors and ordinary repository forks do not preserve release assets.
Private byte-for-byte retention protects the acquired evidence without
claiming that another public custodian or current public mirror exists.
Dated records of earlier upload activity remain historical records, not proof
of present release availability.

Vendor firmware retains its original ownership and licence provenance.
Firmware packages and private deployment material are not to be published by
this project. Public availability elsewhere does not grant redistribution
rights, and the project's MIT licence does not cover vendor firmware.
