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
| Microphone capture | The raw ALSA path is resolved: stereo 48 kHz `S32_LE`, 256-frame periods, and 16 periods. The speaker target starts no capture consumer. Opening/configuring raw PCM can overwrite an earlier DSP mute route; a future voice service must obey the capture-owner contract below. |
| Microphone privacy | Mic-Mute means microphone privacy, not speaker mute. One process-lifetime MCU controller owns physical-button/API changes, RAM state, retry, and the red animation. On a configured capture path, attended speech/tap tests measured 99.975% nonzero unmuted and exactly 0/244,736 nonzero muted. This is a trusted software boundary, not an electrical disconnect or protection from arbitrary root-level raw-device access. |
| Physical controls | Rotary volume, Mic-Mute short press, Action short press, Bluetooth short/long press, and Mic-Mute long press have owned actions. Bluetooth short toggles the bounded pairing window; long retains the validated reopen fallback. Action toggles Bluetooth play/pause. Mic-Mute long requests the isolated provisioning window when the image is booted in STA/uAP mode. Other decoded keys are published for compatibility. |
| LEDs | Animation transport, `ledOff`, and the separate front/rear `ledSet` transport are recovered. Privacy red-ring on/off and the top-ring white pairing indication were observed. The new state-driven rear pairing/connection policy is host-tested but has not been physically validated under reInvoke. |
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
7. A capture owner must not open or read the raw PCM while confirmed state is
   muted. If low-latency operation requires keeping PCM configured, it must
   configure the stream, request mute, wait for confirmation, and discard every
   sample before that confirmation. ALSA `hw_params` can overwrite an earlier
   DSP route; direct raw opens are outside the privacy controller.

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
GPIO5 pin-function transition, DSP reset bit, boot-image download, command
correlation, and private microphone socket. Before every download it selects
GPIO5 manual chip-select mode; afterward it restores message mode with a
read-modify-write that preserves MCU GPIO3. It never calls the amplifier or DAC
unmute procedures on DSP boot.

### Front and rear indicator contract

The MCU service registers `com.harman.ledSet`. It requires a first positional
target and recognizes `front` and `back`; optional `mode` and `color` keyword
arguments follow the donor contract. Extra positional arguments and unknown
keyword keys are ignored. Modes encode as `off=0`, `on=1`, `dim=2`,
`slow-blink=3`, and `fast-blink=4`. Front `white` and `amber` are mutually
exclusive, while back ignores colour. Unknown targets or front colours send
the unchanged confirmed state. The service persists the three confirmed
channel states and sends:

```text
09 <front-amber> <front-white> <back> 00 00
```

as one fixed command to MCU address `0x36`. It serializes state and transport,
rolls back candidate state on I2C failure, and propagates that failure to the
WAMP caller. The final zero bytes replace indeterminate donor stack residue.
This contract is supported by static donor disassembly and host tests. The
front/rear transport and the state-driven rear policy still require physical
reInvoke validation. These indicators are separate from the top-ring animation
and microphone privacy policy.

The donor rear-indicator policy is state-driven:

| Donor Bluetooth state | Rear indicator |
|---|---|
| `pairing` | `slow-blink` |
| `connected` | `on` |
| disconnected or any other state | `off` |

Donor `audio-ui` maps the logical Bluetooth **short** press to pairing and maps
another short press while pairing to cancellation. It defines no local
Bluetooth-long action. The donor binaries do not reveal the missing bridge from
the MCU's `com.harman.vui.keypress` publication to the topic `audio-ui`
subscribes to, so this is recovered policy rather than a proven retail
end-to-end path.

reInvoke implements the short press as `SIGUSR2`: it opens the configured window
while idle and cancels Pairable/Discoverable while active. `SIGUSR1` retains the
already-validated long-press reopen fallback. Signal handlers set only
`sig_atomic_t` flags; D-Bus work stays in the bounded dispatch loop.

The pairing agent tracks the allowlisted BlueZ `Device1.Connected` property at
startup and through `PropertiesChanged`. It atomically publishes a mode-0600
`/run/reinvoke/bluetooth-state` file: an active window publishes `pairing`
regardless of connection, otherwise a connected peer publishes `connected`,
and all other states publish `off`. The MCU service reads this bounded state,
maps it to rear slow-blink/on/off, and deduplicates confirmed I2C writes. A
missing or invalid producer state is logged and safely clears the rear
indicator; the generation guard removes stale state when the producer exits.
This controller never changes the top-ring player or microphone
privacy state. The complete short-toggle and automatic rear indication remain
hardware-validation items.

### Bluetooth and audio dependencies

