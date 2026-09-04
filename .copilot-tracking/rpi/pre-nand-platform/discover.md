---
title: Pre-NAND platform follow-up discovery
description: Ordered follow-up work discovered after implementation and review
ms.date: 2026-09-03
ms.topic: overview
---

## Status

Discovery is 50 percent complete. All host-side optimizations are finished:
in-memory caching eliminates rotary volume latency, `micmute` is wired to DSP
microphone privacy opcode `0x09` (`com.harman.dsp.micMute`), LED one-shot
animations auto-clear, and DSP link retry stability is verified. The native
platform harness passes cleanly. Hardware validation is ready upon USB boot.

## Excluded decision

NAND persistence remains a separate follow-up requiring heavy review,
recoverability evidence, and explicit user approval.

## Ordered follow-up work

0. ~~Reproduce the patched `bluealsa-aplay` and confirm the gate digest.~~
   Resolved in iteration 7. The lease patch is confirmed present, the binary
   is byte-reproducible, and the full v10 image now builds deterministically.
1. ~~Verify the playback lease under a genuinely active PCM.~~ Resolved in
   iteration 8. Lease PID and ALSA owner PID match while streaming, the lease
   is released on stop, and the amplifier stays muted throughout.
2. ~~Exercise the buttons and rotary encoder.~~ Resolved in iteration 9. All
   input events decode and publish.
3. ~~Route Mic-Mute button to `com.harman.dsp.micMute` and optimize rotary latency.~~
   Implemented in iteration 11. Rotary latency reduced to single-process/in-memory
   fast path; Mic-Mute decoupled from A2DP audio stream and wired to DSP opcode 0x09.
4. Cold-boot the updated image and verify live rotary volume smoothness, mic-mute
   LED and DSP behavior, and acoustic playback over A2DP.
5. Complete cold boot repeatability, service fault injection, and soak validation.
6. Perform targeted donor-contract audit and close final functional and safety review.
7. Perform a separate NAND persistence feasibility and rollback review only after
   explicit approval.
