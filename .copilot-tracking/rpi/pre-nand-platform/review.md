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
