---
title: Microphone capture implementation plan
description: Plan for a privacy-gated DSP microphone capture owner
---

## Goal

Deliver the donor-designated DSP voice-recognition channel to local consumers
through one supported owner, while preserving the MCU privacy controller as the
sole authority.

## Architecture

### Capture owner

Add a supervised Go service, `reinvoke-mic-capture`, that:

* is the only packaged service allowed to open the capture PCM;
* starts a checksum-gated `arecord` helper with fixed native parameters;
* drains and discards stereo capture while privacy is blocked;
* extracts the left `S32_LE` channel without rate conversion;
* sends versioned, generation-fenced 256-frame packets over a root-only Unix
  stream socket;
* gives every client a small bounded queue and disconnects lagging clients;
* closes every client and clears every queue on mute, capture failure, or DSP
  generation change; and
* restarts capture only after a new privacy synchronization succeeds.

### MCU privacy synchronization

Add two root-only private control surfaces:

* an MCU-owned synchronization socket used by the capture owner after ALSA
  configuration; and
* a capture-owned fence socket used by the MCU before every mute transition.

The MCU creates a random process-lifetime privacy authority epoch in mode-0600
RAM state. A capture generation is authorized only while that exact MCU epoch,
the DSP socket generation, and the configured PCM generation all remain live.
Loss or change of any one immediately revokes delivery.

The capture owner sends `SYNC` only after ALSA `hw_params` has completed.

Under the existing privacy mutex the MCU owner:

1. records the logical requested policy, including `desired` and `unknown`,
   rather than only the last confirmed hardware state;
2. synchronously fences the capture owner and waits until all fan-out is
   blocked and client connections are closed;
3. forces mute and waits for DSP confirmation;
4. restores unmute only when entry policy explicitly permitted it, hardware
   state was known, and no mute was outstanding;
5. waits for the restore confirmation; and
6. returns final confirmed state plus the MCU authority epoch.

This preserves physical-button serialization, state persistence, retry, and the
red indicator. A button event arriving during synchronization waits behind the
privacy mutex and then applies to the final confirmed state; it is not silently
folded into a stale pre-transaction snapshot. The capture service never sends
DSP opcode `0x09` directly.

For ordinary physical/API mute, the MCU must fence delivery before it writes
`muted` state or sends DSP opcode `0x09`. If the capture owner does not
acknowledge within a short bound, the MCU verifies and terminates the configured
capture-owner process before proceeding with hardware mute. A restarted capture
owner begins blocked, so failure of the gate cannot leave an old authorized
stream running.

### Privacy revocation

The synchronous MCU fence is the revocation authority. The capture owner also
watches the parent directory of `/run/reinvoke/microphone-state` with inotify
because the state is replaced by atomic rename. The file watcher is a
fail-closed secondary signal, not the mechanism that proves revocation.

* The MCU fence blocks fan-out, invalidates the generation, clears every
  user-space queue, and closes every client before the DSP mute request.
* `muted` independently keeps delivery blocked. It is written before the DSP
  command, giving a second conservative signal.
* `unmuted` enables a new delivery generation only after a successful
  post-configuration synchronization for the current DSP and MCU authority
  epochs.
* Missing, invalid, oversized, or unreadable state is treated as muted.

The protocol guarantee separates three boundaries:

1. **Producer fence:** no sample is placed in any consumer queue after the MCU
   receives the capture owner's `BLOCKED` acknowledgement.
2. **DSP confirmation:** the DSP route is confirmed muted only after the fence
   is complete.
3. **Consumer observation:** bytes already copied into a consumer's process or
   kernel receive queue before the fence are irrevocable. The service minimizes
   this backlog with shallow queues and closes the connection at the fence.

Acceptance measures zero newly delivered records after DSP confirmation and
includes an intentionally backlogged consumer so a dead or empty capture path
cannot pass accidentally.

### DSP restart

