---
title: Pre-NAND platform follow-up discovery
description: Ordered follow-up work discovered after implementation and review
ms.date: 2026-09-03
ms.topic: overview
---

## Status

Discovery is 10 percent complete. Item 0 was added after the iteration 6
host audit. The audit retracted an incorrect blocker and closed a real
reproducibility gap by archiving pinned upstream sources.
after the pre-NAND acceptance gate.

## Excluded decision

NAND persistence remains a separate follow-up requiring heavy review,
recoverability evidence, and explicit user approval.

## Ordered follow-up work

0. Reproduce the patched `bluealsa-aplay` from the newly archived pinned
   upstream source and confirm it matches the gate digest `4c997821...`.
   This is a reproducibility confirmation, not a blocker; the shipped binary
   is already correct and byte-reproducible across two builds.
1. Finish the packaged v9 cold-boot, audible, and acoustic acceptance gate.
2. Complete five cold boots, service fault injection, and soak validation.
3. Perform the targeted donor-contract audit and close the final functional
   and safety review.
4. Evaluate an optional owned blue Bluetooth pairing LED pattern.
5. Evaluate an optional local management web UI.
6. Evaluate runtime Wi-Fi-change user experience.
7. Evaluate selective userspace upgrade behavior without persistence.
8. Perform a separate NAND persistence feasibility and rollback review only
   after explicit approval.
