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

### Soak result on boot 1

The v12 boot stayed up 1 hour 10 minutes and survived three injected service
kills. Every supervised service was alive at the end, load average was steady
near 0.4, and `/run/reinvoke/dsp-booted` was present after the final DSP
restart. No supervisor entered a restart loop.

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
`0004-reproducible-lzo-piggy.patch` compresses a scratch copy of the input with
its mode and modification time pinned first. `parse_header` in
`lib/decompress_unlzo.c` skips mode, both mtime words, the file name, and the
header checksum, so the kernel decompressor reads nothing that changed.

Review caught a regression in the first version of this patch, which pinned the
times on `arch/arm/boot/Image` itself. `Image` is a make target built from
`vmlinux`, so pinning it to epoch left `vmlinux` permanently newer and made
`if_changed` rebuild the objcopy, compress, link, and image tail on every later
make with no source change. Modelling the real `vmlinux` to `Image` to `piggy`
edges reproduced it: three consecutive runs each rebuilt both targets. With the
scratch copy, run 1 builds and runs 2 and 3 report up to date, the scratch file
is removed, and determinism is unchanged.

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
`514700fa88835c591cf6a02e8db7ef8d80d5b5f199355e643317609b69e33500`. Two independent builds of both are byte-identical. v13 is
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

### ledSet vocabulary recovered

The front Wi-Fi diffuser and rear pairing indicator are driven by
`com.harman.ledSet`, which reInvoke does not implement. Static recovery from the
donor `mcu-interface` and `audio-ui` gives targets `front` and `back`, related
names `wifi` and `bluetooth`, states `slow-blink` and `fast-blink`, colours
`white`, `amber`, `green`, `blue`, and `black`, and a separate brightness
procedure bounded to 0-100. That matches both observed behaviours: amber and
white Wi-Fi states, and a rear slow-blink while pairing is open.

The wire transport is still unresolved. Argument order, opcode, and frame layout
need handler disassembly, as `ledAnimate` and `ledOff` did. These indicators must
not be approximated with top-ring animations, which would report Wi-Fi and
pairing state on the wrong physical part.

### Remaining

Cold boots 2 through 5 need the operator, because yellow mode requires the
button held at power-on and cannot be entered from software.

## Iteration 16: cold boot 2 on v13, and the regression it exposed

The operator power-cycled into yellow mode and the armed loader injected v13
automatically: prompt caught, kernel and initramfs loaded, ADB back in five
seconds.

### What v13 proved

* `network.wamp_firewall` passes. The INPUT chain accepts ports 9998 and 9999
  from loopback and from the single configured allowlist entry, then drops the
  rest. Bonefish still binds every interface, which is why the chain, not the
  bind address, is the control.
* The microphone privacy boundary is in place. `com.harman.dsp.micMute` is
  registered by the MCU service, one of its eleven procedures. The DSP no longer
  exposes it, so the raw opcode cannot be reached from the unauthenticated bus.
* PID 1 reported `hardware initialized muted; WAMP unmute policy=false`.
* The correlated setup reader works against the real router. Five MCU restarts
  each re-registered exactly eleven procedures, 36 through 80 cumulatively, with
  no registration, subscription, or bound errors.
* Nine of ten supervised services came up and stayed up.

### Regression exposed: the DSP could not boot

`reinvoke-dsp-interface` restart-looped, failing every attempt identically:

```
boot DSP: stream image at offset 20468: SPI_IOC_MESSAGE: connection timed out
```

Always the same offset, roughly 12 percent into a 40,121-transfer download.

The cause was the SPI GPIO-ready patch, not any owned service. The version v13
carried had replaced the donor's endless spin with a single instantaneous
sample:

```c
if ((*gpioreg & 0x2000) == 0) {
        message->status = -ETIMEDOUT;
        goto early_exit;
}
```

That fails the transfer if the DSP has not already asserted ready at the moment
it is first sampled. The DSP legitimately needs a short interval mid-download,
so the transfer aborts at the first such point every time.

The earlier bounded version, which v12 booted successfully with, polls with a
100 ms jiffies deadline and `cpu_relax()`. It keeps the property the change was
made for, that a caller can no longer wedge forever in `spidev_sync`, without
demanding that the device be instantly ready. That version is restored and the
source and patch gates are re-pinned.

