---
title: Current reInvoke product and architecture contract
description: Canonical behavior, service ownership, dependency boundaries, and native NAND acceptance status
ms.date: 2026-09-12
ms.topic: overview
---

The current reInvoke target combines intended product behavior with the
accepted native architecture. The
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
| reInvoke target | Current normative product | Owned NAND-started Linux lifecycle with local Bluetooth audio, physical controls, microphone privacy, and local networking; no Cortana or vendor supervisor |

Before the approved NAND trials, the examined physical sample contained
`Barracuda_libre-12.2050.3`. That historical installed-image fact does not change the
identity of either Harman's final firmware or the reInvoke target.

## Current product contract

The supported target is a closed Invoke running the owned runtime from NAND.
Yellow-mode U-Boot and RAM Linux remain the recovery and development path.

Candidate 02 is the verified native milestone. Unless a row explicitly says
native, detailed privacy, supervision, firewall, and restart measurements below
come from the earlier host-loaded RAM platform or offline tests. Image
composition describes the intended native graph; the running native kernel,
PID 1, and mount table have not been read through a shell. Candidate 03 is now
installed and returned its changed `reInvoke-NAND` Bluetooth name after an
owner-controlled power-only boot. That startup indicator does not transfer
candidate 02's broader acceptance to 03; native USB remains absent and native
SSH awaits provisioning and a login.

| Capability | Current contract and evidence status |
|---|---|
| Boot and recovery | Candidate 03's fresh, changed Bluetooth name after a power-only start establishes native runtime progress. Candidate 02 previously demonstrated encrypted pairing, audible playback, rotary volume, indicators, physical provisioning, and MCU/DSP calls. Native USB/ADB remains unavailable. No recovery Linux was loaded between either accepted flash and its first power-only test; see [NAND startup status](nand-write-decision.md). |
| Persistent storage | Complete candidates 02 and 03 were each installed through one explicitly approved vendor whole-good-block erase/program operation, including the default app seed and paired owned rootfs/BSL. All nine vendor program/read address sets, full transfer length and returned prompt were checked. The 03 wrapper took 36.819 seconds including validation and cleanup. This is not independent Linux readback or a programmer-grade raw restore image. The cause of candidate 02's breakthrough remains unisolated. |
| Administration | Candidate 03 packages dedicated key-authenticated SSH with a private source allowlist and host-key pin. Offline authentication positive/negative controls passed. Native login, listener, firewall, PTY, and kernel behavior remain unverified until network access is established. WAMP is not a shell and absence of USB does not establish native boot failure. |
| Bluetooth playback | BlueZ 5.55 and patched BlueALSA 4.0.0 provide classic A2DP Sink playback. The allowlist, bond state, D-Bus state, and runtime configuration are volatile. Candidate 02 passed native encrypted pairing, audible musical playback, and physical rotary volume in both directions. A successor needs its own bounded acceptance run. |
| Speaker safety | The owned MCU service initializes amplifier and DAC muted. It opens the physical path only while ALSA is `RUNNING`, the active-PCM lease thread matches ALSA's owner, and that thread resolves to the packaged player. Disconnect, silence, process exit, or shutdown reasserts mute. A 1.5-second holdoff prevents brief transport gaps from flapping the hardware mute gates. |
| Microphone capture | `reinvoke-mic-capture` supervises `arecord` on `hw:1,0` and delivers mono left-channel 48 kHz `S32_LE` 256-frame records to consumers at `/run/reinvoke/mic-capture/audio.sock` (mode `0600`). The donor-designated left channel is the voice-recognition path; right channel is call audio. The implemented 100 ms state-file poll drops periods after observing `muted`; it is not an instantaneous fence for queued data. DSP restart creates a new stream generation. Historical RAM hardware acceptance covered 14 toggles, zero muted delivery in the measured windows, resumed unmuted audio, and DSP restart recovery. Native capture/privacy acceptance is open. Beamforming and AEC activation are not proven. |
| Microphone privacy | Mic-Mute means microphone privacy, not speaker mute. One process-lifetime MCU controller owns physical-button/API changes, RAM state, retry, and the red animation. Historical RAM speech/tap tests measured 99.975% nonzero unmuted and exactly 0/244,736 nonzero muted. Candidate 02 demonstrated red-indicator toggling, not that data-path measurement. This is a trusted software boundary, not an electrical disconnect or protection from arbitrary root-level raw-device access. |
| Physical controls | Rotary volume, Mic-Mute short press, Action short press, Bluetooth short/long press, and Mic-Mute long press have owned actions. Bluetooth short toggles the bounded pairing window; long retains the validated reopen fallback. Action toggles Bluetooth play/pause. Mic-Mute long requests the isolated provisioning window when the image is booted in STA/uAP mode. Other decoded keys are published for compatibility. |
| LEDs | Animation transport, `ledOff`, and the separate front/rear `ledSet` transport are recovered. Historical RAM RC5 physically verified rear slow blink and top red privacy; RC6 verified connected-state publication without an attended rear-connected-light observation. Native candidate 02 demonstrated red on/off and blue top-tap feedback. Other native indications remain unverified. |
| Networking | Candidate 02 opened its isolated provisioning AP from a physical Mic-Mute long press during native NAND startup. The operator client verified the ephemeral TLS descriptor, delivered credentials through the authenticated parser, restored its own network, and reached the Invoke's MCU/DSP services on the shared local network. `reinvoke-networkd` owns DHCP, route, and resolver state. Credentials remain volatile and must be reprovisioned after power loss. |
| Local control | Bonefish provides a legacy MessagePack WAMP compatibility bus, not an authenticated administration shell. The image's intended firewall accepts ports 9998/9999 from loopback and configured operator allowlist entries, then drops the rest. Historical RAM tests validated enforcement; native firewall internals remain unread. Candidate 02 passed eight automated WAMP groups including invalid arguments, three fresh DSP events, volume/mute changes, events, and restoration. No heartbeat appeared in a 75-second native observation. The allowlist is private and empty by default; historical RAM images before v13 lack this firewall. |

