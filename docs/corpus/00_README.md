---
title: Invoke hardware evidence
description: Hardware references, evidence conventions, and acquired-source provenance
ms.date: 2026-09-12
---

## Reference map

Invoke (`HKINVOKE`, FCC ID `APIHKINVOKE`) combines a removable Berlin-family
compute module with separate audio, controls, and microphone assemblies.
The corpus connects published specifications and source analysis to observations
of one unit. Current operating status belongs in the
[native NAND guide](../native-nand-platform.md), not the hardware inventory.

| Reference                      | Contents                                       |
| ------------------------------ | ---------------------------------------------- |
| [Hardware baseline][baseline]  | Product specifications and measured geometry   |
| [FCC exhibits][fcc]            | Acquired PDFs and qualified package readings   |
| [Sibling sources][siblings]    | Berlin driver references and community work    |
| [Firmware reference][firmware] | Retained inputs, image formats, vendor runtime |
| [Open questions][questions]    | Unresolved physical and recovery boundaries    |

## Evidence conventions

Manufacturer specifications describe the product; FCC photographs describe
the certification sample. Runtime results identify the unit, date, and
candidate or RAM environment. Image contents and source code establish
implementation, not execution. Third-party reports retain their attribution.

Measurements take precedence over sibling-platform guesses: this unit has
512 MiB DRAM, 256 MiB NAND, and SD8887-family radio enumeration. Exact package
identities, connector wiring, and boot-selection semantics remain separate
questions. A search miss applies to the searched repositories and refs only.
Numeric derivations retain their premises, such as `19 V x 2 A = 38 W`;
that supply rating is not interchangeable with the published 40 W audio rating.

## Provenance

Public [metadata sidecars](../../metadata/) record acquired-artifact source
URLs, timestamps, sizes, checksums, and Git revisions. The
[acquisition manifest](../acquisition/invoke_berlin_artifact_acquisition_manifest.md)
indexes them; the [storage policy](../acquisition/storage-policy.md) covers
held originals and mirrors. Private archive-relative locators are not downloads.
Git versions the documentation; artifact hashes identify the acquired bytes.

* FCC acquisition: 20 PDFs, 13 byte-unique, acquired 2026-08-28.
  The [exhibit inventory][fcc] links all 20 metadata records.
* Invoke firmware/source: the [firmware reference][firmware] distinguishes
  vendor packages, community modifications, and independently captured images.
* Community and sibling code: the [cross-index][siblings] links pinned metadata
  where available and identifies historical searches without exact commit pins.
* Cortana SDK notices: the [2023-12-03 Wayback capture][notices] was acquired
  after the live Harman URL redirected. The private record is
  `web-pages/harman-cortana-sdk-opensource-20231203010301.html`, SHA-256
  `6b2e25ae48c4e3456c1952a2ff13d8013cf978b68f94d7295a741e30aac7696b`.
  This Microsoft-authored attribution document lists Expat, RapidJSON, Parson,
  zlib, curl, Breakpad, OpenSSL, Opus, Unicode data, and CMake-related code.
  It is not SDK source or an Invoke-image component manifest.

## Research milestones

The August 2026 baseline began with product/regulatory evidence and community
USB reports. September RAM bring-up established memory and radio geometry,
preserved NAND data, and recovered MCU/DSP and capture interfaces. Native
startup followed, without resolving every physical part or guaranteeing
recovery from arbitrary boot-chain corruption. See the
[journal](../journal.md) for milestone evidence and the
[roadmap](../revival-roadmap.md#remaining-work) for remaining implementation work.

[baseline]: 01_CANONICAL_HARDWARE_BASELINE.md
[fcc]: 04_FCC_EXHIBIT_INVENTORY.md
[siblings]: 05_SIBLING_SOURCE_CROSSINDEX.md
[firmware]: ../firmware-reference.md
[questions]: 01_CANONICAL_HARDWARE_BASELINE.md#open-questions
[notices]: https://web.archive.org/web/20231203010301/https://www.harmankardon.com/cortana-sdk-opensource.html
