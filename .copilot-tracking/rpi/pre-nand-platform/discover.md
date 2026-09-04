---
title: Pre-NAND platform follow-up discovery
description: Ordered follow-up work discovered after implementation and review
ms.date: 2026-09-03
ms.topic: overview
---

## Status

Discovery is 20 percent complete. All host-side work is finished: the runtime
and initramfs are byte-for-byte reproducible from committed source plus
archived, checksummed, GPG-verified upstream inputs. Item 0 is resolved.
Every remaining item is hardware-gated.

## Excluded decision

NAND persistence remains a separate follow-up requiring heavy review,
recoverability evidence, and explicit user approval.

## Ordered follow-up work

0. ~~Reproduce the patched `bluealsa-aplay` and confirm the gate digest.~~
   Resolved in iteration 7. The lease patch is confirmed present, the binary
   is byte-reproducible, and the full v10 image now builds deterministically.
1. Cold-boot the v10 image and complete the audible and acoustic acceptance
   gate. Requires the device in yellow mode.
2. Complete five cold boots, service fault injection, and soak validation.
3. Perform the targeted donor-contract audit and close the final functional
   and safety review.
4. Evaluate an optional owned blue Bluetooth pairing LED pattern.
5. Evaluate an optional local management web UI.
6. Evaluate runtime Wi-Fi-change user experience.
7. Evaluate selective userspace upgrade behavior without persistence.
8. Perform a separate NAND persistence feasibility and rollback review only
   after explicit approval.
