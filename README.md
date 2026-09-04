# reInvoke

Preservation and hardware-research corpus for the **Harman Kardon Invoke**
(`HKINVOKE`, FCC ID `APIHKINVOKE`) and its Marvell 88DE3006 (BG2CDP) "Berlin" platform.

## What this repository is

This repository holds the **small, durable, high-value layer**: research documents,
evidence ledgers, acquisition manifests, provenance metadata with cryptographic
hashes, and the extracted configuration/script layer of the firmware bundles.

It deliberately does **not** contain the multi-gigabyte binary payloads. Those are
preserved outside Git. See [Storage policy](docs/acquisition/storage-policy.md).

The governing rule, inherited from the corpus methodology:

> A claim must be traceable to evidence, or it remains explicitly unknown or hypothetical.

## Status and next steps

See **[PLAN.md](PLAN.md)** for current state, established hardware facts, and the
analysis work queued next.

## Layout

```text
reInvoke/
├── docs/
│   ├── corpus/            Hardware baseline, claim/evidence ledger, FCC inventory, cross-index
│   ├── acquisition/       Artifact manifest, retention ranking, storage policy
│   ├── bundle-contents/   Extracted text layer + full listings of firmware bundles
│   ├── emulation/         Running the device's own userland off-device
│   └── journal.md         Dated record of work, findings, and corrections
├── metadata/              Provenance sidecars: source URL, UTC time, SHA-256, size
└── tools/                 Acquisition tooling
```

## Notable results

The device's control plane is a WAMP message bus routed by `bonefish`, an
open-source router that ships in the firmware. Because the router and every
service are ordinary ARM executables, the whole control plane runs on a
workstation under emulation, and a third-party client can call its procedures
and change state. See [control-plane emulation](docs/emulation/control-plane-emulation.md).

Harman's final firmware, `Barracuda_libre-12.2134.0`, removes Cortana and
Spotify and adds a Wi-Fi blocker, converting the product into a local
Bluetooth speaker. The repurposed device the project set out to describe was,
in part, shipped by the vendor. See
[OTA2 analysis](docs/bundle-contents/invoke-ota2/ota2-analysis.md).

## Three-tier storage model

| Tier | Contents | Location |
|---|---|---|
| 1 | Docs, metadata, hashes, extracted text layer | **This repository** (~480 KB) |
| 2 | Firmware bundles (569 MB) | [GitHub Releases](../../releases) + Azure cold storage |
| 3 | Full working set incl. Git mirrors (4.9 GB) | Private Azure Blob container |

Every artifact held outside Git is indexed here by SHA-256 in [`metadata/`](metadata),
so this repository remains the authoritative catalogue of the whole archive.

## Why the split

The bulk payloads are already-compressed firmware images and are effectively
incompressible: recompressing them yields ~0%, and Git packing barely helps while
making the bytes permanent in history. Meanwhile the *engineering meaning*
concentrates in a tiny fraction of the bytes.

A worked example — `docs/bundle-contents/invoke-flashing/marvell_flash_tool/gen-cmd.sh`
is 596 bytes and yields the device's complete NAND partition map, its serial console
parameters (`ttyS0,115200`), and a recovery boot path. Those partitions sum to exactly
**512 MB**, establishing the flash size by derivation.

That single file is worth more to reverse engineering than the 569 MB it shipped
alongside — which is precisely why the split exists.

## Provenance and integrity

Originals are preserved **byte-for-byte** and are never repacked or recompressed.
The SHA-256 values recorded in [`metadata/`](metadata) at acquisition time are the
integrity anchor for every artifact.

Note that `83_IMAGE` exists in two distinct variants of identical length
(107,934,810 bytes) but different SHA-256: the standalone `StockRoot` release asset
is a patched variant, not a duplicate of the copy inside the flashing bundle.

## Safety

Acquisition tooling is authorized only to download, mirror, archive, hash, extract,
index, and document. **Nothing here should be executed or flashed to a device.**
Firmware images are retained as research evidence, not as a distribution channel.

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
| `tools/`, `docs/` research and analysis written for this project | MIT |
| `metadata/` provenance sidecars authored here | MIT |
| `patches/invoke-kernel/` | GPL-2.0 — derivative of the Linux kernel |
| `patches/bluealsa/` | MIT — derivative of BlueALSA, which is MIT |
| `docs/bundle-contents/` extracted vendor text, scripts, drivers, PDFs | Proprietary, Harman International |

Adding an MIT licence cannot relicense material this project does not own.
The vendor-derived and GPL-derived paths above are included as research
evidence under their own terms.

### Build-time dependencies

The runtime image is built against upstream projects that are **not**
redistributed by this repository. No binaries are committed here; only build
instructions, patches, and recorded checksums. Anyone reproducing the build
fetches these sources themselves.

| Dependency | Version | Licence |
|---|---|---|
| [BlueALSA](https://github.com/arkq/bluez-alsa) | 4.0.0 | MIT |
| [BlueZ](https://www.bluez.org/) | 5.55 | GPL-2.0-or-later (daemon), LGPL-2.1-or-later (libraries) |
| [SBC](https://www.kernel.org/pub/linux/bluetooth/) | 2.0 | GPL-2.0-or-later |
| [D-Bus](https://dbus.freedesktop.org/) | 1.12.20 | AFL-2.1 OR GPL-2.0-or-later |

Recorded URLs, checksums, and build flags for each are in
[metadata/P1-045.json](metadata/P1-045.json).

`patches/bluealsa/` applies to BlueALSA, which is MIT, so the patch is MIT and
retains upstream copyright. The copyleft dependencies are used unmodified at
build time and reached over D-Bus at runtime; because this repository conveys
no binary built from them, their distribution obligations are not triggered
here. They would apply to anyone who chooses to distribute a built image.

### Firmware mirror attribution

The Invoke firmware published under [Releases](../../releases) was obtained from the
**[coggy9/HKHacking](https://github.com/coggy9/HKHacking)** project, which originally
made these bundles available as GitHub release assets. Full credit for locating and
publishing that material belongs to that project and its contributors.

It is mirrored here for preservation. Release assets are not archived by Software
Heritage, are not captured by the Wayback Machine, and are not copied by repository
forks — so a single account deleting a release would remove the only public copy.
This mirror exists to prevent that.

The firmware itself remains the property of Harman International. It is retained and
republished as research and preservation material, not as a vendor distribution
channel, and carries no warranty. Rights holders may request removal.
