---
title: Pre-NAND platform review
description: Functional and safety review findings for the complete RAM-only replacement platform
ms.date: 2026-09-04
ms.topic: concept
---

## Status

Review is 90 percent complete. Three review rounds found lifecycle, safety,
concurrency, and acceptance defects. Every source-level finding is fixed and
the complete host and race suites pass. The live rotary gate now passes; final
review waits for packaged v9 hardware acceptance, audible/acoustic evidence,
and soak results.

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
`volumedown` publications during attended rotations. A temporary hot-swap
acceptance run correctly reported one runtime-hash failure because the live
binary differed from the packaged manifest; this is a packaging gate, not a
functional acceptance result.

The same donor-backed monitor captured a physical `micmute` keypress and a
`bluetooth-long` keypress. The latter reopened the bounded 300-second
allowlisted pairing window, with the operator observing the top white pairing
indicator. The Mic-Mute event was valid, but BlueALSA had no active PCM, so
the software mute state could not be exercised in that subtest.

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
