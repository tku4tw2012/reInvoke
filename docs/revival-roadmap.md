---
title: reInvoke revival roadmap
description: Milestones and remaining work toward a maintained local assistant endpoint
ms.date: 2026-09-12
ms.topic: overview
---

The target is a local assistant endpoint on the Invoke's existing compute,
speakers, microphones and controls. The project retains BG2CDP rather than
replacing working electronics. Native startup and Bluetooth playback are
milestones, not a complete assistant or a 1.0.0 release.

## Completed milestones

| Milestone            | Outcome                                                                                               | Reference                                                                              |
| -------------------- | ----------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| Preservation         | Firmware acquired, hashed, classified and extracted; originals retained separately from authored work | [Firmware reference](firmware-reference.md)                                            |
| Software mapping     | Boot/update, services, WAMP, audio, radio and hardware-control boundaries recovered                   | [Control-plane reference](emulation/control-plane-emulation.md#vendor-control-surface) |
| Emulation            | Bonefish and selected ARM services accepted WAMP calls and changed state under `qemu-user`            | [Emulation](emulation/control-plane-emulation.md)                                      |
| Closed-unit bring-up | External USB/U-Boot, RAM Linux, NAND logical reads and usable audio/radio/control interfaces          | [Journal](journal.md#closed-unit-bring-up)                                             |
| Owned services       | MCU/DSP, media, privacy/capture and provisioning implemented; detailed safety/restart tests in RAM    | [Product contract](current-product-contract.md)                                        |
| Native operation     | Candidate 02 audio/control baseline; candidate 03 startup, provisioning and SSH negotiation           | [Native results](native-nand-platform.md#current-result)                               |

The evidence does not establish a full schematic, every upstream build input,
or arbitrary-corruption recovery. Unknown part identities and connector
pinouts do not prevent maintaining the recovered software interfaces.

## Remaining work

1. Establish native administrative access. Candidate 03 negotiates the pinned
   Dropbear host identity, then closes at user authentication. Obtain a shell
   or trace before treating account/NSS/toolchain hypotheses as a diagnosis or
   claiming native kernel, PID 1, mount or firewall inspection.
2. Repeat bounded candidate-03 audio, rotary, indicator and service checks.
   Its observed A2DP connection is not acoustic acceptance; candidate-02
   results remain separately scoped.
3. Validate microphone capture/privacy under native startup. Decide whether
   the implemented polled gate meets the intended consumer contract before
   adopting the [synchronous design](microphone-capture.md#deferred-synchronous-privacy-design).
4. Design persistence for Wi-Fi, Bluetooth bonds and preferences, including
   secret handling, power loss, updates, recovery and reset semantics.
   No storage mechanism or partition allocation has been selected.
5. Integrate an assistant consumer of the owned capture/playback interfaces.
   Wake-word detection and assistant protocols are not current product features.
6. Close release prerequisites: supported compatibility, retained build inputs,
   licensing obligations, failure limits and recovery guidance.

There is no candidate-04 feature commitment or dated 1.0.0 schedule.
Deterministic composition from pinned held artifacts is narrower than a
complete rebuild of all dependencies from a clean public clone.

## Unresolved behavior

Native USB enumeration and SSH authentication remain open. Older observations
also include occasional missing MCU Mic-Mute events, unexplained media volume
zero and unestablished AVRCP absolute-volume synchronization. Their evidence
belongs in the [MCU](emulation/mcu-boundary.md),
[speaker](emulation/owned-speaker-control.md) and
[Bluetooth](emulation/bluetooth-stack.md) references.

The [NAND record](nand-write-decision.md) retains the unattributed
factory-setting block difference and failed boot trials. Donor active/inactive
slot selection remains unresolved in [boot/update state](emulation/boot-update-state.md).
These questions do not justify silently changing an accepted artifact's pins.

## Release boundary

A maintained release needs candidate-specific acceptance and explicit recovery
limits, not nominal-path success alone. Logical/OOB captures are not raw
restores, and recovery after observed experiments is not a fail-safe guarantee.
Vendor payloads retain their original terms; public research does not grant
firmware redistribution rights. See [storage policy](acquisition/storage-policy.md)
and [security policy](../.github/SECURITY.md).
