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
unresponsive DSP did not recover it, so a new boot confirmation remained
necessary.
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

## Iteration 18: exact indicator transport and donor policy

Static ARM disassembly recovered `com.harman.ledSet` end to end. The WAMP
handler accepts a required target, optional `mode` and `color` keyword
arguments, retains front amber, front white, and back state, and sends one
six-byte command:

```text
09 <front-amber> <front-white> <back> <stack> <stack>
```

The final donor bytes are uninitialized stack residue. The owned encoder sends
zeroes instead. It uses the existing fixed MCU-command transport, serializes
state with the I2C write, and commits only a successfully written state.

Modes are `off=0`, `on=1`, `dim=2`, `slow-blink=3`, and `fast-blink=4`.
Selecting front white clears amber and selecting amber clears white. Back
ignores color. Unknown target or front color sends the unchanged state; unknown
mode on a selected channel resolves to off. Host, WAMP, rollback, concurrency,
and race tests pass, and focused review found no significant issue.

The donor caller trace then recovered policy separately from transport.
`audio-ui` alone calls `ledSet`:

* Bluetooth `pairing` sends back `slow-blink`;
* `connected` sends back `on`; and
* disconnected or other state sends back off.

Its logical Bluetooth short press starts pairing, and another short press while
pairing cancels it. It defines no Bluetooth-long action. The physical bridge is
still absent from the donor rootfs: MCU publishes
`com.harman.vui.keypress`, while `audio-ui` subscribes only to
`com.harman.test.inputEvent`, and no local binary references both.

reInvoke retains its already-validated long-press fallback until it owns
pairing-window cancellation and authoritative connected/pairing state. Wiring
the rear LED directly to a button would be incorrect because timeout,
connection, and daemon restart could leave it stale.

The v16 MCU binary is
`c3db4b9e650588f7261f5967a4137a62f543a24bfca704e2b88ea44d16cbd36f`,
the runtime manifest is
`1bf8622eb08517739628fd6a17737945435a000b18c7f78f7be0166266401980`,
and the 32,389,027-byte initramfs is
`fef5f412fd0d5589f5a1cf739c9b131127f9e788147574c4388ef7122eea4453`.
Each was reproduced in a second independent build. v16 is staged and armed for
cold boot 4.

## Iteration 19: v16 boot and degraded-DSP control isolation

The first yellow entry returned to U-Boot before Linux enumerated. The device
was still at a live prompt, so the loader immediately retried the same
checksum-verified pair; v16 then returned ADB in six seconds.

Native acceptance passed all 23 checks. The v16 runtime manifest matched
`1bf8622eb08517739628fd6a17737945435a000b18c7f78f7be0166266401980`,
the DSP downloaded all 40,121 transfers, and `EVENT_DSP_BOOTUP` arrived.

The first microphone-mute control request changed the authoritative RAM state
to `muted`, then received an all-zero DSP response header. The DSP subsequently
booted but failed every attempt to restore muted state. Replacing it in RAM with
the preserved accepted-v12 DSP binary produced the same result. This rules out the v15 response-retry change as the cause. The operator later
clarified that every yellow-mode cycle included power removal, so the same
result also rules out the earlier warm-reset explanation. The next discriminator
is command order: the earlier successful sequence requested `getVer` before
Mic-Mute.

That degraded state exposed a separate MCU coupling. Every failed DSP-session
mute reconciliation returned an error from `handleDSPSessionEvent`, which tore
down the MCU WAMP session. All unrelated MCU procedures—including the new
indicator transport—therefore disappeared and reappeared with DSP health.

The handler now requests the existing background reconciliation retry, logs the
failure, and keeps the WAMP session registered. The privacy state remains
`muted`; only unrelated control-plane availability changes. A regression test
verifies logging, retry scheduling, and a non-fatal return. Focused review found
no significant issue.

A RAM-only replacement proved the behavior on the degraded device:

* rear `fast-blink` and `off` calls both returned successful WAMP results;
* front amber/white calls covered `on`, `dim`, `slow-blink`, and `fast-blink`;
* every channel was explicitly cleared;
* MCU status still returned `000116`; and
* the MCU process remained alive while the background DSP retry continued.

