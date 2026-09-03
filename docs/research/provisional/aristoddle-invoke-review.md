---
title: Aristoddle Invoke project review
description: Provisional evidence and reuse review of the community open-source speaker project
ms.date: 2026-09-02
ms.topic: reference
---

The public `Aristoddle/hk-invoke-opensource-speaker` repository was mirrored at
commit `948e85e2ddbdd560e186913cdfaad3f57f118c93` after the reInvoke project had
independently reached RAM-native Linux, NAND readback, native Wi-Fi, MCU access,
and partial Bluetooth bring-up.

This review treats the external repository as prior art, not as authoritative
evidence.

## Provenance

| Field | Value |
|-------|-------|
| Upstream | `https://github.com/Aristoddle/hk-invoke-opensource-speaker.git` |
| Commit | `948e85e2ddbdd560e186913cdfaad3f57f118c93` |
| Commit date | 2026-06-23 |
| License | MIT |
| Mirror | `reinvoke-archive/git-mirrors/community/hk-invoke-opensource-speaker.git` |
| Metadata | `metadata/P2-004.json` |

## Corroborated findings

The following external directions agree with independent reInvoke hardware
observations:

* Yellow service mode can reach a RAM-only U-Boot and Linux path.
* The useful kernel/initramfs load sequence uses image types `0x81` and `0x82`.
* The aggregate NAND data area is 256 MiB.
* SD8887 Wi-Fi requires firmware and board calibration staged into the RAM
  filesystem before the module loads.
* Persistent NAND work must remain separate from RAM bring-up.
* Transient credential files and explicit approval gates are appropriate for
  Wi-Fi association.

## Claims not promoted

The repository states that Wi-Fi association, DHCP, and internet access were
proven, but points to a user-local session directory that is not committed.
The result is plausible and directionally corroborated by reInvoke's successful
native scan, but the public repository does not contain the cited run evidence.

The README describes WM8904 as the Invoke codec. The acquired GPL source
contains an ACast reference device tree with a WM8904 node, but no physical
Invoke codec identity has been measured. The statement remains a hypothesis.

The public status still lists audio output, Bluetooth A2DP, controls, LED ring,
and Home Assistant integration as future work. It is not a completed speaker
runtime.

## Rejected safety conclusion

The external durability design argues that an immutable mask-ROM path makes
some persistent writes recoverable. That conclusion is not accepted:

* The component that emits every observed `1286:8174` stage has not been proven
  to be immutable mask ROM.
* The documented `99_IMAGE` failure is itself a counterexample to universal
  recovery through that path.
* A userspace or U-Boot blocklist cannot constrain arbitrary code inside a
  served initramfs.

reInvoke therefore retains its no-NAND-write policy until readback, slot
selection, signed-container behavior, and restoration are independently proven.

## Reuse candidates

Adapt after review:

* `hk_usb_boot.c` marker/state-machine and split-marker console handling
* Executable safety self-tests for command filters
* Kernel `mtdparts` entries marked read-only
* Explicit approval gates for network association
* No-NAND initramfs inspection and readiness checks

Do not copy unchanged:

* The loader's mutation of `07_IMAGE` inside its input image directory
* Its incomplete U-Boot blocklist
* macOS- and Zsh-specific wrappers
* String-presence tests presented as end-to-end safety verification
* The durability document's Tier T2 persistent-write recommendation

The loader's `0xFF` check is not itself a defect. The physical unit transitions
through a transient `0xFF` iROM stage, and the loader's auto mode then waits for
the later non-`0xFF` request-serving stage. Any adapted implementation must
retain that multi-stage distinction rather than treating either subclass as
the unit's permanent identity.

## Relationship to reInvoke

The projects are complementary. The external project validates the value of a
stock-kernel plus RAM-userspace approach and provides useful guard patterns.
reInvoke has independently progressed further in published evidence for NAND
imaging, active-rootfs extraction, MCU hardware access, partial native
Bluetooth, independent WAMP control, and replacement-kernel construction.

The external work does not change the immediate priority: first establish a
reproducible rebuilt-kernel boot with the known-good memory layout and device
tree, then add SPI and audio boundaries incrementally.
