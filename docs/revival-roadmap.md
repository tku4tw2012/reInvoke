---
title: reInvoke revival roadmap
description: Milestones and remaining work toward a maintained local assistant endpoint
---

The target is a local assistant endpoint on the Invoke's existing compute,
speakers, microphones and controls. The project retains BG2CDP rather than
replacing working electronics. The speaker boots and runs on its own; an
assistant is not implemented, and that is the substance of the work left.

## Completed milestones

| Milestone            | Outcome                                                                                               | Reference                                                                              |
| -------------------- | ----------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| Preservation         | Firmware acquired, hashed, classified and extracted; originals retained separately from authored work | [Firmware reference](firmware-reference.md)                                            |
| Software mapping     | Boot/update, services, WAMP, audio, radio and hardware-control boundaries recovered                   | [Control-plane reference](emulation/control-plane-emulation.md#vendor-control-surface) |
| Emulation            | Bonefish and selected ARM services accepted WAMP calls and changed state under `qemu-user`            | [Emulation](emulation/control-plane-emulation.md)                                      |
| Closed-unit bring-up | External USB/U-Boot, RAM Linux, NAND logical reads and usable audio/radio/control interfaces          | [Journal](journal.md#closed-unit-bring-up)                                             |
| Owned services       | MCU/DSP, media, mic mute/capture and provisioning implemented; detailed safety/restart tests in RAM    | [Product contract](current-product-contract.md)                                        |
| Native operation     | Boots from NAND unattended; audio, control, networking, SSH, USB ADB and persistence in service | [Native results](native-nand-platform.md#current-result)                                |

The evidence does not establish a full schematic, every upstream build input,
or arbitrary-corruption recovery. Unknown part identities and connector
pinouts do not prevent maintaining the recovered software interfaces.

## Remaining work

1. Integrate an assistant consumer of the owned capture and playback
   interfaces. Wake-word detection and assistant protocols are not current
   product features, and nothing in the image consumes the microphone today.
2. Decide whether the implemented polled mic-mute gate meets the intended
   consumer contract before adopting the
   [synchronous design](microphone-capture.md#deferred-synchronous-mute-design).
   The polled gate is verified to silence capture; the question is the
   contract a consumer should be able to rely on, not whether muting works.
3. Extend persistence beyond Wi-Fi credentials and Bluetooth stack
   configuration to user preferences, and define update, recovery and reset
   semantics for the `/persist` contents.
4. Close release prerequisites: supported compatibility, retained build
   inputs, licensing obligations, failure limits and recovery guidance.
5. Establish a restorable NAND backup. The retained captures are logical
   main-data and exposed-OOB reads, not physical programmer images, and five
   pages reported uncorrectable ECC during capture. Recovery has worked after
   observed failures; that is not a guarantee against arbitrary boot-chain
   corruption. See the [NAND record](nand-write-decision.md).

Native administrative access, USB enumeration and microphone capture under
native startup were previously listed here and are now in service; see
[release validation](release-validation.md) for how each was observed.

There is no dated 1.0.0 schedule. Deterministic composition from pinned held
artifacts is narrower than a complete rebuild of all dependencies from a clean
public clone.

## Unresolved behavior

The USB ADB bring-up record appears in the boot log and the kernel buffer but
not in the runtime log. Four explanations were tested and disproved. The cause
is not established; the behaviour is functionally harmless.

Older observations, not re-tested on the current build, include occasional
missing MCU Mic-Mute events, unexplained media volume zero and unestablished
AVRCP absolute-volume synchronization. Their evidence belongs in the
[MCU](emulation/mcu-boundary.md),
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
