---
title: reInvoke documentation
description: Index of the public documentation, grouped by engineering task
---

Current status lives in the [native result ledger](native-nand-platform.md#current-result)
and the evidence behind it in [release validation](release-validation.md).
Implementation, RAM measurements and vendor evidence are separate scopes.

## Start here

| Page                                                 | Purpose                                                      |
| ---------------------------------------------------- | ------------------------------------------------------------ |
| [Project overview](../README.md)                     | Goal, architecture, hardware and build boundary              |
| [Version history](versions.md)                       | Build numbering, and what every earlier name maps to         |
| [Product contract](current-product-contract.md)      | Service ownership, policy and interfaces                     |
| [Native NAND platform](native-nand-platform.md)      | Current results, artifact identities and installation limits |
| [Release validation](release-validation.md)          | What the running build has demonstrated, and how             |
| [Revival roadmap](revival-roadmap.md#remaining-work) | Milestones and remaining engineering gates                   |
| [Security policy](../.github/SECURITY.md)            | Private reporting, sensitive data and repository checks      |

## Operate and develop

| Page                                                              | Purpose                                         |
| ----------------------------------------------------------------- | ----------------------------------------------- |
| [Wi-Fi provisioning](native-provisioning.md)                      | Onboarding, bootstrap trust and durable credentials |
| [Microphone capture](microphone-capture.md)                       | Stream protocol and implemented mic mute gate    |
| [USB ADB](usb-adb.md)                                             | Gadget modules, bring-up and teardown on this unit |
| [USB device mode](usb-device-mode.md)                             | Superseded: the built-in-kernel route, and the hardware evidence |
| [RAM platform](native-ram-platform.md)                            | Host-loaded development/recovery runtime        |
| [U-Boot access](uboot-access.md)                                  | USB recovery procedure and observed limits      |
| [NAND flash procedure](nand-flash-procedure.md)                   | Service-mode entry, the helper, and what fails  |
| [Bluetooth enable path](bluetooth-enable-path.md)                 | Controller hand-off and stack start-up          |
| [MCU command map](mcu-command-map.md)                             | Recovered I2C command and reply shapes          |
| [Footprint baseline](footprint-baseline.md)                       | Measured image and memory cost                  |
| [Control tools](../tools/control/README.md)                       | WAMP clients, adapters and Bluetooth helpers    |
| [Kernel tools](../tools/kernel/README.md)                         | Kernel/DTB inputs, profiles and checks          |
| [Capture tools](../tools/mic-capture/README.md)                   | Capture service and local client                |
| [NAND builders](../tools/nand-pilot/README.md)                    | Bootstrap, BSL and bundle composition           |
| [NAND inspection](../tools/nand-inspect/README.md)                | Read-only container, capture and compare checks |
| [Settings persistence](../tools/nand-pilot/persistence/README.md) | Guarded app storage, Wi-Fi resume and snapshots |
| [Provisioning tools](../tools/provisioning/README.md)             | Parser, network/window services and client      |
| [USB recovery tools](../tools/usb-boot/README.md)                 | Host helper and bounded installation tooling    |

## Inspect interfaces

The `emulation/` directory includes donor analysis and owned-service evidence,
not exclusively emulated results.

| Page                                                            | Purpose                                          |
| --------------------------------------------------------------- | ------------------------------------------------ |
| [Owned speaker control](emulation/owned-speaker-control.md)     | Speaker mute procedures and volume               |
| [MCU boundary](emulation/mcu-boundary.md)                       | I2C, physical inputs and indicators              |
| [Vendor button semantics](vendor-button-semantics.md)           | Recovered button-to-action table and its limits  |
| [DSP boundary](emulation/dsp-boundary.md)                       | Firmware, SPI/GPIO control and message framing   |
| [Bluetooth stack](emulation/bluetooth-stack.md)                 | Donor limitations and replacement media path     |
| [Control-plane emulation](emulation/control-plane-emulation.md) | WAMP execution and vendor control surface        |
| [Donor control plane](donor-control-plane.md)                   | The vendor's own service and call topology       |
| [Boot/update state](emulation/boot-update-state.md)             | Historical markers and unresolved slot selection |

## Trace milestones and sources

| Page                                                                               | Purpose                                               |
| ---------------------------------------------------------------------------------- | ----------------------------------------------------- |
| [Decision record](journal.md)                                                  | What was measured, what it forced, and what it cost  |
| [NAND decision history](nand-write-decision.md)                                    | Failed trials, withdrawn methods and recovery limits  |
| [bootimgs format](bootimgs-format.md)                                              | The container layout and the three-slot header table  |
| [Firmware reference](firmware-reference.md)                                        | Retained inputs, image formats and vendor generations |
| [Corpus index](corpus/00_README.md)                                                | Evidence conventions and provenance                   |
| [Hardware baseline](corpus/01_CANONICAL_HARDWARE_BASELINE.md)                      | Sourced hardware facts and open questions             |
| [FCC inventory](corpus/04_FCC_EXHIBIT_INVENTORY.md)                                | Regulatory artifacts and their limits                 |
| [Sibling-source cross-index](corpus/05_SIBLING_SOURCE_CROSSINDEX.md)               | Related platforms and community projects              |
| [Acquisition manifest](acquisition/invoke_berlin_artifact_acquisition_manifest.md) | Source identities and acquisition status              |
| [Storage policy](acquisition/storage-policy.md)                                    | Retention priorities, backup and restore              |

Private evidence locators are not download links. Public acquisition sidecars
do not contain every build manifest or hardware capture.