The current media path does not use Harman's Bluedroid service, `audio-ui`, or
`music-source-manager`.

| Component | Ownership and boundary |
|---|---|
| `bluetoothd` 5.55 | Upstream BlueZ; classic BR/EDR adapter and A2DP/AVRCP control |
| `bluealsa` 4.0.0 | Upstream plus reInvoke patches for the accepted Invoke playback behavior and SBC gap handling |
| `bluealsa-aplay` 4.0.0 | Upstream plus reInvoke patches for the donor ALSA write contract, decoded-PCM buffering, short-stream draining, underrun recovery, and the active-PCM lease |
| `bluealsa-cli` | Local control adapter used by the MCU service for authoritative per-peer volume and mute |
| `hci-init` and pairing agent | Owned helpers; reset volatile controller state, permit only the configured peer and A2DP/AVRCP services during a bounded window, track that peer's connection, and publish authoritative volatile Bluetooth state |

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
|---|---|---|| Rotary clockwise/counter-clockwise | Coalesced BlueALSA volume change and compatibility publication | Live in both directions during A2DP playback |
| Mic-Mute short press | Toggle DSP microphone privacy; red ring follows confirmed state | Occasional presses produce no MCU frame under both donor and owned services; software cannot synthesize a missing hardware event |
| Bluetooth long press | Reopen the bounded allowlisted pairing window | Validated compatibility fallback; donor `audio-ui` defines no long-press action |
| Action short press | Toggle Bluetooth play/pause and play the reviewed one-shot action animation | Owned reinterpretation; no assistant action is assigned |
| Action long press | Compatibility publication only | Product action incomplete |
| Bluetooth short press | Open pairing while idle; cancel pairing while active | Donor-compatible policy is implemented and host-tested; physical toggle and rear indication remain unvalidated |
| Mic-Mute long press | Request a bounded isolated provisioning window | Requires a STA/uAP boot; physical end-to-end validation remains |
| Reset short/long press | Compatibility publication only in the RAM runtime | Runtime reset/factory-reset policy intentionally unimplemented |

`com.harman.ledAnimate` plays a checksum-gated asset. `com.harman.ledOff`
cancels an ordinary animation and sends the recovered 41-byte clear packet:
opcode `0x0e`, first-chunk flag `0x01`, and three zero 13-byte frames. Generic
LED calls cannot extinguish the red privacy indication while microphone mute is
required.

RC3 and RC4 exposed a v13 regression that held GPIO3 low, preventing every
physical input and indicator. The MCU remained alive, but the partial expander
direction mask left a cold-initialization state different from the donor. A live
donor service released the line, and the owned service kept it high when handed
the donor-initialized hardware. RC5 restores the donor `0x03=0x00` direction
value while leaving DSP reset output bit 0 exclusively owned by the DSP
service. Its first cold boot restored the physical controls and indicators.

## Factory reset

The stock behavior is recorded here so it is not lost, and is deliberately not
implemented.

On the retail unit, holding the recessed Reset pinhole beside Mic-Mute for five
seconds with the unit booted and USB disconnected, then releasing it, restarts
the speaker and deletes pairings and other writable user state. Holding Reset
while applying power is a different, early-boot action and is not this feature.

reInvoke does not implement it, for a reason that is structural rather than
incidental. A factory reset is only meaningful against persistent state, and
this target mounts no NAND: every pairing, bond, key, and configuration value
already lives in RAM and disappears on power loss. A reset control here would
either do nothing or would have to reach past the storage boundary the platform
exists to enforce.

Implementing it therefore belongs to the NAND discussion, not before it, and
depends on decisions that discussion has to make first:

* which partitions hold user data and may be erased;
* which system and recovery assets must stay immutable so a reset cannot brick
  the unit;
* what the unit does if power is lost mid-erase; and
* how a failed reset rolls back.

Until then the pinhole remains an operator-facing recovery path on the stock
firmware, not a reInvoke product control.

## Build reproducibility

Every artifact the image carries is checksum-gated, and the deployable kernel,
module tree, runtime composition, and initramfs each agree byte for byte across
two builds. The owned Go services reproduce from the archived Go 1.18.1
toolchain and checked-in builders. The kernel image and installed module tree
reproduce with the archived Android NDK r10e GCC 4.9 toolchain.

The kernel was rebuilt a third time after hardening those provenance gates. The
fresh build reproduced `d29a0075...`, the supplied DTB, and all four module
digests exactly; it is retained as
`reinvoke-kernel-v14-9-provenance-20260906`.