The lesson is narrow and worth keeping: bounding an unbounded wait is a
correctness fix, but reducing the bound to zero is a different change, and only
hardware distinguishes them. Nothing in the host suite could have caught this.

### v14 candidate

Kernel `d29a007535794d74d8ed900da366f02631a9a981356caea707e6b163f6d07746`, built
twice byte-identically. The module tree is unchanged from v13, so the v13
initramfs `27d052e7cfa2fba18188ee698712bb3612ab235fc897e60993bfef0a3d4043a2`
is still correct and both are staged.

Testing the fix needs another yellow-mode window, because the DesignWare SPI
driver is built into the kernel rather than loadable.

### Open observation

One MCU log line appeared during the DSP restart storm and has not been
explained: `rotary input: MCU interrupt remained low after 1024 pending reads`.
It may be a consequence of the DSP crash-loop contending for the shared IO
expander. Re-check it on the v14 boot, when the DSP is healthy.

## Iteration 17: cold boot 3 on v14

The host reboot removed the old USB boot session, relay, FIFO, and armed
catcher. The v14 images survived with their expected hashes. A new session was
started from scratch, all safety gates passed, and the catcher injected v14
when the operator entered yellow mode. ADB returned in five seconds.

### v14 acceptance

The bounded SPI-ready wait fixed the v13 regression:

* the DSP downloaded all 40,121 transfers and 160,484 bytes;
* it registered seven procedures;
* `EVENT_DSP_BOOTUP` arrived; and
* the native acceptance suite passed 23 of 23 checks, including the WAMP
  firewall, DSP marker, and all supervised services.

One DSP restart also completed the full download, recreated the private
microphone socket, and restored the boot marker.

### DSP response-retry regression

An experimental capture probe opened the verified 48 kHz stereo `S32_LE`
device but timed out in the codec DMA path. The next microphone-mute request
changed the durable RAM state to `muted`, then received an all-zero DSP response
header. That is fail-closed, but the DSP service treated the missing response as
a link failure. Every following service generation booted the DSP, then failed
while restoring the muted state and restarted.

The link refactor in v13 had changed an important older behavior. Previously it
sent a command once, then retried the response read after sleeping and waiting
for active-low Ready again. The refactor instead retransmitted the complete
command up to three times. That loses the behavior already documented after
the first all-zero-header hardware finding and can duplicate a command whose
effect occurred even though its response was missed.

The link again sends exactly once. A rejected or all-zero header sleeps, waits
for Ready, and retries receive only. Valid unrelated frames are still preserved
and response ID/opcode correlation remains. Unit tests assert one command
transmission across the retry. A RAM-only replacement on the already
warm-wedged DSP did not recover it, so cold-boot confirmation remains necessary.
The speculative one-second post-boot settle tested during diagnosis did not
help and was removed.

### Bluetooth generation lifecycle

Fault injection validated the generation guard itself:

* old `bluetoothd` PID 913 and pairing-agent PID 965;
* the old agent was gone within one second of killing the daemon;
* new `bluetoothd` PID 2958 appeared at five seconds; and
* new pairing-agent PID 2985 appeared at six seconds.

The test exposed a separate gap. `hci-init --reset` ran only before the first
daemon generation. The restarted daemon therefore saw `hci0` as `Not Powered`,
and replacement agents failed in bounded cleanup/retry cycles. Running the
owned HCI initializer live restored the real 120-second pairing window.

PID 1 now wraps every supervised `bluetoothd` generation with
`hci-init --reset`, then `exec`s the daemon so the PID file still identifies the
actual generation. The existing pairing guard therefore invalidates old agents
on PID change, and every replacement daemon starts with an initialized,
powered controller.

### v15 candidate

The kernel stays at
`d29a007535794d74d8ed900da366f02631a9a981356caea707e6b163f6d07746`.
The v15 runtime manifest is
`4685923f86a8e485cc5be4bf0618384593b488ddfbf481525e174f8aa3cfc6bb`,
and the 32,382,132-byte initramfs is
`9ab76db2ee7f8d9e7533355ce91d2dde014205a5d6f22111096db256004eddd9`.
Two independent runtime and initramfs builds agree byte for byte. The pair is
staged for cold boot 4.
