---
title: Pre-NAND platform review
description: Functional and safety review findings for the complete RAM-only replacement platform
ms.date: 2026-09-04
ms.topic: concept
---

## Status

Review is 98 percent complete. No high-severity source-level blockers remain.
The complete host and race suites pass, service fault injection preserves
microphone privacy, and the final BlueALSA pair is reproducible. Final review
waits for accepted-image cold-boot validation, the pairing-agent generation
guard, and one attended playback run. WAMP setup interleaving remains tracked
as medium-priority hardening.

## Required review areas

* Logic and contract compatibility
* Error handling and restart behavior
* Concurrency and cross-process hardware ownership
* Mute-first and NAND-read-only safety
* Reproducible build and cold-boot acceptance evidence

## Iteration 1 findings

1. High: a missing `/opt/reinvoke/etc/runtime.conf` would terminate PID 1
   because `.` is a POSIX special built-in. The bundle guard now requires the
   file and validates its required values before sourcing.
2. High: the 2016 target ADB daemon does not propagate remote shell exit codes,
   so the host collector could report success for failed device checks. The
   collector now parses the required `SUMMARY failures=N` record.
3. Medium: synchronous BlueALSA subprocesses blocked the unbuffered MCU
   interrupt consumer. Rotary work now uses a bounded coalescing worker while
   WAMP publication remains immediate.
4. Medium: the Bluetooth bootstrap process survived shutdown. Shutdown now
   stops it first, and every bootstrap wait exits when the shutdown marker
   appears.
5. Medium: a service could start between the supervisor's shutdown check and
   PID-file write. The supervisor now kills such a child immediately, and
   shutdown repeats the mute-first stop pass after 100 ms.

Networkd now uses the same generic supervisor and PID-file contract as the
other services. WAMP invocations run independently so a bounded media backend
operation cannot block MCU status or mute calls.

## Iteration 2 findings

1. High: concurrent WAMP requests could allocate the same request ID. Request
   allocation is now mutex-protected.
2. Medium: initial and reopened Bluetooth pairing durations were inverted.
   Separate validated durations now drive the correct windows.
3. Medium: rotary coalescing could lose work during a worker handoff. The
   pending-step transition is now synchronized and race-tested.

## Iteration 3 findings

1. Critical: ALSA reports the playback worker thread ID, but the first lease
   wrote the process ID. The patch now writes `SYS_gettid`, and the policy
   verifies that exact thread through `/proc`.
2. High: reused BlueALSA worker storage could inherit `lease_active` and
   underflow the lease reference count. Worker initialization and guarded
   decrement now fail closed.
3. High: PID 1 could defer its shutdown trap for an hour while waiting on
   `sleep 3600`. One-second waits now bound signal response.
4. Medium: shutdown signaled audio producers before MCU mute completion. It
   now stops and waits up to five seconds for the MCU policy owner first.
5. Medium: service logs could consume unbounded RAM. BusyBox syslog now rotates
   at 256 KiB with one backup, and `/dev/kmsg` is the bounded fallback.
6. Medium: networkd could start before syslog and lose degraded-mode
   diagnostics. Logger startup now precedes networkd.
7. Medium: DSP boot acceptance depended on a one-shot rotating log entry. The
   DSP process now owns a volatile boot marker cleared on every process start.
8. High: legacy ADB CRLF and exit semantics could reject successful acceptance
   or discard failure evidence. The collector always gathers evidence and
   parses an unanchored machine summary.
9. Medium: MCU and DSP WAMP frame readers could leak across reconnect errors.
   Per-session cancellation, connection close, and reader joins now cover every
   return path.
10. Medium: WAMP mute toggle used a split read-modify-write. It now uses the
    existing atomic BlueALSA operation shared with the physical button path.
11. Medium: the Bluetooth bootstrap PID file could outlive its process and
    later target a reused PID. Startup now handshakes PID publication and
    removes the file through exit and signal traps.

Independent final re-review found no remaining high-confidence source defects
in the playback lease, GPIO edge path, logging, mute-first shutdown, WAMP
lifecycle, or acceptance collector.

## Hardware review update

