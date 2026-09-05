---
title: Current reInvoke product and architecture contract
description: Canonical behavior, service ownership, dependency boundaries, and acceptance status for the RAM-only reInvoke target
ms.date: 2026-09-05
ms.topic: overview
---

# Current reInvoke product and architecture contract

This document is the canonical contract for the current reInvoke target. It
describes intended product behavior and the accepted RAM architecture. The
research corpus, firmware analyses, journal, and iteration records are evidence
for how the project reached this design; they do not override this contract.

Use [PLAN.md](../PLAN.md) for current completion status and the linked evidence
documents for provenance. A hash in a dated evidence record identifies that
iteration only unless a current build gate explicitly pins it.

## Product generations

Do not merge these three systems into one description.

| System | Role in this repository | Behavior |
|---|---|---|
| 2017 retail Harman Kardon Invoke | Historical product and hardware evidence | Cortana-era retail speaker with Harman's original cloud, media, MCU, DSP, and update stack |
| Harman 2021 final Bluetooth firmware | Historical donor and comparison point | `Barracuda_libre-12.2134.0` removes the cloud-assistant components found in earlier images and adds `wifi-blocker`; it is a vendor Bluetooth-speaker firmware, not reInvoke |
| reInvoke target | Current normative product | Owned RAM-booted Linux lifecycle with local Bluetooth audio, physical controls, microphone privacy, and optional local networking; no Cortana or vendor supervisor |

The examined physical sample contained the earlier
`Barracuda_libre-12.2050.3` rootfs. That installed-image fact does not change the
identity of either Harman's final firmware or the reInvoke target.

## Current product contract

The supported target is a closed Invoke running entirely from a reviewed kernel
and initramfs loaded through yellow-mode U-Boot.

| Capability | Current contract and evidence status |
|---|---|
| Boot and recovery | reInvoke owns PID 1 and the service lifecycle. Yellow-mode Micro-USB recovery and RAM boot are verified. A power cycle returns to the installed firmware. |
| Persistent storage | NAND is not mounted. Ordinary writable MTD nodes are removed; only the explicit read-only NAND node may exist. NAND installation is a separate, unapproved project. |
| Bluetooth playback | BlueZ 5.55 and patched BlueALSA 4.0.0 provide classic A2DP Sink playback. The allowlist, bond state, D-Bus state, and runtime configuration are volatile. Audible playback and rotary volume have been demonstrated; the final accepted image still needs its attended acceptance run. |
| Speaker safety | The owned MCU service initializes amplifier and DAC muted. It opens the physical path only while ALSA is `RUNNING`, the active-PCM lease thread matches ALSA's owner, and that thread resolves to the packaged player. Disconnect, silence, process exit, or shutdown reasserts mute. A 1.5-second holdoff prevents brief transport gaps from flapping the hardware mute gates. |
| Microphone capture | The ALSA capture path is resolved: stereo 48 kHz `S32_LE`, 256-frame periods, and 16 periods are verified. Speech/tap correlation was observed while unmuted, and an accepted restart/privacy test captured only zero samples while muted. This proves the software privacy path, not an independent electrical disconnect. |
| Microphone privacy | Mic-Mute means microphone privacy, not speaker mute. One process-lifetime controller in the MCU service owns physical-button and compatibility-API changes, RAM state, retry, and the red privacy indication. |
| Physical controls | Rotary volume, Mic-Mute short press, and Bluetooth long press are implemented and observed. Other decoded keys are published for compatibility but do not yet have complete product actions. |
| LEDs | Animation transport and `ledOff` are recovered. Privacy red-ring on/off and the white pairing indication were observed. Asset names are inherited evidence, not a complete semantic specification for every color or animation. |
| Networking | SD8887 station and STA/uAP modes work in RAM. `reinvoke-networkd` owns DHCP, route, and resolver state after a root-controlled supplicant connects. The authenticated provisioning parser and privileged apply adapter work, but the final physical-button-to-AP orchestration is not yet a normal product path. |
| Local control | Bonefish provides a legacy MessagePack WAMP compatibility bus. It is unauthenticated, so it is not a public network API. PID 1 accepts ports 9998 and 9999 from loopback and from configured operator allowlist entries, then drops the rest in the INPUT chain. The allowlist is operator-local configuration and is empty by default. Images before v13 carry no firewall and listen on every interface. |