This validates WAMP dispatch and successful I2C submission, not visible LED
appearance; no observer was present.

The v17 MCU binary is
`9b38f0f3fdc2e7dba47d279909e8f2f2d185fb965417229ffa59df11fbfd83e7`,
the runtime manifest is
`b3d0a36234693ed289af82dca00e926756ddaf1e57ef9c7b595a0ad9a0fae1f4`,
and the 32,390,080-byte initramfs is
`2d0f17105343b9f8d5cd3d0cb8a530608c8c62ad73b5b3380c365eda7fc21cd4`.
Both runtime and initramfs were built twice byte-identically.

### Armed-catcher first-attempt regression

The last two armed cycles sent commands before the new BootROM/U-Boot banner,
and U-Boot never echoed or processed image 81. A manual retry after the actual
prompt worked. This is not a device requirement for two attempts.

The waiter previously accepted any prompt appended after its byte offset. When
armed against a running Linux gadget, a delayed prompt from the old console
generation could satisfy that condition after USB disconnected. It now records
whether Linux was present when armed and, in that case, requires both a new
`U-Boot 2013.04` banner and prompt after the offset. A deterministic regression
test appends a stale prompt, proves the waiter stays blocked, then appends the
new banner and prompt and proves it releases.

### Physical indicator acceptance

With the v17 MCU service kept online during degraded DSP retries, an attended
test exercised the recovered fixed-command transport:

* front `on` amber: steady amber/orange;
* front `on` white: switched from amber to steady white;
* front `slow-blink` amber: visibly slow;
* front `fast-blink` white: visibly faster;
* front `off`: fully dark;
* back `on`: steady light visible beside the Bluetooth button;
* back `slow-blink`: visibly slow;
* back `fast-blink`: visibly faster;
* back `dim`: no visible output during a later isolated dark-room test; and
* back `off`: fully dark.

Every WAMP call returned success and the MCU process remained alive. Front
amber/white mutual exclusion and the physical rear aperture are therefore
confirmed. Automatic Bluetooth-state policy remains separate work; this test
only proves direct indicator control.

## Iteration 20: resolving DSP command timing

Every yellow-mode entry in this campaign included power removal. `getVer` first
and Mic-Mute first both failed without tracing, and the preserved accepted-v12
binary failed identically on the same runtime, ruling out command order and the
new response-correlation code.

The original donor client initially failed too because its shell-based `devmem`
helper parsed the final `pwd` output from `/etc/profile`. Removing that output
in RAM and supplying the historical toolbox calling convention made the donor
succeed on the same hardware and kernel:

```text
readmsg: 0x00 0x00 0x08 0x00 0x00 0x64 0x58
EVENT_DSP_VERSION=0.0.64.58
```

A syscall trace of that success preserves one eight-byte transmit and one
twelve-byte response. Donor nominal one-microsecond sleeps took 8.6-9.4 ms on
this old kernel.

Tracing the owned client made its nominal 10 ms waits take 18-19 ms. Under that
delay it immediately completed `getVer`, then completed Mic-Mute with
`EVENT_MIC_MUTE`. An isolated untraced 20 ms run also passed, which was
provisionally treated as a timing result. Packaged and repeated trials later
disproved that attribution.

Startup mute was retained behind a conservative one-second readiness barrier.
A later first capture was confounded by raw ALSA `hw_params`, which can
reconfigure the DSP route after any earlier mute. Reasserting mute after capture
configuration produced all 244,736 samples zero.

The attended pair is conclusive:

| State | Samples | Nonzero | RMS | Peak |
|---|---:|---:|---:|---:|
| unmuted | 244,224 | 244,163 (99.975%) | 88,026,326 | 1,808,420,363 |
| muted | 244,736 | 0 | 0 | 0 |

The swiveling red animation appeared for confirmed mute and cleared for
confirmed unmute. The observer was asked only about the light; state came from
RAM, DSP events, and DMA analysis.

Focused review found that WAMP commands could still enter during the one-second
settle. The DSP service now keeps its router session and pump live but blocks
external dispatch on the readiness channel. Persisted startup mute uses the
private ungated link method, then closes readiness; queued WAMP work can proceed
only afterward. Cancellation and release are covered by unit and race tests.