The rebuilt MCU service was tested on the v9 RAM runtime after the donor
pinmux correction. GPIO3 read high, the pinmux register read
`0x0038D249`, and a passive WAMP monitor captured repeated `volumeup` and
`volumedown` publications during attended rotations.

### Live audio and controls acceptance

1. Multi-device Bluetooth lock-in
   * Target device Bluetooth BD_ADDR was anchored to `D8:F7:10:C1:46:E9`
     directly in `tools/usb-boot/local.conf`. This strictly prevents host BlueZ
     from accidentally connecting to nearby stock Invoke units.
2. Rotary performance
   * In-memory fast-path rotary volume was validated during live A2DP streaming.
     Operator confirmed: "rotary feels much better!" with low latency and smooth
     attenuation.
3. Audio dropouts and clicks
   * Root-caused momentary clicks to the DAC/amplifier mute policy flapping on
     transient ALSA buffer underruns (`XRUN`).
   * Implemented a 1.5-second holdoff delay in `tools/mcu-interface/playback_policy.go`
     to keep the amplifier unmuted across transient buffer underruns while
     maintaining instant unmute on stream start.
4. Mic-Mute privacy and LED ring
   * Verified the rear button is strictly the Microphone Privacy switch (DSP
     opcode `0x09` on `com.harman.dsp.micMute`), not speaker mute.
   * Captured the donor `com.harman.ledOff` implementation and recovered its
     exact 41-byte packet: opcode `0x0e`, first-chunk flag `0x01`, and three
     zero frames.
   * The operator confirmed that Mic-Mute now turns the red ring on and off.
5. Toolchain standards
   * Standardized on the pinned repository Go toolchain at
     `../reinvoke-archive/toolchains/ubuntu-go-1.18.1/extracted/usr/lib/go-1.18` via
     `tools/mcu-interface/build.sh` and `test.sh`. All 40 unit tests pass.

## Iteration 6 findings (host audit, 2026-09-04)

### Retracted: the lease patch is present

An initial audit concluded that no archived binary carried the playback-lease
patch. That conclusion was wrong and is retracted. The scan targeted
`bin/bluealsa`, but the patch modifies `utils/aplay/aplay.c`, which builds
`bluealsa-aplay`. Rescanning the correct target confirms:

* `reinvoke-native-runtime-v9-20260904/bin/bluealsa-aplay` and its independent
  `-repro` counterpart both contain `REINVOKE_PLAYBACK_LEASE` and the lease
  diagnostic strings, and are byte-identical at SHA-256 `4c997821...`.
* That digest is exactly `BLUEALSA_APLAY_SHA256` in the packaging gate.

The lease mechanism is therefore intact, reproducible, and correctly pinned.
The lesson recorded for future audits is to resolve a patch to the binary its
target file builds before drawing conclusions from a symbol scan.

### Resolved: the generation split is intentional

The packaging gate pins `bluetoothd`, `bluealsa`, and `bluealsa-cli` to the
v1 artifact set and `bluealsa-aplay` to v9. This is not a defect. Comparing
v8 to v9 shows `bluealsa-aplay` as the only binary that changed; the three
daemon-side binaries are unchanged across v8 and v9. The v1 digests and the
v8/v9 daemon digests differ only because the v1 set is unstripped
(4,124,528 bytes) while later sets are stripped (3,177,368 bytes).

Reading the gate as a whole, it pins one coherent Bluetooth stack: the stock
upstream daemon trio plus the single patched consumer that must carry the
lease. No mixed-generation risk exists.

### Confirmed gap: upstream sources were never archived

`sources/` held only `community` and `harman`; there was no acquisition
record for BlueZ or bluez-alsa. The binaries originated from an SDK build
path whose inputs were not retained, so the patched `bluealsa-aplay` was not
rebuildable from archived material. This was a genuine reproducibility gap
rather than a correctness defect, and it has now been closed.

### Upstream acquisition

Pinned sources are archived under `sources/upstream/` at Tier 2:

