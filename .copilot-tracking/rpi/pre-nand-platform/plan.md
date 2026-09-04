---
title: Pre-NAND platform plan
description: Dependency-ordered implementation plan for the complete RAM-only replacement and acceptance gate
ms.date: 2026-09-03
ms.topic: overview
---

## Planned phases

1. Consolidate the three research streams into evidence-backed requirements.
2. Resolve or explicitly bound physical microphone capture.
3. Implement the must-have remaining owned service contracts.
4. Package one checksum-gated, restart-safe RAM image with owned services.
5. Run targeted host validation and functional review.
6. Run repeated cold-boot hardware acceptance with mute and recovery controls.
7. Update the pull request and discover follow-up work.

## Implementation work packages

### Package foundation

1. Add aggregate host validation and consistent test entry points.
2. Enforce an explicit initramfs size budget below the 68 MiB boot limit.
3. Package checksum-gated router, D-Bus, owned MCU, owned DSP,
   `dsp-img.ldr`, BlueZ, BlueALSA, playback, and control artifacts.
4. Record every artifact and digest in `/etc/reinvoke-release`.

### Own the runtime lifecycle

1. Decouple the MCU heartbeat from WAMP connectivity.
2. Add bounded reconnect behavior to MCU and DSP clients.
3. Add PID 1 supervision, restart backoff, dependency ordering, and
   command-line kill switches for critical services.
4. Reap orphaned processes, back off the USB console loop, and remute before
   controlled shutdown or service restart.
5. Prevent writable MTD exposure and remove unnecessary flash-capable applets.

### Complete the local speaker contracts

1. Port the tested speaker-control state and BlueALSA authority to a static
   target binary.
2. Add one mute-policy owner for stream start, stop, disconnect, failure, and
   shutdown.
3. Bind rotary events to the volume authority and verify AVRCP synchronization.
4. Recover button events and implement a bounded, one-peer pairing window.
5. Define a stable RAM-platform Bluetooth identity.
6. Complete attended acoustic microphone correlation with the now-valid ALSA
   capture geometry.
7. Allow authenticated Wi-Fi replacement over the active LAN.
8. Allow a physical long press to open a bounded, isolated provisioning AP when
   the configured network is unavailable, without reset or USB recovery.
9. Roll back a failed runtime credential replacement to the previous working
   connection while it remains available in RAM.

### Complete product-facing behavior

1. Recover and implement useful LED states without copying cloud behavior.
2. Add redistributable owned earcons only if they improve standalone use.
3. Publish required readiness and liveness signals.
4. Keep cloud voice-agent, factory setters, OTA, boot-slot, and MCU update
   contracts excluded.

### Prove release readiness

1. Add a machine-readable on-device acceptance collector.
2. Add a host repeat-boot harness.
3. Pass focused builds, tests, functional review, and safety review.
4. Pass at least five consecutive cold boots plus service fault injection and
   soak validation.
5. Correct stale documentation, add evidence sidecars, and prepare the draft
   pull request for review.

## Management interface boundary

A Cortana-like application is not required. The optional future management
surface should be a local web interface, or a thin mobile wrapper over the same
local API, for provisioning, pairing, identity, volume, diagnostics, and image
version. It must not become a dependency of boot, playback, mute safety, or
recovery.

Before persistent storage is approved, Wi-Fi credentials and bonds remain
volatile across power removal. The runtime can support cable-free network
changes now; survival across cold boot belongs to the later persistence design.

## Completion weighting

The broader replacement-platform estimate uses these fixed weights:

| Workstream | Weight | Current state |
|------------|-------:|---------------|
| Boot, kernel, hardware buses, and recovery | 20% | Complete |
| Networking and provisioning | 15% | Complete |
| Bluetooth transport and audible playback | 15% | Complete |
| Owned MCU and DSP control | 15% | Complete |
| Microphone capture | 5% | Unresolved |
| Required physical and service contracts | 8% | Partially complete |
| Autonomous packaging and supervision | 12% | Not complete |
| Fault recovery, soak, and repeated cold boots | 7% | Not complete |
| Evidence, documentation, and pull-request gate | 3% | In progress |

Microphone capture and remaining contracts account for at most 13 percentage
points. Autonomous packaging, supervision, and acceptance reliability account
for another 19 percentage points. Optional LED polish, earcons, cloud
contracts, and persistent updates do not block the core RAM-only platform.

## Safety gates

* Keep amplifier and DAC muted except during an attended audible-output window.
* Keep NAND filesystems unmounted and do not issue erase, write, or boot-slot
  commands.
* Preserve yellow-mode USB recovery as the rollback path.
* Do not promote optional donor or cloud contracts into release blockers.

## Completion

Planning is complete. Work packages, dependencies, safety boundaries,
management scope, and acceptance criteria are fixed. Offline MCU contract
decoding can refine button and LED implementation without changing the plan.
