---
title: Microphone capture review
description: Review findings for the privacy-gated microphone capture owner
---

## Status

The initial plan review completed before implementation. Blocking findings were
folded into the plan.

## Findings

### Blocking: stale confirmed state can reverse a pending mute

The first plan proposed restoring the last confirmed unmuted state after a
capture synchronization. If a mute request had failed after recording
`desired=muted` and `unknown=true`, that would have silently undone the user's
request. Synchronization now preserves logical policy and restores unmute only
when entry state was known and no mute was outstanding.

### Blocking: inotify is not a revocation boundary

A filesystem watcher can run after DSP confirmation, and closing a Unix stream
cannot retract bytes already queued to a receiver. The MCU now synchronously
fences capture delivery before requesting DSP mute. State-file watching remains
a fail-closed secondary signal.

### Blocking: MCU authority loss could leave delivery enabled

An unchanged `unmuted` file and DSP socket can survive an MCU crash. Capture
authorization now binds to a random process-lifetime MCU authority epoch.
Authority loss closes clients and blocks delivery until a fresh synchronization
completes.

### Important: helper ownership is operational, not protection from root

The owner-supervised `arecord` child does not violate the single-owner model,
but root consumers could still open the raw node whenever it is free. The
threat model now states that local root is trusted and that exclusivity is
proven only while the one hardware substream is held open.

### Important: mono is a donor-left-channel view

The donor labels the left channel for voice recognition, but active beamforming
and AEC are unproven. Documentation must use that exact wording and must not
claim algorithmic processing that has not been measured.

## Remediation review

The security reviewer re-read the implementation after remediation and reported
no remaining high-confidence privacy vulnerabilities.

The final implementation adds:

* `DRAIN`/`DRAINED` after confirmed DSP mute;
* 64 consecutive all-zero periods before any restore/allow;
* requested-policy versioning that prevents a racing mute from being restored
  to unmute;
* terminal generation failure for invalid/missing privacy state;
* fresh nonzero delivery generation on every allow;
* MCU PID/start-time and random-epoch fencing;
* DSP PID/start-time and socket-inode fencing; and
* process-group cleanup for the capture helper.

The reviewer explicitly confirmed that the original three blocking findings
were closed. The only compilation issue it observed was a nested test function
introduced during development; that test file was corrected and the complete
suite now passes repeatedly under the race detector.