* `bluez-alsa-4.0.0.tar.gz`, SHA-256
  `ce5e060e61669d61d44f5f9bad34a7b88378376e9d49d31482406a68127a6b29`,
  matching the `v4.0.0` string in the shipped binaries. MIT licensed.
* `bluez-5.55.tar.xz`, SHA-256
  `8863717113c4897e2ad3271fc808ea245319e6fd95eed2e934fae8e0894e9b88`,
  matching the `5.55` string in the shipped `bluetoothd`. Verified as a good
  GPG signature from the BlueZ maintainer key `E932 D120 BC2A EC44 4E55
  8F01 06CA 9F5D 1DCF 2659`.

Neither tarball enters Git. They are build-time inputs held in archive
storage, consistent with the documented tier policy.

### Packaging gate reproducibility

Every gate constant in `build-native-runtime.sh` was checked against archived
artifacts and, where source exists, against a fresh local build.

| Binary | Gate status |
|---|---|
| `reinvoke-mcu-interface` | Rebuilt from source to the exact digest `c9102b23...`, byte-identical across two builds |
| `reinvoke-dsp-interface` | Rebuilt from source to the exact digest `f5b36cc3...` |
| `bluealsa-aplay` | Gate digest matches the v9 artifact; lease patch confirmed present; two archived builds byte-identical |
| `bluealsa`, `bluealsa-cli`, `bluetoothd` | Gate digests match the v1 artifact set |
| `hci-init` | Gate digest matches the v1 artifact set |
| `bluez-pairing-agent` | Repinned to `faaba0eb...`, rebuilt from source, byte-identical across two builds |

The MCU result is the important one. It closes the packaging gate that
iteration 5 left open: the digest was recorded after the pinmux correction but
never reproduced. It now rebuilds deterministically from committed source.

### Resolved: pairing agent digest repinned

`PAIRING_AGENT_SHA256` was `ae60d800...`, and no archived artifact carried that
digest. Four different variants existed across artifact sets, none matching. A
fresh build from `tools/control/bluez-pairing-agent.c` against the archived
dbus-1.12.20 produces `faaba0eb...` deterministically across two builds, with
a functionally identical D-Bus surface and the same A2DP/AVRCP UUID allowlist.

The old gate digest referred to a binary built by a toolchain that was never
recorded, so it could not be reproduced or re-verified by anyone. On explicit
user decision, the gate was repinned to the reproducible build.

What this buys: every binary in the runtime is now rebuildable from committed
source plus archived, checksummed, GPG-verified upstream inputs. Nothing in the
stack depends on an artifact of unknown origin.

What it costs: the `ae60d800...` value was an original attestation, and it is
now superseded. It is preserved in `metadata/P1-049.json` under
`superseded_sha256` so the substitution stays auditable rather than silent.

Toolchain of record: `arm-linux-gnueabihf-gcc` 11.4.0, flags
`-std=c11 -O2 -Wall -Wextra -Werror -static`. The rebuilt binary and its build
notes are archived under `build/pairing-agent-rebuild-20260904/`.

## Iteration 7: fully reproducible runtime, v10

With the pairing-agent pin resolved, the whole stack was repackaged from
verified inputs. Recorded as [P1-051](../../../metadata/P1-051.json).

### Result

| Stage | Outcome |
|---|---|
| Owned Go binaries | All five rebuild from source to their exact gate digests |
| Pairing agent | Rebuilds to its repinned digest |
| Runtime directory | Built twice, byte-for-byte identical, manifest `449bf750...` |
| Initramfs | Built twice, byte-for-byte identical, `38a90212...`, 29,217,281 bytes |
| Host suites | MCU, DSP, provisioning, native platform harness all pass |

This is the first image where nothing depends on an artifact of unknown
origin. Every binary traces to committed source plus archived, checksummed,
GPG-verified upstream inputs.

### A gate caught a real mistake

The first packaging attempt used `extracted/phase3/stockroot/rootfs` as the
donor. It failed on the LED animation manifest: that tree carries 28 lights
assets, while the reviewed set carries 8. The correct donor is
`extracted/ota2/83_members/rootfs`.