Track the DSP PID record, mic-control socket device/inode, and MCU authority
epoch. A change or disappearance:

1. blocks delivery;
2. closes clients;
3. stops the capture helper;
4. waits for the new private DSP socket;
5. starts and configures capture; and
6. runs the MCU privacy synchronization before opening a new generation.

Successful MCU synchronization is the readiness proof because the MCU receives
the DSP event response through the new generation's private control socket.
PID and inode are restart diagnostics, not authorization tokens; the random MCU
epoch binds authorization to one live privacy-owner process.

## Protocol

The first connection record is a fixed binary stream header:

* magic and version;
* header length;
* delivery generation;
* sample rate 48,000;
* one channel;
* `S32_LE` format identifier; and
* 256 frames per packet.

Every packet carries:

* generation;
* monotonically increasing sequence;
* monotonic capture timestamp; and
* 1,024 bytes of mono PCM.

Generation changes require a reconnect. A Unix stream does not preserve write
boundaries, so clients must use exact-length reads for the connection header,
record header, and payload. EOF mid-record discards that incomplete record.
Consumers must not infer continuity across mute, capture restart, or DSP
restart.

## Security and failure behavior

* Runtime directory mode 0700; data and synchronization sockets mode 0600.
* Validate path ownership, type, parent filesystem, and peer credentials.
* Stale-socket recovery uses a lifecycle lock and inode recheck.
* The capture helper path and `libasound` are checksum-gated.
* A helper or ALSA failure closes all clients and triggers bounded restart.
* A privacy synchronization failure leaves delivery blocked and retries.
* A slow consumer cannot block capture or another consumer.
* Shutdown closes clients before stopping the helper.
* The packaged capture helper is an operational child of the owner. Root
  programs remain inside the trusted boundary and can bypass DAC permissions;
  single ownership is enforced while the PCM is open because the hardware
  exposes one capture substream. Protection from hostile root is out of scope.

## Validation

### Host tests

* Wire header and mono-channel extraction.
* No delivery before initial privacy synchronization.
* A synchronous MCU mute fence closes an intentionally backlogged client before
  the DSP confirmation is allowed to complete.
* `muted` independently closes clients and clears queued frames.
* Invalid/missing state is fail-closed.
* Unmute does not serve data until synchronization succeeds.
* PCM helper restart and DSP generation change invalidate clients.
* MCU process death or authority-epoch replacement invalidates clients.
* Slow-client queue overflow disconnects only that client.
* MCU synchronization never restores unmute when `desired`, `unknown`, or a
  newer request requires mute.
* A physical mute racing each transaction stage cannot become an unintended
  unmute.
* Tests fail when each corresponding gate is removed.
* `go test -race ./...` passes.

### Hardware acceptance

* A test consumer receives continuous mono 48 kHz `S32_LE`.
* Attended speech/tap is visible in the voice-recognition channel.
* Positive attended speech/tap controls pass before and after mute, so an empty
  or dead path cannot satisfy the zero test.
* Physical Mic-Mute closes a deliberately backlogged client; an automatic
  reconnecting consumer records zero new records during the confirmed muted
  interval.
* Physical unmute resumes a new generation.
* Killing `reinvoke-dsp-interface` closes delivery; after supervised restart,
  privacy is synchronized and a new generation resumes.
* Killing `reinvoke-mcu-interface` closes delivery; it remains closed until a
  new authority epoch completes synchronization.
* A second direct open of the one hardware capture substream fails while the
  owner is active. This proves runtime exclusivity, not protection from hostile
  root while the owner is stopped.
* The existing native acceptance collector remains green.

## Delivery order

1. MCU privacy synchronization API and tests.
2. Capture service protocol, client queues, and privacy state watcher.
3. Capture helper supervision and DSP generation fencing.
4. PID1/runtime packaging and checksum gates.
5. Host race/fault tests.
6. Functional and security review.
7. Reproducible candidate build.
8. Hardware acceptance.
