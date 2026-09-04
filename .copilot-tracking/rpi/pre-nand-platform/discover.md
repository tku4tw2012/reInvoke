---
title: Pre-NAND platform follow-up discovery
description: Ordered follow-up work discovered after implementation and review
ms.date: 2026-09-03
ms.topic: overview
---

## Status

Discovery is 35 percent complete. All host-side work is finished: the runtime
and initramfs are byte-for-byte reproducible from committed source plus
archived, checksummed, GPG-verified upstream inputs. Item 0 is resolved, and
the v10 image has now been booted and validated on hardware: A2DP pairs and
streams, the playback lease behaves correctly under an active PCM, the
amplifier never unmutes, and all physical controls work. Every remaining item
is hardware-gated.

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
   ten input events decode and publish; Mic-Mute proven by controlled toggle.
3. Route the Mic-Mute button to `com.harman.dsp.micMute` instead of to the
   BlueALSA A2DP PCM. The button is the stock microphone privacy control, but
   our runtime currently mutes the incoming music with it while the DSP mic
   mute goes uncalled. Not a safety problem, since the speaker cannot sound
   either way, but it is the wrong failure direction for a privacy control.
   Needs an attended test with microphone capture running to confirm the mute
   reaches the captured audio.
4. Cold-boot the v10 image and complete the audible and acoustic acceptance
   gate. Requires the device in yellow mode. Also the point at which to test
   the cold-versus-warm hypothesis for the outstanding `dsp.boot_event`
   failure, which is the only remaining acceptance failure.
5. Complete five cold boots, service fault injection, and soak validation.
6. Perform the targeted donor-contract audit and close the final functional
   and safety review.
7. Evaluate an optional owned blue Bluetooth pairing LED pattern.
8. Investigate the Bluetooth button reporting `bluetooth-long` for what the
   operator intended as a short press. Low priority, but `bluetooth-long`
   reopens the pairing window, so a short press can reopen pairing
   unintentionally.
5. Evaluate an optional local management web UI.
6. Evaluate runtime Wi-Fi-change user experience.
7. Evaluate selective userspace upgrade behavior without persistence.
8. Perform a separate NAND persistence feasibility and rollback review only
   after explicit approval.