Worth recording plainly. The donor rootfs was chosen from memory of an earlier
note rather than resolved from the gate, and the gate is the only reason a
wrong-donor image did not get built and booted. The lesson generalises: resolve
each input from its own pinned digest instead of from recollection. Every other
input in this run was resolved by digest search, which is why nothing else
needed correcting.

### Reproduction command inputs

- donor rootfs `extracted/ota2/83_members/rootfs`
- source initramfs `extracted/ota2/OTA2/82_IMAGE`
- kernel modules `build/artifacts/invoke-kernel-gcc49-audio-sd8887-spi-timeout-20260903/modules`
- Bluetooth daemon trio and `hci-init` from `reinvoke-native-runtime-20260903/bin`
- `bluealsa-aplay` from `reinvoke-native-runtime-v9-repro-20260904/bin`

### Not yet done

The v10 image has never been booted. Every remaining acceptance item is
hardware-gated and needs the device in yellow mode.

## Iteration 8 — active-PCM mute test on hardware

The v10 image has now been booted and the first genuinely new hardware result is
in. Superseding the closing line of Iteration 7.

### What was tested

The attended active-PCM mute test, which had been blocked for several iterations
because no A2DP source was ever connected and so BlueALSA never held an active
PCM. With the host adapter allowlisted into the image and paired, a 20-second
44.1 kHz stereo tone was streamed from the host to the device's A2DP sink while
the amplifier state was observed.

### Result: pass

The playback lease behaves exactly as designed.

- During the stream, `/run/reinvoke/bluealsa-playback-active` contained `1754`
  and `/proc/asound/card1/pcm0p/sub0/status` reported `state: RUNNING` with
  `owner_pid: 1754`. The lease PID and the ALSA owner PID matched, which is the
  condition `playbackPolicy` requires before it will treat playback as real.
- The MCU tracked the transitions accurately, logging `physical playback path
  active=true` on each stream start and `active=false` on each stop.
- When the stream ended the lease file was unlinked and the PCM closed.

Most importantly, the amplifier never unmuted. The only unmute-related line in
the entire boot log is the startup line `hardware initialized muted; WAMP unmute
policy=false`. Real audio flowed through a real PCM with a valid lease and the
policy still declined to unmute, because no unmute has been authorised.

### A false alarm worth recording

An earlier reading of this test appeared to show a missing lease file and I
briefly believed the producer side was misconfigured. That was wrong, and the
error was mine: the tone had already finished playing by the time the lease was
read, so the absent file was correct behaviour rather than a defect.

The detour did confirm the wiring, which is worth keeping. The lease is produced
by `bluealsa-aplay` reading the `REINVOKE_PLAYBACK_LEASE` environment variable
set in `/init`, not from a command-line flag, and the running process was
verified to have inherited it. The `--playback-lease` flag on
`reinvoke-mcu-interface` is the consumer side. Producer and consumer are
configured independently and both were correct.

The general lesson is to sample a transient signal while it is live. A single
observation taken after the event proves nothing about the mechanism.

### Acceptance status

21 checks run, 1 failure, unchanged from the previous boot: `dsp.boot_event`.
Still the absent `EVENT_DSP_BOOTUP` hardware event on a warm-reset boot, with
the DSP otherwise healthy. The cold-versus-warm hypothesis remains untested and
should be checked on the next cold boot before being treated as a defect.

## Iteration 9 — physical controls

The buttons and rotary encoder had never been exercised on any native image, so
the whole input chain was unproven on hardware. It was tested attended, with the
operator pressing each control in turn.

Pressing a control is safe regardless of outcome. `micmute` reaches only the
BlueALSA software mute on the incoming A2DP stream, and the hardware amplifier
path is gated behind `AllowPlaybackUnmute`, which is false. No input event can
unmute the amplifier.

### Result: the input chain works

All five inputs decoded and published correctly through the MCU to the WAMP
router. Action, Bluetooth and Mic-Mute arrived on `com.harman.vui.keypress`, and
both rotary directions arrived on `com.harman.test.inputEvent` carrying step
values from 1 through 4. The stepped encoding is genuine velocity data: a slow
rotation produced steps of 1 and 2, a faster one produced 3 and 4. Acceptance
was rerun afterwards and still showed the same single `dsp.boot_event` failure,
so nothing regressed.