Evidence is outside Git under
`reinvoke-archive/hardware/software-captures/20260905T183500Z-donor-getver-success/`,
`reinvoke-archive/hardware/software-captures/20260905T184500Z-owned-traced-success/`,
and
`reinvoke-archive/hardware/usb-attempts/20260905T180300Z-v17-cold-boot-5/privacy/`.

The v19 experiment DSP binary is
`95c223f94594ab8658043e491434b6d206da5dfbc5be7052fd82676b8173b548`,
the runtime manifest is
`47343f69a1398e0dcd87abb97d716731001747e52719c9d033e4ab0e8e7959f5`,
and the 32,389,139-byte initramfs is
`ca9d5ce4b3cd11881a97a72c02d17a35981f6e7cfdc76f2dbf72e3537070a72d`.
Each was reproduced independently.

## Iteration 21: separating timing from scheduling

The v19 package disproved the first timing conclusion: PID 1 execution still
failed while the same 20 ms binary succeeded when attached. Controlled sweeps
then changed one phase at a time under `start-stop-daemon`:

| Experiment | Result |
|---|---|
| Handshake/release 60, 80, 100 ms | Boot event, then all-zero command response |
| Handshake/release 120 ms | Could miss boot event |
| Post-Active-low turnaround 1, 5, 10, 20 ms | All-zero command response |
| Sleep after pump claim 50, 100, 200 ms | All-zero command response |
| One complete idle pump cycle before claim | Isolated passes, later 0/9 repeat |

The isolated deferral passes were false positives. Ten identical detached
trials later produced zero command passes and one boot-event miss. Deferral was
removed; donor-compatible 10 ms handshake/release remains.

The DMA sequence also refined the privacy boundary:

1. startup mute was acknowledged;
2. a later first raw ALSA open/configuration carried audio;
3. mute reasserted after `hw_params` produced the byte-identical all-zero WAV.

Therefore DSP mute controls an active/configured path; it cannot prevent trusted
root from reconfiguring raw PCM later. The current speaker image starts no
capture consumer. A future voice service must own that raw node contract: do not
open/read while muted, or configure first, reassert mute, wait for
confirmation, and discard all pre-confirmation samples.

The v21 experiment DSP binary is
`667beeee278ee3692855e60a039de89d8d1e9b168f16e3609e55952a7ab44901`,
the runtime manifest is
`aca3a532ea971482d88442669637e99dea3097a43e8fdf49cafbb572dd79c9de`,
and the 32,388,952-byte initramfs is
`9b88112e5425c4095098d492d3e3b4bbc4319804d8ee786e079616d38c167c94`.
Each was reproduced independently.

## Root-cause correction: GPIO5 pinmux lifecycle

The churn ended when the hidden mutable precondition was measured directly.
After owned image download, register `0xF7EA8008` read `0x0038D249`. Donor
analysis had already recorded that GPIO5 is switched for manual image-download
chip select and restored afterward, but earlier work incorrectly dismissed the
write because download and the unsolicited boot event still worked.

Setting only GPIO5's `0x01000000` function bit produced `0x0138D249`. The
unchanged, detached, no-deferral service immediately returned
`EVENT_DSP_VERSION=0.0.64.58` and completed Mic-Mute in both directions.
Forcing the bad value, then starting the fixed service, proved the service
itself performs the correction.

The full lifecycle is now:

1. acquire `/run/reinvoke/pinmux.lock`;
2. read-modify-write only GPIO5's function bit clear for download mode;
3. download all 40,121 transfers;
4. read-modify-write only GPIO5's function bit set for message mode;
5. verify readback before starting the message pump.

MCU GPIO3 uses the same lock and its `0x00200000` bit is preserved. Two
consecutive DSP generations starting from message mode each downloaded,
restored `0x0138D249`, received `EVENT_DSP_BOOTUP`, and completed `getVer`.
Persisted Mic-Mute restore also passed before readiness.

Timing, inter-byte pacing, response-turnaround, thread-affinity, synchronous vs
asynchronous response, and idle-deferral experiments were rejected and are not
part of the release candidate.