## Accepted runtime architecture

```text
yellow-mode USB/U-Boot
  -> reviewed kernel + reInvoke initramfs
     -> reInvoke-owned PID 1
        |-- read-only storage boundary, USB ADB/ACM, radio modules
        |-- bounded logger and service supervisors
        |-- reinvoke-networkd
        |-- Bonefish compatibility router
        |-- reinvoke-mcu-interface
        |    |-- amplifier/DAC power and mute policy
        |    |-- rotary, buttons, LED transport
        |    `-- process-lifetime microphone privacy owner
        |         `-- mode-0600 DSP microphone Unix socket
        |-- reinvoke-dsp-interface
        |    |-- DSP image load, SPI/GPIO/reset ownership
        |    `-- seven public DSP WAMP registrations
        |-- private D-Bus
        |-- BlueZ bluetoothd
        |-- patched BlueALSA daemon and player
        `-- owned HCI and bounded pairing helpers
```

PID 1 starts each long-lived component under a restart loop with bounded logs.
Shutdown stops the MCU policy owner first so the physical outputs are muted
before audio producers exit.

### Microphone privacy boundary

The microphone path intentionally has one public owner:

1. A physical `micmute` event reaches the MCU privacy controller before WAMP
   publication. It continues to work while Bonefish is unavailable.
2. The MCU service also registers the compatibility procedure
   `com.harman.dsp.micMute`.
3. Both paths update the same process-lifetime controller. Confirmed state is
   atomically stored in mode-0600 RAM state and survives service restarts within
   the current boot.
4. The controller sends DSP opcode `0x09` only through
   `/run/reinvoke/dsp-mic-control.sock`. The DSP service creates that Unix socket
   with mode `0600`; it is not a WAMP registration.
5. On DSP restart, the DSP service re-reads RAM privacy state, restores required
   mute, and only then publishes session readiness.
6. An indeterminate unmute is immediately followed by a mute attempt. Failed
   mute reconciliation is retried independently of the router.

The donor DSP service historically registered eight WAMP procedures, including
`com.harman.dsp.micMute`. The owned DSP service registers seven. Moving raw
Mic-Mute off that unauthenticated DSP surface prevents callers from bypassing
state persistence, retry, and LED policy.

### MCU and DSP ownership

`reinvoke-mcu-interface` is the sole owner of MCU I2C transactions, physical
input decoding, LED animation, amplifier/DAC mute policy, and the public
Mic-Mute compatibility API. It preserves the DSP reset bit when updating the
shared expander register.

`reinvoke-dsp-interface` is the sole owner of the DSP SPI link, handshake GPIOs,
DSP reset bit, boot-image download, command correlation, and private microphone
socket. It never calls the amplifier or DAC unmute procedures on DSP boot.

### Bluetooth and audio dependencies

The current media path does not use Harman's Bluedroid service, `audio-ui`, or
`music-source-manager`.

| Component | Ownership and boundary |
|---|---|
| `bluetoothd` 5.55 | Upstream BlueZ; classic BR/EDR adapter and A2DP/AVRCP control |
| `bluealsa` 4.0.0 | Upstream plus reInvoke patches for the accepted Invoke playback behavior and SBC gap handling |
| `bluealsa-aplay` 4.0.0 | Upstream plus reInvoke patches for the donor ALSA write contract, decoded-PCM buffering, short-stream draining, underrun recovery, and the active-PCM lease |
| `bluealsa-cli` | Local control adapter used by the MCU service for authoritative per-peer volume and mute |
| `hci-init` and pairing agent | Owned helpers; reset volatile controller state and permit only the configured peer and A2DP/AVRCP services during a bounded window |

The pairing address is operator-local configuration. Documentation, logs, and
examples must use `<allowlisted-peer>` or the pattern
`XX:XX:XX:XX:XX:XX`, never a real address.

### Network dependencies

PID 1 loads the SD8887 Wi-Fi firmware and starts `reinvoke-networkd` unless the
volatile command line disables it. Networkd waits for a root-controlled station
supplicant, supervises DHCP, validates lease data, and owns the RAM-only route
and resolver lifecycle.