One press was misidentified at the time. The operator pressed the Bluetooth
button briefly but the MCU reported `bluetooth-long`, which is a hardware or
firmware timing threshold rather than a fault in our code. Worth noting because
`bluetooth-long` is the event that restarts the pairing window, so a short press
can reopen pairing unintentionally.

### A real finding: AVRCP absolute volume is unsupported by the peer

Every `volumedown` event logged `Couldn't set BT device volume:
UnknownProperty: No such property 'Volume'`, while `volumeup` did not. The
asymmetry looked like a directional bug in `AdjustVolume`, and it is not.

The volume was already at 95 and climbing, so each `volumeup` clamped to the
same value and BlueALSA had no change to propagate. Only `volumedown` produced
an actual change, and it was the propagation that failed. Setting the volume
manually to 60 and to 0 reproduced the same warning, which rules out direction
as a factor.

The cause is on the peer. The host exposes no `org.bluez.MediaTransport1`
interface for the device, so it does not implement AVRCP absolute volume
control. BlueALSA correctly applies the volume locally through SoftVolume, which
is enabled, and then warns that it could not also inform the peer.

This is benign for our purposes. Local volume control works, the warning is
purely about remote synchronisation, and the operator is the one holding both
ends. It should not be treated as a defect in the runtime.

### Defect: the Mic-Mute button does not mute the microphone

The button is the stock far-field microphone mute, a privacy control whose
purpose is to cut the mic array so the speaker cannot listen. The donor
boundary documentation records code `0x04` as "Microphone short press", and the
DSP exposes the matching control as `com.harman.dsp.micMute`, opcode `0x09`,
with the DSP reporting `EVENT_MIC_MUTE` in return.

Our runtime routes it somewhere else. `blueALSAController.Apply` intercepts the
`micmute` event and calls `ToggleMuted`, which resolves the PCM through
`selectBlueALSAPCM` against the allowlisted peer and lands on
`.../a2dpsnk/source`, the incoming A2DP music stream. `com.harman.dsp.micMute`
is registered by the DSP interface but never called by anything.

So pressing Mic-Mute currently mutes the music rather than the microphone. For a
privacy control that is the wrong failure direction: someone pressing it would
reasonably believe the microphone had been cut when it has not.

Three separate mute concepts exist here and this document previously conflated
the first two:

- The hardware speaker mute, held by `ampMuteMask` and `dacMuteMask` in
  `controller.go`. This is the safety-critical one, asserted at boot and
  unreleasable while `AllowPlaybackUnmute` is false.
- The BlueALSA software mute on the A2DP PCM, which attenuates incoming music.
  This is what the button currently reaches.
- The microphone mute in the DSP, which nothing currently drives.

This is a correctness defect rather than a safety one, since the speaker cannot
produce sound in either case, and it should be fixed before the button is
presented to anyone as a microphone control. The fix is to route `micmute` to
`com.harman.dsp.micMute` instead of to the BlueALSA PCM. It is deliberately not
being made in this iteration, because the DSP path deserves its own attended
test with the microphone capture running to confirm the mute actually takes
effect in the captured audio.

### The toggle mechanism itself works

Whatever it ought to be wired to, the input path is sound. The first Mic-Mute
press was observed only after the fact, and since the starting state had not
been recorded it proved nothing. It was retested properly: the software mute was
cleared to `Muted: L: N R: N` first, the operator then pressed the button once,
and the PCM afterwards read `Muted: L: Y R: Y` with `Volume: L: 95 R: 95`
unchanged. The keypress publication is in the log at the matching time.

That establishes the full path from the physical button through the MCU and the
BlueALSA controller to the PCM property, and confirms mute is a separate flag
that does not disturb the volume setting.

The general point is the same one from Iteration 8: a single observation taken
after an event proves nothing about a toggle. Establish the starting state, act,
then observe.

## Iteration 10: first audible playback, DSP command path

Operator confirmed audible 440 Hz playback over A2DP on the native platform,
and confirmed rotary volume up and down by ear during live playback.