That is not yet a complete clean-room rebuild claim for every C binary. The
current BlueZ, BlueALSA, iptables, and helper artifacts are pinned and unchanged
across the accepted candidates, but some builders still depend on an
unarchived host ARM sysroot, built static libraries, or unpinned host tools.
Candidate composition is therefore reproducible and its inputs are
content-addressed; complete reconstruction of every C input solely from
retained material remains open.

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

1. package and re-gate the reviewed Bluetooth ObjectManager connected-state
   fix, then physically confirm the rear connected indication;
2. confirm the WAMP allowlist closes ports 9998 and 9999 to non-allowlisted
   sources on a live network;
3. complete one attended playback-continuity run on that image; and
4. finish physical-button orchestration for an isolated provisioning window.

`pre-nand-rc5` is the current fully gated candidate. `pre-nand-rc6` is the
current experimental candidate; it packages the live-tested BlueZ
ObjectManager state fix and must clear the same cold-boot gate before replacing
RC5. RC5's first cold boot
restored GPIO3 high and recovered rotary input, Mic-Mute privacy, Bluetooth
short-toggle, Bluetooth long reopen, and the rear pairing indication. The
operator observed both rear slow blink and top red privacy, while the evidence
bundle recorded 18 rotary publications, both microphone directions, and
`pairing -> off -> pairing`. The full collector exited zero with DSP version
`25688` and no structural failures.

RC5 restores the donor expander-direction value that v13 replaced with a
speculative partial mask. A live donor/owned handoff isolated that change, and
the first cold boot confirmed it. The candidate is archived as a bound
kernel/initramfs pair with its source revisions and evidence manifests.

On a cold boot from power-off the accepted image selected download mode,
restored message mode, and reported
`EVENT_DSP_VERSION=0.0.64.58`, and the full
`collect-native-acceptance.sh` gate passed twice with no failures, once on the
first DSP generation and once after a supervised restart. A killed DSP service
was replaced under PID 1, restored the GPIO5 pin function to `0x0138D249`,
reapplied the retained microphone mute before announcing readiness, and
round-tripped `getVer` and both microphone directions. A killed `bluetoothd`
was likewise replaced, re-registered both A2DP endpoints, and the generation
guard restarted the pairing agent.

That guard reopens a pairing window on every `bluetoothd` generation, which is
the agent's documented startup behaviour rather than a regression. The window is
bounded to its configured seconds and was observed returning to `off` on
expiry, and the agent rejects every device-bearing request from any address
outside its single-device allowlist, so an unattended replacement cannot admit
an unexpected peer.

Physical button presses reach the services as MCU publications and cannot be
injected over WAMP. RC5 now proves those publications and their policies. The
RC6 pairing agent also proved `pairing -> connected -> off` against a
persistent RAM-only bond when run live on RC5. The operator was absent for the
rear connected light, and the packaged RC6 binary still needs a cold boot.

The WAMP firewall was verified structurally on the running image. Both ports
accept loopback and the single allowlisted host and drop every other source,
with the accept rules ordered ahead of the drop rules. PID 1 installs those
rules before the router starts and leaves the autonomous runtime stopped if the
setup fails, so there is no interval in which the router listens unprotected.

The router binds `0.0.0.0` on both ports, which is deliberate because the
allowlisted host is off-box, but it means the firewall is the only thing
standing between the router and the wider network rather than a second layer.
That places the fail-closed ordering above and the pinned allowlist on the
critical path for this boundary.

The drop target itself is now known to work rather than merely present. A DROP
rule for port 9999 inserted ahead of the accept rules made a previously working
WAMP call time out, and removing it restored the call, with the original six
rules left intact. That exercises the hand-symlinked `xtables` modules, the
chain ordering, and the drop target on this kernel, which structural inspection
alone could not establish.

What remains unproven is only the source matching: that a non-allowlisted remote
address is dropped while the allowlisted one is accepted. The live-network half
of that gate cannot be closed on-box. This kernel exposes
only a mount namespace, so no network namespace can be created to originate
genuine non-allowlisted traffic, and packets addressed to a local address
traverse loopback and would match the accept rule instead. Confirming the drop
behaviour therefore requires a separate host on the same WLAN.

This kernel also sets `pid_max` to 4096, and the PID counter was observed
wrapping within 132 seconds of ordinary supervision churn. PID reuse is a
routine event here, so nothing may treat a recorded PID as proof of identity.
The MCU pairing control resolves `/proc/<pid>/exe` and refuses to signal a
mismatched executable, which keeps a recycled PID from receiving a control
signal intended for the pairing agent.

Entering yellow mode requires the recovery button held at power-on, so cold-boot
gates cannot be driven from software and need the operator present.

NAND persistence, additional cloud/voice features, and full semantics for every
button and LED asset are outside the accepted contract.