## Accepted runtime architecture

```text
normal wall-power boot
  -> retained vendor native boot chain and kernel
     -> read-only reInvoke NAND bootstrap
        -> reInvoke-owned PID 1
        |-- read-only storage boundary, optional USB diagnostics, radio modules
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
        |-- reinvoke-mic-capture
        |    |-- arecord supervision, left-channel extraction
        |    |-- state-file gate (polls /run/reinvoke/microphone-state)
        |    `-- mode-0600 stream socket /run/reinvoke/mic-capture/audio.sock
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
7. `reinvoke-mic-capture` is the sole owner of the raw PCM handle. Consumers
   read from `/run/reinvoke/mic-capture/audio.sock` (mode `0600`); they never
   open the raw device. The capture owner polls `/run/reinvoke/microphone-state`
   every 100 ms and discards periods after observing `muted`. This is not a
   synchronous fence: polling delay and queued records remain, and the stronger
   authority/drain protocol is [deferred design](microphone-capture.md#deferred-synchronous-privacy-design). DSP
   restart increments the stream generation; consumers detect this via the
   generation field in the stream header and reconnect. ALSA `hw_params` can
   overwrite an earlier DSP route; direct raw opens are outside the privacy
   controller and must not be used.

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
This contract is supported by static donor disassembly and host tests.
Historical RAM RC5 additionally confirmed rear slow blink; full front/rear
semantics and native state-driven rear behavior remain unverified.
These indicators are separate from the top-ring animation
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
privacy state. RAM RC5 verified physical short-toggle and rear slow blink;
RC6 verified the published connected state. Complete native rear indication
remains a hardware-validation item.

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
derives the WPA2 key, and starts fixed supplicant paths. Access-point setup uses the isolated `p2p0` interface with no gateway, DNS
service, or forwarding. Candidate 02 demonstrated the physical-button path,
authenticated credential delivery, client-network restoration, and subsequent
local-network MCU/DSP access.

## Dependency boundary

### Included in the current owned runtime

* reInvoke PID 1, MCU service, DSP service, network daemon, and owned helpers;
* the retained vendor native kernel, plus the reviewed recovery kernel and
  device tree outside the normal boot path;
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

The owned runtime does not start Harman's `system-manager`, Bluedroid service,
`audio-ui`, `music-source-manager`, Cortana services, OTA updater, crash-dump
writers, or flash utilities. It does not require Azure, a cloud assistant, SSH,
or an active vendor update service.

## Physical controls and indications

| Input | Current local action | Evidence limit |
|---|---|---|
| Rotary clockwise/counter-clockwise | Coalesced BlueALSA volume change and compatibility publication | Live in both directions during A2DP playback |
| Mic-Mute short press | Toggle DSP microphone privacy; red ring follows confirmed state | Occasional presses produce no MCU frame under both donor and owned services; software cannot synthesize a missing hardware event |
| Bluetooth long press | Reopen the bounded allowlisted pairing window | Validated compatibility fallback; donor `audio-ui` defines no long-press action |
| Action short press | Toggle Bluetooth play/pause and play the reviewed one-shot action animation | Owned reinterpretation; no assistant action is assigned |
| Action long press | Compatibility publication only | Product action incomplete |
| Bluetooth short press | Open pairing while idle; cancel pairing while active | Historical RAM RC5 verified physical toggle and rear slow blink; native short-toggle and full rear indication remain unverified |
| Mic-Mute long press | Request a bounded isolated provisioning window | Native candidate 02 completed the physical AP-to-station flow |
| Reset short/long press | Compatibility publication only in the owned runtime | Runtime reset/factory-reset policy intentionally unimplemented |

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

Harman's [2017 owner's manual, page 8](https://support.harmankardon.com/on/demandware.static/-/Sites-masterCatalog_Harman/default/dwdac694e8/pdfs/Harman%20Kardon%20Invoke%20Owners%20Manual.pdf#page=8)
describes the reset pin as resetting settings and restarting the device.
Page 34 also identifies a factory-reset light pattern. This documents a
settings reset, not a firmware reinstall or a way to undo modified read-only
filesystem contents. Availability in the owned runtime is unverified and no factory reset was
performed during native candidate acceptance.

On the retail unit, holding the recessed Reset pinhole beside Mic-Mute for five
seconds with the unit booted and USB disconnected, then releasing it, restarts
the speaker and deletes pairings and other writable user state. Holding Reset
while applying power is a different, early-boot action and is not this feature.
That five-second procedure is a retained historical record, not a newly
performed test or a confirmed handler on the reconstructed 12.2050.3 image.
The 2017 manual separately assigns a five-second Mic On/Off hold to Wi-Fi
setup; do not confuse it with factory reset.

reInvoke does not implement it, for a reason that is structural rather than
incidental. A factory reset is only meaningful against persistent mutable state. The system
image starts read-only from NAND, while pairing, bonds, keys, and configuration
remain in RAM and disappear on power loss. A reset control would currently add
no useful behavior or would have to cross a new persistent-storage boundary.

Implementing it belongs to the still-open mutable-state design, even though
NAND startup itself now works. It depends on:

* which partitions hold user data and may be erased;
* which system and recovery assets must stay immutable so a reset cannot brick
  the unit;
* what the unit does if power is lost mid-erase; and
* how a failed reset rolls back.

Until then the pinhole remains an operator-facing recovery path on the stock
firmware, not a reInvoke product control.

## Build reproducibility

The following kernel/RC evidence describes the accepted RAM/recovery builds.
Candidate 02 instead retains the vendor native kernel byte-identically.
Its main rootfs and paired BSL compose from private RC12 artifacts and declared
source pins; a public fresh clone cannot reconstruct the whole image by
itself. See the [native build boundary](native-nand-platform.md#build-and-reproducibility-boundary).

Every artifact the accepted RAM image carries is checksum-gated, and the deployable kernel,
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

Candidate 02 has passed the personal-project native milestone: wall-power NAND
startup, encrypted Bluetooth pairing, audible playback, physical rotary volume,
Mic-Mute indication, physical Wi-Fi provisioning, and repeated local-network
MCU/DSP/WAMP calls. Its reproducible builders and targeted failure controls
remain separate from hardware acceptance.

The 2026-09-12 native service checks verified both RawSocket WAMP on 9999
and WebSocket WAMP on 9998 using `wamp.2.msgpack`. Unknown procedures and
malformed RawSocket arguments failed as expected; volume/music-mute tests
restored music `44` unmuted and system `70`. The checks did not change
microphone state or use playback, helper firmware, or reboot. No heartbeat
appeared during the 75-second observation. Exact timing and private-evidence
locators are in the [native guide](native-nand-platform.md#current-result).

Remaining gates are:

1. restore native USB/ADB or a separately authenticated administrative path;
2. repeat microphone capture/privacy measurements on a native NAND boot;
3. design persistent Wi-Fi and bond state with power-loss and update behavior;
4. repeat the bounded build and service checks for the approved successor.

### Historical RAM acceptance chronology

The end-to-end setup path works. An external client joined the speaker's setup
access point, received DHCP from the speaker, authenticated over HTTPS,
delivered credentials, and the speaker joined the home network and stayed
reachable there. Station state was wiped and verified empty first, so the result
was not a leftover association.

Two behaviours are worth stating plainly because they are easy to misread.
Volume has two distinct controls: `com.harman.volumeSet` takes
`[value, "music"]` and sets the media volume, while `com.harman.dsp.volumeSet`
takes a single raw byte and sets DSP gain. Calling the second while the first is
zero produces a confident acknowledgement and silence. Separately, a freshly
acquired BlueALSA transport starts at maximum volume, so a newly connected peer
is lowered to a safe ceiling and a quieter one is never raised.

`pre-nand-rc9` was the first build whose
STA/uAP provisioning window actually works. It passed a clean cold boot from the
packaged image with no hot patches: `windowd` reported `control socket ready` at
uptime 5.32 with no crash loop, and the acceptance collector exited zero with DSP
version `25688` and no NAND mount.

Its provisioning gate is closed. A physical Mic-Mute long press passed all
fourteen checks, covering the access point, the bounded descriptor, IPv4 and IPv6
forwarding staying off, the WAMP control plane staying closed to access-point
clients, no NAND mount, and full self-cleanup at the 300 second bound. IPv6 is
not a supported feature; the kernel enables it, so the gate asserts its
forwarding stays off rather than relying on it being absent.

At that historical checkpoint, NAND startup remained a separate qualification.
The owner-approved complete pilot occupies `[0x02920000,0x04fa0000)`, 308
erase blocks inside the established 90 MiB rootfs allocation. Its final
38.5 MiB payload passed data/ECC verification and independent readback.
That initial rootfs-only operation did not rewrite boot images or bootloaders.

On September 10, a USB-loaded custom kernel and RC12 RAM bridge mounted that
NAND copy read-only and handed PID 1 into its bootstrap. The pilot reached its
runtime and repeated real shells, with the runtime init hash matching the
installed manifest. This advances NAND runtime execution evidence, not
vendor-kernel or host-independent boot acceptance.

The normal power-on at 23:52 UTC on 2026-09-09 remained at the Marvell `FF`
download endpoint with no pilot USB product or ADB. It did not demonstrate
standalone reInvoke. The installed pilot is being retained at the owner's
request; at that checkpoint, the next work concerned the boot path rather than
another speculative flash.
The pilot's new network credentials and Bluetooth bonds are also RAM-only,
so persistent configuration remains a product gap even if startup succeeds.

The later owner-approved vendor-stack flash on September 11 reported erasing
2,046 good blocks before programming its eight records. Normal boot then
changed to spinning lights without an observed USB endpoint, not a working
reInvoke runtime. Yellow-mode U-Boot and RAM ADB remained accessible after
owner-assisted re-entry. Independent rootfs readback still matches the pilot;
the old update/status data were erased. The owner prohibits restoration and
requests a forward startup variation. Those facts superseded the earlier rootfs-only write boundary at that
checkpoint. Candidate 02's later nine-record native success supersedes this
failed eight-record trial as the current installed result.

Earlier, the two-block diagnostic and its approved restoration passed storage
checks. Full main-data and exposed-OOB comparison then matched the September 7
capture. Host-supplied RAM recovery remained usable, including attachment to a
present `FF` endpoint without another physical reset. Those historical results
do not prove stock boot, permanent ADB access or physical-OOB clone fidelity.

Use the [native NAND guide](native-nand-platform.md) for the current result and
artifact pins; the [NAND decision history](nand-write-decision.md) preserves the
failed trials and their checkpoint-era workflows.

## Open defects and unexplained observations

These are recorded so a later session does not rediscover them or misdiagnose a
recurrence.

These are historical RAM/acquisition observations unless explicitly qualified
as native. They remain unresolved evidence, not new observations on candidate 02.

**One factory-setting erase block changed without an attributed operation.** Two
complete 2026-09-07 logical reads match each other, but differ from the
2026-09-02 image in `0x00660000-0x0067ffff`. The block previously held 13,040
non-`0xFF` bytes and now reads entirely `0xFF`. The decoded captured version
table places it in `factory_setting`; earlier example-layout labels were
incorrect. No preserved console log contains an
operator-issued NAND write or erase. The earlier block was not independently
reread, so a 2026-09-02 read artifact remains possible; an autonomous erase is
also not excluded. The cause and time are unknown.

**Media volume was once observed at zero, cause unknown.** The speaker appeared
completely dead: the digital path was healthy, ALSA reported `RUNNING` with an
advancing timestamp, the DSP acknowledged gain changes with `EVENT_NEW_DAC_GAIN`,
and nothing was muted. `com.harman.volumeGet` reported `music.volume` as `0`
while `system.volume` was `70`. Setting the media volume restored sound. Two
explanations were tested and disproven: the host's PulseAudio sink volume does
not propagate over AVRCP, and changing the host sink during playback does not
reset it. The connect ceiling does not explain it either, because that lowers a
new transport to twelve rather than to zero. A speaker that silently drops to
zero with no indication is a real defect and the cause is still unknown.

**AVRCP absolute volume is not wired up.** BlueALSA logs `Couldn't set BT device
volume: No such property 'Volume'` and `Couldn't open mixer: Mixer element not
found`. That RAM trial did not establish remote absolute-volume synchronization.
The owned physical rotary path changes BlueALSA media volume; the raw DSP gain
API is a separate control and should not be presented as its replacement.

**networkd leaves a cosmetic shutdown error if its runtime directory vanishes.**
The service now detects the loss and exits so its supervisor can restart it from
a clean state, but the teardown path still logs `open lease lock` on the way out.

**Error and recovery paths have not run on real hardware.** Window readiness
timeouts, a supplicant that refuses to terminate, DHCP failure recovery, MCU DAC
and amplifier rollback, and DSP download cancellation are covered only by host
fault injection. Every hardware defect found so far came from a path executing on
the device for the first time, so these should not be treated as proven.

### Historical RC control and recovery findings

Holding Mic-Mute opens the owned provisioning window. Its authenticated local
handoff replaces the vendor phone-app setup flow; the physical gesture is not
new, since the 2017 manual also documents Mic On/Off hold for Wi-Fi setup.
The top action button publishes
`action-long` and is deliberately ignored. RC7 booted and passed
cold-boot acceptance, but its first-ever `sta-uap` boot exposed a latent defect:
`reinvoke-provision-windowd` crash-looped on `hostapd library path is not
root-controlled`, because `/opt/reinvoke` directories ship `root:root 0775` and
the path check masked `0022`, misreading root-group write as untrusted access.
Every earlier boot was station-only, so the uAP branch had never executed. RC8
carries RC7 unchanged apart from two independent fixes: `windowd` now trusts
group write only when the group is root, and packaging normalizes staged
`/opt/reinvoke` directories to `0755`, which also removes a host-umask
dependency from the image hash. Either fix alone unblocks the boot. The
predicate fix was validated live on the running RC7 rootfs, whose directories
remain `0775`, so the repair is causally isolated. RC6 retains RC5's MCU,
kernel, DSP, and
physical-control fix and adds the reviewed BlueZ ObjectManager connected-state
fix. RC5's first cold boot
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

Physical-button policy is dispatched from decoded MCU events rather than
trusting WAMP publications as physical input. RC5 proved those events and
their policies. The packaged RC6 pairing agent proves `pairing -> connected -> off` against a
persistent RAM-only bond. Its cold-boot collector passed with all statuses zero,
and the packaged pairing-agent hash matches the candidate manifest. The
operator was absent for the rear connected light.

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

Source matching was tested separately without disrupting the live WAMP clients.
An isolated listener on port 19997 used two temporary local source aliases.
With the same ordered source-accept and default-drop rules, the allowed alias
connected and echoed data while the denied alias timed out.
Packet counters advanced on both rules. The aliases, listener, and test rules
were then removed, and the original six WAMP rules compared byte for byte with
their pre-test capture. Combined with the direct port-9999 DROP test, this
closes the allowlist mechanism without requiring an unrelated network device.

This kernel also sets `pid_max` to 4096, and the PID counter was observed
wrapping within 132 seconds of ordinary supervision churn. PID reuse is a
routine event here, so nothing may treat a recorded PID as proof of identity.
The MCU pairing control resolves `/proc/<pid>/exe` and refuses to signal a
mismatched executable, which keeps a recycled PID from receiving a control
signal intended for the pairing agent.

Entering yellow mode requires the recovery button held at power-on, so cold-boot
gates cannot be driven from software and need the operator present.

Persistent user settings, additional cloud/voice features, and full semantics
for every button and LED asset are outside this accepted milestone. Native
NAND startup itself is demonstrated by candidate 02.
