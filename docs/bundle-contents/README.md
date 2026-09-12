---
title: Preserved firmware text and authored analysis
description: Navigation and corrections beside unchanged extracted vendor evidence
ms.date: 2026-09-12
---

## Evidence boundary

The subdirectories retain the small extracted text layer and listings from
the acquired vendor bundles. Original scripts, configuration, notices, driver
files, and manuals remain unchanged under their original terms; the project
MIT licence does not relicense them. Full packages and extracted binaries
remain private. The original public source is
[coggy9/HKHacking releases](https://github.com/coggy9/HKHacking/releases), not
a reInvoke release.

These are historical vendor inputs, not current installation instructions.
In particular, original flash scripts may invoke operations excluded from
the owned workflow. Image 99 is not approved for reInvoke installation.

## Authored analysis

| Page | Evidence scope |
|---|---|
| [Phase 3 analysis](invoke-flashing/phase3-analysis.md) | Recovery kernel/initrd, normal filesystem, OTA, and radio lineage |
| [Runtime interface inventory](invoke-flashing/runtime-interface-inventory.md) | Static vendor clients, router, buses, and later corrections |
| [OTA2 analysis](invoke-ota2/ota2-analysis.md) | Final 12.2134.0 vendor firmware and complete kit contents |
| [Root Phase 3 findings](../../FINDINGS.md) | StockRoot/flashing variants and extraction provenance |

## Original bundle directories

* [Flashing text layer](invoke-flashing/) and its [original listing](invoke-flashing/LISTING.txt)
* [OTA2 text layer](invoke-ota2/) and its [original listing](invoke-ota2/LISTING.txt)

The vendor `gen-cmd.sh` example describes 512 MiB of allocations. It does not
establish this unit's geometry: live identification and complete logical reads
established 256 MiB NAND. Keep that correction beside the original rather than
editing the extracted script.

Likewise, vendor `serviceport.sh`, DHCP, and U-Boot development addresses in
static analysis are vendor defaults, not live operator bindings or connection
instructions. Use the [native NAND guide](../native-nand-platform.md) for the
current owned image and the [product contract](../current-product-contract.md)
for its evidence limits.
