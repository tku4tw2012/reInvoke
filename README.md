---
title: reInvoke
description: An open Linux runtime and local-assistant endpoint project for the Harman Kardon Invoke
ms.date: 2026-09-12
---

reInvoke is building a local assistant endpoint on the Harman Kardon Invoke
(`HKINVOKE`, FCC ID `APIHKINVOKE`). Owned Linux services replace the vendor
application stack while reusing the existing compute, audio and controls.
Native startup and Bluetooth playback are demonstrated milestones, not a
complete assistant. Images remain experimental, unit-specific builds.

## Current status

Results from one closed Invoke, as of 2026-09-12:

| Scope                         | Result                                                                                             |
| ----------------------------- | -------------------------------------------------------------------------------------------------- |
| Installed native candidate 03 | Wall-power startup as `reInvoke-NAND`; reported physical pairing and observed host A2DP connection |
| Candidate 03 networking       | Attended Wi-Fi provisioning and ping; pinned Dropbear negotiation, then disconnect before login    |
| Candidate 02 native baseline  | Audible playback, rotary volume, indicators, provisioning and MCU/DSP/WAMP checks                  |
| RAM platform                  | Detailed microphone capture/privacy, speaker safety, firewall and restart measurements             |
| Remaining product work        | Native administration and acceptance, persistent settings and assistant integration                |

Candidate 03 has no native shell or USB enumeration; its SSH failure cause is
unknown. Earlier audio/control and RAM results do not establish acceptance of
the installed image. Wi-Fi credentials, Bluetooth bonds and preferences are
volatile after power loss.

The [native guide](docs/native-nand-platform.md#current-result) owns the result
ledger and artifact pins. See the [product contract](docs/current-product-contract.md),
[remaining work](docs/revival-roadmap.md#remaining-work) and
[documentation index](docs/README.md).

## System overview

The packaged runtime separates media, hardware control, capture and network
ownership. This is software composition, not a board schematic or a native
process dump.

```mermaid
flowchart LR
    NAND["Vendor boot payloads<br/>and owned bootstrap"] --> Init["Owned init<br/>and supervision"]
    Init --> Media["BlueZ and BlueALSA<br/>Bluetooth to ALSA playback"]
    Init --> Control["MCU: I2C controls<br/>DSP: SPI/GPIO control"]
    Init --> Capture["ALSA capture<br/>Privacy gate and local socket"]
    Init --> Network["Wi-Fi and provisioning"]
    Control -. "Privacy state" .-> Capture
    WAMP["Bonefish<br/>compatibility bus"] <--> Control
```

PCM uses ALSA. MCU I2C and DSP SPI/GPIO carry hardware control, not that audio
stream. Bonefish supplies compatibility calls, not authentication or a shell.

## Hardware and firmware

| Fact                                                           | Evidence                                      |
| -------------------------------------------------------------- | --------------------------------------------- |
| Marvell 88DE3006 / BG2CDP Berlin                               | Firmware and hardware corpus                  |
| 512 MiB DRAM                                                   | U-Boot observation                            |
| 256 MiB NAND; 2 KiB pages; 128 KiB erase blocks                | U-Boot, RAM Linux and logical main-data reads |
| Seven microphones, volume ring, touch panel, service Micro-USB | Manufacturer documentation                    |
| SD8887 firmware/calibration                                    | Donor assets and RAM driver bring-up          |

Seven physical microphones do not imply seven raw ALSA channels or verified
beamforming/AEC. The [hardware baseline](docs/corpus/01_CANONICAL_HARDWARE_BASELINE.md)
records unresolved component and signal-path identities.

The original Cortana product, final Bluetooth-oriented `12.2134.0` donor and
reInvoke are distinct systems. The pre-trial unit contained `12.2050.3`.
See the [firmware reference](docs/firmware-reference.md).

## Repository and build boundary

| Path                    | Contents                                                    |
| ----------------------- | ----------------------------------------------------------- |
| [docs/](docs/README.md) | Contracts, operating guides, journal and reference evidence |
| [metadata/](metadata/)  | Public acquisition provenance, sizes and hashes             |
| [tools/](tools/)        | Services, builders, analysis and recovery tools             |
| [patches/](patches/)    | Kernel and BlueALSA patches                                 |

Public Git holds authored material and sanitized metadata; acquired originals
and generated images/captures remain in separate private storage. Acquisition
hashes identify originals, while build manifests identify generated artifacts.
See [storage policy](docs/acquisition/storage-policy.md).

Builders compose pinned donor and RC12 artifacts. A clean public clone lacks
some inputs, sysroots and libraries; deterministic composition is not a complete
reproducible build. See the [build boundary](docs/native-nand-platform.md#build-and-reproducibility-boundary).

## Installation and recovery

> [!CAUTION]
> Native installation erased and reprogrammed the whole good-block set.
> Byte-identical vendor boot payloads do not mean untouched boot-chain regions.
> Recovery worked after observed trials, not arbitrary boot-chain corruption.
> Logical main-data and exposed-OOB captures are not raw restore images.

Use [U-Boot access](docs/uboot-access.md) and the
[NAND installation boundary](docs/native-nand-platform.md#installation-boundary),
not an inferred flash recipe.

## Documentation checks

With Node.js 20.19 or later, run from the repository root:

```bash
npm --prefix scripts ci
npm --prefix scripts test
npm --prefix scripts run validate
```

The checker validates tracked Markdown, relative links/anchors and selected
credential patterns in tracked text. Tests include broken links and synthetic
private-data fixtures. Keep real identifiers in external private rules and
render changed Mermaid diagrams separately. See [coverage and limits](.github/SECURITY.md#repository-checks).

## Licensing and attribution

Authored source, documentation and metadata use [MIT](LICENSE).
[Kernel patches](patches/invoke-kernel/) are GPL-2.0 derivatives;
[BlueALSA patches](patches/bluealsa/) retain MIT attribution.
Vendor evidence retains its original terms.

Firmware inputs came from [coggy9/HKHacking releases](https://github.com/coggy9/HKHacking/releases).
Public availability does not grant redistribution rights, and forks do not
preserve release assets. Custom deployment images remain private.

BlueALSA 4.0.0, BlueZ 5.55, SBC 2.0 and D-Bus 1.12.20 provenance is recorded
in [P1-045](metadata/P1-045.json); [Dropbear provenance](docs/native-nand-platform.md#offline-ssh-implementation-milestone)
is separate. Image distributors must assess each dependency's license
obligations. Report vulnerabilities through the [security policy](.github/SECURITY.md).