Root cause of the silence: `reinvoke-dsp-interface` was launched without
`-allow-state-changing-procedures`, so `safeOnly` was true and `dispatch`
refused every DSP command before transmission. The DSP therefore never
received `com.harman.dsp.volumeSet` and never emitted `EVENT_DSP_BOOTUP`.
One defect, two symptoms: silence and the `dsp.boot_event` acceptance failure.
Verified by restarting the service by hand with the flag: `/run/reinvoke/dsp-booted`
appeared and `volumeSet` was accepted and queued as `04 1e`.

Enabling the flag exposed a latent defect the safe mode had masked. `transmit`
called `waitReady()` once, then retried `receive()` without re-checking Ready.
A DSP that deasserts Ready while computing returns an all-zero header, so every
retry clocked a silent bus and the link died on the first command it was ever
sent (`header=0000000000`). Fixed by re-waiting for Ready between retries.
`TestTransmitWaitsForReadyBetweenRetries` was confirmed to fail without the fix.

LED ring stays lit after `bluetooth-long` because donor animations carry no
trailing blank frame and nothing clears the ring. `clearLEDs` is added but not
yet wired, so `runLEDAnimation` still matches the recovered donor contract.

## Iteration 11: rotary latency elimination, stock volume recovery, and mic-mute routing

### Stock volume model recovered from donor firmware

Reverse engineering donor `audio-ui` (`_ZN3aui13VolumeManager*`, `volume_to_alsa`,
and `volume_from_alsa`) reveals:
- Volume is represented as an integer percentage from 0 through 100.
- `volume_to_alsa(vol, max_alsa)` computes `(vol * max_alsa) / 100`.
- ALSA softvol controls (`system`, `music`, `call`) are updated in memory without shelling out.

### Root cause of rotary sluggishness and resolution

The previous `blueALSAController` executed three sequential `exec.Command` child processes
(`bluealsa-cli list-pcms`, `info`, and `volume`) on every single rotary step (~60-100 ms per tick).
When spun rapidly, process execution queues caused noticeable lag and stepped latency.

Resolution:
- Added in-memory caching of the active `pcmPath`, `cachedVolume`, `cachedMuted`, and `cachedValid` state.
- `AdjustVolume` and `SetVolume` now execute on a single fast path: computing the clamped target volume and issuing at most a single `volume` command, bypassing `list-pcms` and `info`.
- When volume reaches minimum (0) or maximum (100), redundant command execution is suppressed.
- On command failure, the cache is automatically invalidated and refreshed via `pcmSnapshotLocked`.
- Verified with unit test `TestBlueALSAControllerCachesPCMPathForLowLatency`.

### Mic-Mute privacy routing

- Decoupled `micmute` from `blueALSAController.Apply` so rotary and media controls no longer mute music playback on mic button press.
- Implemented WAMP `call` on `wampConnection` (`wampCall = 48`).
- Routed `micmute` events in `wampService` to invoke `com.harman.dsp.micMute` (DSP opcode `0x09`) with argument `1` (muted) or `0` (unmuted), alongside WAMP keypress publication.
- Wired LED state: activates `L_108_c_error` (red ring) while muted and clears when unmuted.

### LED ring auto-clear

- Updated `ledPlayer.Start` to automatically invoke `clearLEDs` when non-repeating animations finish, ensuring the ring does not remain stuck on the final frame after pairing or touch events.

## Iteration 13: playback continuity and privacy fail-closed

The first attended 45- and 90-second streams contained repeatable clicks,
stutters, pauses, and occasional noise. The source was one continuous WAV from
the Mac mini, not multiple playbacks. Kernel `XXXRUNN` records matched operator
stopwatch reports, while one-second ALSA state sampling missed the short
failures.

An HCI capture found continuous sequence numbers but periodic arrival stalls up
to 742 ms and 5.885 seconds of timestamp-confirmed missing PCM across one
94-second transfer. The source later resumed with advanced RTP timestamps, so
the sink could not recover the omitted audio by replaying packets.

Offline disassembly of the final HK Bluetooth process recovered its ALSA
contract:

