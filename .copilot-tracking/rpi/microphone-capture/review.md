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

## Holistic candidate review

The whole-candidate review found four additional issues before boot:

1. The direct-PCM exclusivity check accepted any nonzero exit, so a successful
   capture killed by `timeout` could pass. It now requires exit status 1, the
   expected busy/resource diagnostic, and zero captured bytes.
2. A redundant `ALLOW` could change the generation while old clients retained
   the prior stream header. Every `ALLOW` now synchronously closes old clients
   before installing a fresh nonzero generation.
3. A reconnecting test client could spin on immediate EOF while muted. It now
   backs off 100 ms after every disconnected generation.
4. Audio-listener failure left the capture loop alive without a public socket.
   Listener failure now cancels the service context and is process-fatal so PID
   1 restarts the complete owner.

The same review identified two concurrency races in the MCU policy:

* policy validation was not linearized with `ALLOW`, so a mute could land
  between the final check and authorization; and
* an older queued request could mutate hardware after a newer request.

The MCU now holds the policy lock through `ALLOW` and rejects stale requests
before hardware mutation. The requested-policy implementation was then
redesigned as an ordered pending-request map with an applied base policy so one
or multiple canceled unmute calls cannot erase an older pending mute.

The final security pass found two last counterexamples:

* cancellation of an actual pending mute removed the mute; and
* a nonzero period received after the drain counter reached zero could be
  ignored while waiting for the minimum drain duration.

Canceled mute now remains pending, fences immediately, and schedules
process-lifetime reconciliation. Drain processing checks nonzero input before
the exhausted-count case and resets the full 64-period requirement.

All corresponding regressions run repeatedly under the Go race detector. The
security reviewer reports no remaining high-confidence privacy vulnerability.

## Accepted residual

Linux 3.8 has no pidfd. If a fenced capture owner does not respond, the MCU
verifies the peer UID, PID file, executable inode, and process start time
immediately before `SIGKILL`, but a theoretical PID-reuse race remains between
verification and signal delivery. This termination is defense in depth:
confirmed DSP mute still zeros the capture stream even if termination fails.
The limitation is explicit and does not weaken the stated confirmed-mute
boundary.

## Final review decision

The holistic reviewer found four candidate-level issues:

* the PCM exclusivity check accepted timeout as success;
* redundant `ALLOW` could change record generation without replacing clients;
* the reconnecting test client could spin on immediate EOF; and
* an audio-listener failure could leave a live capture loop without a public
  service socket.

All were fixed with bidirectional regression tests. The review then found an
ALLOW race, stale queued policy requests, canceled mute/unmute combinations,
terminal teardown ordering, and a drain-counter reset defect. Requested policy
was redesigned as an ordered pending-request map, policy validation is held
through ALLOW, every generation exit fences clients before helper shutdown, and
nonzero input always resets the drain.

The legacy ADB collector was also corrected to parse explicit remote status
sentinels; a fake ADB that always exits zero proves remote failures are retained.
The parser accepts both LF and legacy CRLF sentinels.

Two final cancellation combinations were added after review:

* canceled mute remains pending, fences immediately, and is reconciled with the
  process-lifetime context; and
* cancellation after a superseded unmute rollback reconciles any unknown or
  inconsistent hardware state rather than leaving capture fenced indefinitely.

The last protocol review found that an authority could send `STATE UNMUTED`
followed directly by `ALLOW` without the mandatory drain, and that a rejected
`ALLOW` response left the authority connection alive. The owner now treats
drain completion as a one-shot authorization prerequisite. Every ordinary
unmute requests a new drain while DSP mute is still confirmed. A pre-drain or
otherwise denied `ALLOW` terminates the authority session and capture
generation.

The final security review reports no remaining high-confidence vulnerability.
The final holistic decision is **GO for a RAM-only build and boot**. Physical
capture, mute/unmute, direct-open exclusivity, and DSP restart acceptance remain
required before the feature can be accepted.