`reinvoke-provisiond` and `reinvoke-wifi-applyd` are separate on purpose. The
first parses one token-authenticated TLS request without radio or shell
privileges. The second accepts only a UID-0 peer on a root-owned Unix socket,
derives the WPA2 key, and starts fixed supplicant paths. Access-point setup uses
the isolated `p2p0` interface with no gateway, DNS service, or forwarding.
Normal physical-button orchestration of these provisioning components remains
incomplete.

## Dependency boundary

### Included in the accepted RAM image

* reInvoke PID 1, MCU service, DSP service, network daemon, and owned helpers;
* the reviewed reInvoke kernel and device tree;
* BlueZ, patched BlueALSA, D-Bus, and the small Bonefish compatibility runtime;
* board-specific SD8887 firmware and calibration;
* the host-loaded `dsp-img.ldr` required at every DSP start; and
* reviewed LED animation assets required by the implemented indications.

### Reused but not trusted as product policy

Bonefish and its isolated runtime libraries are retained only as a compatibility
router. Board firmware, calibration, and the DSP image are immutable donor
assets with checksum gates. The persistent MCU firmware is used in place and is
never upgraded by reInvoke.

### Excluded

The RAM product does not start Harman's `system-manager`, Bluedroid service,
`audio-ui`, `music-source-manager`, Cortana services, OTA updater, crash-dump
writers, or flash utilities. It does not require Azure, a cloud assistant, SSH,
or a persistent NAND modification.

## Physical controls and indications

| Input | Current local action | Evidence limit |
|---|---|---|
| Rotary clockwise/counter-clockwise | Coalesced BlueALSA volume change and compatibility publication | Live in both directions during A2DP playback |
| Mic-Mute short press | Toggle DSP microphone privacy; red ring follows confirmed state | Occasional presses produce no MCU frame under both donor and owned services; software cannot synthesize a missing hardware event |
| Bluetooth long press | Reopen the bounded allowlisted pairing window; start pairing indication | Live pairing-window signal and white indication observed |
| Action short press | Play the reviewed one-shot action animation | No media or assistant action is assigned |
| Action long press | Compatibility publication only | Product action incomplete |
| Bluetooth short press | Compatibility publication only | Product action incomplete |
| Mic-Mute long press | Compatibility publication only | Provisioning action incomplete |
| Reset short/long press | Compatibility publication only in the RAM runtime | Runtime reset/factory-reset policy intentionally unimplemented |

`com.harman.ledAnimate` plays a checksum-gated asset. `com.harman.ledOff`
cancels an ordinary animation and sends the recovered 41-byte clear packet:
opcode `0x0e`, first-chunk flag `0x01`, and three zero 13-byte frames. Generic
LED calls cannot extinguish the red privacy indication while microphone mute is
required.

## Build reproducibility

Every artifact the image carries is built from a checksum-gated input and is
byte-reproducible. Two independent builds of the kernel, the module tree, the
runtime bundle, and the initramfs each agree byte for byte.

Reproducibility is a safety property here, not a convenience. It is what makes a
pinned digest meaningful: a gate that no preserved artifact can reproduce cannot
be verified, and updating one to match a locally produced binary weakens it
unless the substitution is deliberate and recorded. Substitutions are documented
with their reason beside the artifacts.

Two kernel build timestamps had to be removed to get there. The YAFFS driver
compiled `__DATE__` and `__TIME__` into its strings, and the LZO step stored a
modification time in the compressed payload's header. The second is easy to miss,
because `vmlinux` and `System.map` can be byte-identical while `zImage` still
differs.

## Acceptance and remaining gaps

The architecture, source-level safety fixes, fault injection, host/race tests,
reproducible ARM builds, microphone mute correlation, machine playback
continuity, and earlier attended audio/controls runs are complete. The current
accepted image is not a released persistent firmware.

Remaining gates are:

1. cold-boot the v13 candidate and complete boots 2 through 5;
2. validate the complete startup order and pairing-agent generation guard from
   that image;
3. confirm the WAMP allowlist closes ports 9998 and 9999 to non-allowlisted
   sources on a live network;
4. complete one attended playback-continuity run on that image;
5. harden WAMP setup-response correlation against interleaved messages; and
6. finish physical-button orchestration for an isolated provisioning window.

Entering yellow mode requires the recovery button held at power-on, so cold-boot
gates cannot be driven from software and need the operator present.

NAND persistence, additional cloud/voice features, and full semantics for every
button and LED asset are outside the accepted contract.