* Open the `music` PCM.
* Accumulate complete 512-frame periods.
* Use an 8192-frame hardware buffer.
* Retry partial writes and call `snd_pcm_recover` without a 50 ms sleep.

BlueALSA 4.0 additionally needed sink-side transport protection:

* The player now maintains a two-second decoded PCM reservoir and closes a
  drained stream after 100 ms.
* The SBC decoder requests `missing_pcm_frames` from RTP synchronization and
  inserts bounded silence for timestamp-confirmed gaps.
* The player uses the donor-equivalent 170 ms buffer and 20 ms requested period,
  which the hardware resolves to 8192 and 512 frames.

The final static ARM binaries are reproducible:

* `bluealsa-aplay`: SHA-256 `59dd5985...`
* `bluealsa`: SHA-256 `62a3c8c4...`

A 96-second adaptive-source machine run and a five-minute conservative-source
soak each completed with one PCM open, zero XRUNs, no mid-stream reopen, a clean
close, and lease removal. Attended listening remains required.

The reviewed v12 package is deterministic. Two runtime builds have identical
trees and manifest SHA-256 `dc07e3c3...`. Two 29,226,746-byte initramfs builds
are byte-identical at SHA-256 `110376e0...`.

Mic-Mute also exposed a separate privacy defect. The MCU WAMP session omitted
the `caller` role, so Bonefish rejected every DSP call. After adding that role,
the DSP service still acknowledged queueing before the hardware transaction
failed. DSP commands now carry tracked completion back to WAMP, and the MCU
changes its privacy LED only after success. Failed commands return an explicit
error and leave the LED unchanged. The DSP response path still returns zero
headers in the current heavily warm-reset state, so microphone privacy is not
accepted until a clean power cycle and capture correlation pass.

## Iteration 14: fault injection and privacy ownership

Repeated physical trials separated touch-controller behavior from software
latency. When a press produced an MCU frame, the DSP transition completed in
the same second. Some attempts produced no frame under both the owned and stock
Harman MCU services. The accepted software cannot synthesize an event that the
companion MCU never reports.

Functional review identified and corrected the following privacy failures:

* Duplicate Mic-Mute frames were visible to WAMP subscribers
* A router outage could delay or discard privacy input
* A daemon restart lost the confirmed mute state
* Indeterminate unmute outcomes could restore a false red indicator
* The raw DSP Mic-Mute procedure allowed callers to bypass state and LED
  ownership
* DSP reset could invalidate mute before MCU reconciliation
* LED I/O could delay the safety-critical DSP mute command
* The DSP message pump could stall or fail without terminating the process
* An unrelated boot frame could be consumed while correlating a command
  response

The accepted design has one process-wide microphone privacy owner in the MCU
service. Physical events reach it before WAMP publication, required mute state
is atomically stored in RAM, unresolved mute retries continue independently of
the router, and unmute failures are immediately remuted. DSP opcode `0x09` is
available only through a mode-0600 Unix socket. The public compatibility
procedure is registered by the MCU service and therefore cannot bypass
persistence or LED handling.

DSP startup now accepts WAMP commands while withholding readiness. It preserves
valid unrelated frames, waits for `EVENT_DSP_BOOTUP`, starts the private socket,
re-reads privacy state under the socket dispatch mutex, restores mute, and only
then publishes `com.reinvoke.dsp.session` readiness. Live logs proved the
accepted order: boot event, restored mute, readiness publication, then MCU
confirmation.

The fault-injection sequence restarted Bonefish, MCU, DSP, BlueALSA, and the
player. The RAM marker remained `muted`, the root-only socket returned, and MCU
and DSP health calls succeeded. The final capture contained 1,573,376 samples,
all zero. Unit tests, race tests, the complete native-platform suite, and
reproducible builds passed.

One medium-priority review item remains: WAMP registration helpers assume setup
responses are not interleaved with invocations. The router behaved
deterministically in every live run, but a future hardening iteration should
use one correlated reader during registration.

The accepted image is not yet cold-boot accepted. The new pairing-agent
generation guard and complete startup order require validation from that image
before the review phase can close.
