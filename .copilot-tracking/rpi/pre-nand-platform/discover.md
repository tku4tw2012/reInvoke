---
title: Pre-NAND platform follow-up discovery
description: Ordered follow-up work discovered after implementation and review
ms.date: 2026-09-04
ms.topic: overview
---

## Status

Discovery is 92 percent complete. Host-side optimizations are finished:
in-memory caching eliminates rotary volume latency, `micmute` is wired to DSP
microphone privacy opcode `0x09` (`com.harman.dsp.micMute`), LED one-shot
animations clear with the recovered three-frame packet, and the final BlueALSA
pair passes machine playback continuity. Target Bluetooth address is pinned to
`D8:F7:10:C1:46:E9`.

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
4. Complete attended playback continuity acceptance. LED clearing is resolved
   with the donor 41-byte, three-frame packet. The patched BlueALSA daemon and
   player pass 96-second and five-minute machine runs without XRUNs or
   mid-stream reopenings. One attended run remains.
5. ~~Test microphone capture and inbound audio from the microphone array over
   DMA.~~ Resolved. Attended speech and tapping were present when unmuted and
   every captured sample was zero when muted.
6. ~~Cold-power the DSP, validate `getVer` and Mic-Mute responses, then
   correlate mute against live capture.~~ Resolved on the running RAM image.
7. Complete cold-boot repeatability and soak validation. Router, MCU, DSP,
   BlueALSA, and player fault injection passed. Boot the accepted image and
   complete cold boots 2 through 5.
8. Complete the final functional and safety review after the accepted image
   validates the pairing-agent generation guard and startup order. Track WAMP
   setup-response interleaving as medium-priority hardening.
9. Perform a separate NAND persistence feasibility and rollback review only after
   explicit approval.

## Iteration 15: v13 assembly and reproducibility repair

Boot 1 of the accepted v12 image was live for this iteration, so live-service
work continued while host builds ran.

### Live evidence recorded from the running v12 image

* On-device acceptance: 22 of 23 checks pass. The single genuine failure is
  `network.wamp_firewall`; v12 predates the firewall work and packages no
  `iptables`. Three earlier apparent failures were a harness error on the host
  side, not target behavior: the script was invoked with `sh` instead of
  `/bin/busybox sh`, so `[` was unavailable.
* WAMP exposure baseline: `9998` and `9999` listen on `0.0.0.0` with no INPUT
  filtering. `mlan0` held no address during the test, so nothing was reachable
  off-box at that moment, but the exposure is real once the station associates.
  This is the condition the v13 allowlist closes.
* Router fault injection: killing Bonefish restarted it (802 to 2048) while the
  MCU and DSP services kept their original PIDs and re-registered against the
  new router, 10 and 8 registrations respectively.
* Service fault injection: the MCU and DSP services each restarted cleanly under
  supervision.

### Defect found on hardware: DSP readiness could be lost on restart

Restarting the DSP service intermittently left `/run/reinvoke/dsp-booted`
absent. One restart reproduced the loss and a later restart did not, which
identified it as a race rather than a deterministic failure.

The service cleared boot state at startup and only recreated it from
`reportBootup`, which runs on the WAMP delivery path. The pump forwards device
frames to that path with a non-blocking send, so a boot frame that arrives while
no session is consuming is dropped and readiness is never recorded.

The DSP link is the authority for that fact, so the pump now records boot state
at detection and treats WAMP delivery as best effort. `recordDSPBootState` is
idempotent, so the existing publish path is unchanged.
`TestPumpRecordsBootStateWithoutWAMPDelivery` drives the pump with an
unbuffered device channel and no reader; it fails without the fix.

### Defect found in the build: the kernel was not reproducible

Two kernel builds differed. `vmlinux` and `System.map` were byte-identical, so
the kernel itself was already deterministic; the divergence entered during
compression. Six bytes differed in `piggy.lzo` at a constant size, in the lzop
header modification time and the header checksum that covers it.

`cmd_lzo` piped the payload into lzop, which leaves lzop no source file to take
a time from, so it stored the current clock. Patch
`0004-reproducible-lzo-piggy.patch` compresses the input as a named file with
its mode and modification time pinned first. `parse_header` in
`lib/decompress_unlzo.c` skips mode, both mtime words, the file name, and the
header checksum, so the kernel decompressor reads nothing that changed.

### Digest substitutions

Recorded in the v13 build notes beside the artifacts. Every replacement was
confirmed by two consecutive byte-identical builds.

* `reinvoke-mcu-interface` and `reinvoke-dsp-interface`: owned source changed.
* `reinvoke-windowd`: newline framing and process-group enforcement landed after
  the previous pin.
* `bluez-media-control`: the previous pin came from a D-Bus tree that was not
  preserved and could not be rebuilt. The reproducible D-Bus recipe is now
  recorded in `tools/control/README.md`.
* `bluetoothd`, `bluealsa-cli`, and `hci-init`: the pins referred to pre-strip
  inputs that no preserved artifact matched. The builder strips these on
  install, stripping is idempotent for all of them, and `bluealsa` and
  `bluealsa-aplay` were already pinned in stripped form. The pins now describe
  the same stripped bytes the image actually carries.

### v13 candidate

Runtime manifest `a3f2ab500af7d34bec553de56525c5d2a028fc3b1a7e933024a8104a3c2dbf20`,
initramfs `27d052e7cfa2fba18188ee698712bb3612ab235fc897e60993bfef0a3d4043a2` at
32,384,776 bytes, and kernel
`eaf31eb8e4a33709752579c097bb17f5136f3fd98598876b1df8af59581ab67c`. Two independent builds of both are byte-identical. v13 is
the first image carrying the WAMP allowlist and `iptables`, the private DSP
microphone socket, the MCU privacy controller, top-tap media control, and the
provisioning window daemon.

### WAMP setup-response correlation closed

Both clients wrote a REGISTER or SUBSCRIBE and then read exactly one frame,
assuming it was the reply. A router is free to deliver an event or an invocation
in between, which would have failed the session or silently consumed a message.

Both now use one correlated reader that matches the reply by request id, fails
on a matching ERROR, and queues everything else for the session loop, bounded at
256 messages. The MCU registers eleven procedures and two subscriptions and the
DSP seven and one, so both had a wide window. Tests cover interleaved traffic on
both clients and an ERROR reply on the MCU.

### Remaining

Cold boots 2 through 5 need the operator, because yellow mode requires the
button held at power-on and cannot be entered from software.
