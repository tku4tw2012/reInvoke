---
title: reInvoke documentation index
description: Candidate 02 acceptance, candidate 03 startup, preserved history, and public-versus-private evidence boundaries
ms.date: 2026-09-12
---

## Start here

reInvoke is a personal DIY project. Candidate 02 demonstrated ordinary
wall-power NAND startup, Bluetooth audio, physical controls, authenticated
Wi-Fi provisioning, and local WAMP control. Native USB/ADB, native microphone
data-path acceptance, and persistent user settings remain open. Candidate 03
is now installed and has answered a fresh Bluetooth name request with
`reInvoke-NAND` after a power-only boot. Native USB remains absent; native SSH
and the broader 03 service checks remain open. Candidate 02's accepted features
are not automatically accepted on its successor.

| Guide | Use |
|---|---|
| [Repository overview](../README.md) | Project scope, licensing, and publication status |
| [Native NAND platform](native-nand-platform.md) | Current 03 startup evidence, candidate 02 acceptance, installed hashes, and recovery limits |
| [Candidate 03 build appendix](native-nand-platform.md#offline-successor-appendix-candidate-03) | Preserved offline build history, exact installed artifact pins, and qualification limits |
| [Product contract](current-product-contract.md) | Owned service boundaries and explicit native-versus-RAM evidence |
| [Project status](../PLAN.md) | Completed milestones and remaining work |
| [Revival roadmap](revival-roadmap.md) | Why the project keeps the existing compute platform |
| [Native provisioning](native-provisioning.md) | Authenticated onboarding with volatile credentials |
| [Microphone capture](microphone-capture.md) | Implemented state-file gate, historical RAM acceptance, and deferred stronger privacy design |

## Development and recovery

The public source is not a complete fresh-clone firmware kit. Native builders
require private RC12 artifacts, donor inputs, toolchains, and declared source
pins. Generated firmware and private deployment configuration are not published.
Build commands do not authorize a device action.

| Guide or tool | Scope |
|---|---|
| [U-Boot access](uboot-access.md) | Previously demonstrated recovery, not a shell into a running native session |
| [RAM platform](native-ram-platform.md) | Host-loaded ARM Linux and its historical component tests |
| [USB-boot tools](../tools/usb-boot/README.md) | Host recovery and RAM composition |
| [NAND builders](../tools/nand-pilot/README.md) | Private-input main rootfs, paired BSL, and vendor bundle composition |
| [Kernel builder](../tools/kernel/README.md) | Custom RAM/recovery kernel, not candidate 02's retained vendor native kernel |
| [Control tools](../tools/control/README.md) | WAMP diagnostics and offline contract references |
| [Provisioning tools](../tools/provisioning/README.md) | Volatile parser, station-apply, and network services |
| [Capture tools](../tools/mic-capture/README.md) | Capture service and attended test client |

## History and failed experiments

Historical “current” and “next” statements describe their checkpoint only.
Do not replay a stored write, restore, reset, or helper command as a current
recipe. Missing USB/ADB was not conclusive proof of a particular failed boot
stage. A later recovery boot does not recover the prior native session's
volatile logs.

* [NAND decision history](nand-write-decision.md) retains the full pilot,
  vendor-stack, StockRoot, BSL, recovery, and candidate 02 record.
* [Two-block NAND experiment](nand-startup-probe.md) retains failed assumptions,
  mapping cleanup defects, corrections, write/readback, and restoration.
* [USB service-mode investigation](usb-service-mode.md) retains the failed
  attempts and corrected interpretations before successful U-Boot access.
* [Hardware validation plan](hardware-validation-plan.md) and
  [closed-unit observation procedure](no-disassembly-observation-procedure.md)
  retain the pre-bring-up plans.
* [Project journal](journal.md) preserves dated findings and corrections.
* [Phase 3 findings](../FINDINGS.md) preserve static vendor-image analysis.

## Service evidence

These pages separate recovered donor behavior from owned service policy.
Detailed on-device traces generally come from historical RAM boots unless
explicitly identified as candidate 02 native observations.

* [Bluetooth stack](emulation/bluetooth-stack.md)
* [Owned speaker control](emulation/owned-speaker-control.md)
* [MCU boundary](emulation/mcu-boundary.md)
* [DSP boundary](emulation/dsp-boundary.md)
* [Donor control-plane emulation](emulation/control-plane-emulation.md)
* [Final vendor firmware control surface](emulation/final-firmware-control-surface.md)
* [Donor boot/update state](emulation/boot-update-state.md)

## Acquisition, hardware, and original evidence

* [Storage policy](acquisition/storage-policy.md) records private retention and
  labels old footprint/cost estimates as historical.
* [Acquisition manifest](acquisition/invoke_berlin_artifact_acquisition_manifest.md)
  is a dated seed specification, not proof that every discovery job completed.
* [Retention ranking](acquisition/source-retention-ranking.md) records dated
  custody-risk observations.
* [Private restore runbook](acquisition/azure-restore-runbook.md) requires
  operator-owned archive access.
* [Hardware corpus](corpus/00_README.md) indexes specifications, the claim
  ledger, FCC exhibits, sibling sources, and corpus digests.
* [Bundle contents](bundle-contents/README.md) separates authored analysis from
  unchanged extracted vendor originals.
* [Provisional research](research/provisional/README.md) preserves external
  reports and their corrections without making their embedded recipes current.

## Publication and evidence limits

The repository release list and API returned no releases on 2026-09-12 UTC.
Public vendor inputs originate at
[coggy9/HKHacking releases](https://github.com/coggy9/HKHacking/releases);
the custom image is not published. Acquired inputs have public provenance
sidecars in [metadata](../metadata). Later private builds/captures use private
manifests and redacted evidence summaries, not a complete public archive index.

Historical `tools/nand-inspect/` references and the early-2134 operator helpers
refer to deferred/private operator tooling, not tracked public build recipes.
Their absence from a public clone is a publication gap, not permission to
replace a reviewed writer. Internal session notes are not product
documentation. Neither they nor private firmware/evidence are removed by this
documentation cleanup.
