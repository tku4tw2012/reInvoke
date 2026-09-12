---
title: reInvoke documentation
description: Index of the 31 public Markdown pages, grouped by engineering task
ms.date: 2026-09-12
---

Current status and candidate-specific acceptance live in the
[native result ledger](native-nand-platform.md#current-result). Implementation,
RAM measurements and vendor evidence are separate scopes.

## Start here

| Page                                                 | Purpose                                                      |
| ---------------------------------------------------- | ------------------------------------------------------------ |
| [Project overview](../README.md)                     | Goal, architecture, hardware and build boundary              |
| [Product contract](current-product-contract.md)      | Service ownership, policy and interfaces                     |
| [Native NAND platform](native-nand-platform.md)      | Current results, artifact identities and installation limits |
| [Revival roadmap](revival-roadmap.md#remaining-work) | Milestones and remaining engineering gates                   |
| [Security policy](../.github/SECURITY.md)            | Private reporting, sensitive data and repository checks      |
| [Documentation index](README.md)                     | All 31 public Markdown pages                                 |

## Operate and develop

| Page                                                  | Purpose                                      |
| ----------------------------------------------------- | -------------------------------------------- |
| [Wi-Fi provisioning](native-provisioning.md)          | Volatile onboarding and bootstrap trust      |
| [Microphone capture](microphone-capture.md)           | Stream protocol and implemented privacy gate |
| [RAM platform](native-ram-platform.md)                | Host-loaded development/recovery runtime     |
| [U-Boot access](uboot-access.md)                      | USB recovery procedure and observed limits   |
| [Control tools](../tools/control/README.md)           | WAMP clients, adapters and Bluetooth helpers |
| [Kernel tools](../tools/kernel/README.md)             | Kernel/DTB inputs, profiles and checks       |
| [Capture tools](../tools/mic-capture/README.md)       | Capture service and local client             |
| [NAND builders](../tools/nand-pilot/README.md)        | Bootstrap, BSL and bundle composition        |
| [Provisioning tools](../tools/provisioning/README.md) | Parser, network/window services and client   |
| [USB recovery tools](../tools/usb-boot/README.md)     | Host helper and bounded installation tooling |

## Inspect interfaces

The `emulation/` directory includes donor analysis and owned-service evidence,
not exclusively emulated results.

| Page                                                            | Purpose                                          |
| --------------------------------------------------------------- | ------------------------------------------------ |
| [Owned speaker control](emulation/owned-speaker-control.md)     | Mute policy, active-PCM ownership and volume     |
| [MCU boundary](emulation/mcu-boundary.md)                       | I2C, physical inputs and indicators              |
| [DSP boundary](emulation/dsp-boundary.md)                       | Firmware, SPI/GPIO control and message framing   |
| [Bluetooth stack](emulation/bluetooth-stack.md)                 | Donor limitations and replacement media path     |
| [Control-plane emulation](emulation/control-plane-emulation.md) | WAMP execution and vendor control surface        |
| [Boot/update state](emulation/boot-update-state.md)             | Historical markers and unresolved slot selection |

## Trace milestones and sources

| Page                                                                               | Purpose                                               |
| ---------------------------------------------------------------------------------- | ----------------------------------------------------- |
| [Engineering journal](journal.md)                                                  | Closed-unit bring-up and milestone-level history      |
| [NAND decision history](nand-write-decision.md)                                    | Failed trials, withdrawn methods and recovery limits  |
| [Firmware reference](firmware-reference.md)                                        | Retained inputs, image formats and vendor generations |
| [Corpus index](corpus/00_README.md)                                                | Evidence conventions and provenance                   |
| [Hardware baseline](corpus/01_CANONICAL_HARDWARE_BASELINE.md)                      | Sourced hardware facts and open questions             |
| [FCC inventory](corpus/04_FCC_EXHIBIT_INVENTORY.md)                                | Regulatory artifacts and their limits                 |
| [Sibling-source cross-index](corpus/05_SIBLING_SOURCE_CROSSINDEX.md)               | Related platforms and community projects              |
| [Acquisition manifest](acquisition/invoke_berlin_artifact_acquisition_manifest.md) | Source identities and acquisition status              |
| [Storage policy](acquisition/storage-policy.md)                                    | Retention priorities, backup and restore              |

Private evidence locators are not download links. Public acquisition sidecars
do not contain every build manifest or hardware capture.
