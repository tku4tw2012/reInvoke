---
title: Microphone capture implementation log
description: Iteration log for the privacy-gated microphone capture owner
---

## Status

Implementation and host validation are complete. Hardware acceptance is pending
one RAM-only cold boot of the packaged candidate.

## Iterations

### Iteration 1: source and transport selection

Research established that card 1, device 0 is the donor-designated DSP
microphone endpoint rather than an exposed seven-channel raw array. The donor
routes its left channel to voice recognition and its right channel to calls.
Active beamforming/AEC remains unproven.

A Unix stream was selected over shared memory and an ALSA plugin. It keeps one
process in operational ownership of the PCM, supports peer credentials and
generation fencing, and avoids a shared mapping that survives privacy
revocation.

### Iteration 2: initial owner and MCU authority protocol

Added:

* `reinvoke-mic-capture`, a static Go owner that supervises a fixed ALSA
  `arecord` child;
* a fixed binary stream protocol for mono 48 kHz `S32_LE`, using the donor's
  left voice-recognition channel;
* root-only data and privacy-authority Unix sockets;
* MCU peer authentication using UID, PID file, executable identity, and process
  start time;
* a random process-lifetime MCU authority epoch;
* DSP PID/start-time/socket-inode generation checks; and
* bounded per-client queues with lagging-client eviction.

The donor ALSA 1.1.0 helper was proven on the running RAM image. One second of
raw stdout was exactly 384,000 bytes, and the live PCM reported the required
stereo 48 kHz `S32_LE`, 256-frame period, 4,096-frame buffer geometry.

### Iteration 3: privacy review remediation

The first plan depended too heavily on an asynchronous state-file watcher.
Review found three blocking privacy gaps:

1. stale confirmed unmute could reverse a newer pending mute;
2. buffered pre-confirmation audio could be delivered after authorization; and
3. an MCU crash could leave capture authorized by stale state.

The implementation now:

* records logical requested privacy policy independently of the long DSP
  transaction mutex;
* synchronously fences every delivery writer before DSP mute;
* kills only the authenticated capture-owner process generation if fencing
  fails;
* drains 64 consecutive all-zero native periods after confirmed mute and before
  any unmute/allow;
* binds authorization to the live MCU PID/start time and random epoch;
* treats invalid state as a terminal generation failure; and
* creates a fresh delivery generation after every authorized unmute.

The source helper is killed as a process group. This was added after repeated
tests demonstrated that killing only a wrapper could leave a child holding
stdout open for 30 seconds.

### Iteration 4: build and packaging

The runtime builder now gates and installs:

* the owned capture service;
* donor ALSA 1.1.0 `arecord`; and
* donor `libasound.so.2`.

The runtime built twice byte-identically. The initramfs built under umask 022
and 077 with the same digest. Final hashes are recorded when the candidate is
archived after the source commit.

## Validation

* `tools/mcu-interface`: full `go test -race` passes, including deterministic
  policy-race, fence, drain, peer-identity, and stale-generation tests.
* `tools/mic-capture`: tests pass repeatedly under `-race`, including protocol
  framing, backlogged client fencing, slow-client isolation, process-group
  cleanup, invalid-state generation teardown, and exact MCU protocol order.
* The complete native host gate passes.
* Final security review reports no remaining high-confidence vulnerability.
* Hardware privacy, restart, and positive-audio acceptance remain pending.

### Iteration 5: whole-candidate hardening

The final candidate review found and corrected:

* a false-positive direct-PCM exclusivity check;
* stale client headers across redundant authorization;
* reconnect spin while capture is blocked;
* a nonfatal listener failure that could remove the service endpoint;
* an ALLOW race with a new mute request;
* stale queued requests that could mutate hardware;
* cancellation sequences that could erase a pending mute;
* helper descendants that could survive owner shutdown; and
* a drain counter that could ignore late nonzero input.

The capture protocol now uses an explicit 64-period all-zero drain while DSP
mute is confirmed, with a minimum 200 ms elapsed barrier. Any nonzero period
restarts the full drain count. Every terminal generation path fences and waits
all consumer writers before it stops the capture helper.

### Final reproducible candidate

The reviewed binaries and packages reproduce exactly:

* MCU privacy owner:
  `96b95f50dc2f31d28841aa7ee4a4c8d4cf4bbc9d0cc52a97e24e00bc50677889`
* capture owner:
  `32f8b403e9462b2a0a3e973d0e9b1a4f64ae2c630a33fd118c71dca597735e87`
* test client:
  `d48dc509fdcbd137537cf1278bad932236e4e8e58d19d14330bd26129dec0f91`
* runtime manifest:
  `117aa5e8cf17b87839231bae8f61ebdd64cf4cd106d583e8a57a3c61a99c3a38`
* initramfs:
  `7b0e23e0aea293be70e5d8fe40c0f199a5857d750a997ede87b1df6bd48694ee`

The runtime was built independently twice with identical trees and manifests.
The initramfs was built under umask 022 and 077 with identical bytes.

The candidate remains RAM-only and hardware acceptance is pending.
