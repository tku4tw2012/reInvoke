---
title: Current reInvoke product and architecture contract
description: Runtime ownership, local policy and interface contracts for the Invoke
ms.date: 2026-09-12
ms.topic: overview
---

reInvoke targets a local assistant endpoint on existing Invoke hardware.
The implemented milestone is an owned runtime, not a complete assistant.
The [native result ledger](native-nand-platform.md#current-result) is the
authority for candidate-specific acceptance; the contracts below describe
implementation unless a measurement is explicitly identified.

## Current product contract

Normal operation starts from NAND. Host-loaded U-Boot/RAM Linux remains the
development and observed recovery path. Wi-Fi credentials, Bluetooth bonds and
preferences are volatile; no persistent-state design has been selected.

Installed candidate 03 has startup, provisioning and SSH-negotiation evidence
but no login. Candidate 02 owns the broader native audio/control baseline;
detailed privacy, firewall and restart checks remain RAM-scoped.
The actual native kernel, PID 1 and mount table remain unread through a shell.

The original Cortana system and final Bluetooth-oriented `12.2134.0` donor
are historical dependencies, not reInvoke product policy. See
[firmware generations](firmware-reference.md#firmware-generations) and
[remaining work](revival-roadmap.md#remaining-work).

## Accepted runtime architecture

Owned PID 1 supervises long-lived services with bounded logs. Shutdown stops
the MCU policy owner first, muting outputs before audio producers exit.

| Owner                         | Responsibility                                           | Interface                                                      |
| ----------------------------- | -------------------------------------------------------- | -------------------------------------------------------------- |
| NAND bootstrap and paired BSL | Select and verify the read-only runtime                  | Retained vendor boot/kernel payloads                           |
| MCU service                   | Inputs, LEDs, amplifier/DAC mute, public Mic-Mute policy | I2C, private DSP socket, WAMP                                  |
| DSP service                   | Firmware, reset, command correlation and readiness       | SPI/GPIO, seven WAMP registrations, private microphone control |
| BlueZ/BlueALSA                | A2DP Sink and PCM playback                               | Private D-Bus, ALSA, active-PCM lease                          |
| Capture owner                 | Supervise `arecord`, select left channel, gate records   | ALSA `hw:1,0`, root-only Unix socket                           |
| Network/provisioning services | Station lifecycle, setup window and credentials          | TLS parser, Unix handoff, fixed executables                    |
| Bonefish                      | MessagePack WAMP compatibility routing                   | RawSocket 9999 and WebSocket 9998                              |

PCM uses ALSA, not the DSP control daemon's SPI channel. The
[system diagram](../README.md#system-overview) shows these boundaries.

### MCU and DSP ownership

`reinvoke-mcu-interface` owns MCU transactions, input decoding, animation and
speaker/privacy policy. Shared expander updates preserve the DSP reset bit.

`reinvoke-dsp-interface` owns `/dev/spidev0.0`, handshake GPIOs, GPIO5
pin-function transition, expander reset and host-loaded `dsp-img.ldr`.
Download uses manual chip select, then restores message mode through a
read-modify-write preserving MCU GPIO3. DSP boot does not unmute the amplifier
or DAC. MCU transport is I2C, not an inferred UART.

The [MCU](emulation/mcu-boundary.md) and [DSP](emulation/dsp-boundary.md)
references retain transport details. Exact part models remain unresolved.

### Speaker safety and volume

The amplifier and DAC initialize muted. Policy opens the physical path only
while ALSA is `RUNNING`, the active-PCM lease thread matches ALSA ownership,
and that thread resolves to the packaged player. Loss of that authorization
reasserts mute after a 1.5-second holdoff; shutdown requests mute directly.
The lease identifies active playback, not nonzero or audible samples.

`com.harman.volumeSet` takes `[value, "music"]`; raw
`com.harman.dsp.volumeSet` takes one gain byte. DSP gain cannot overcome media
volume zero. Newly acquired BlueALSA transports start at maximum volume;
connect policy caps them at twelve without raising a quieter transport.
Rotary control changes media volume. See [speaker control](emulation/owned-speaker-control.md).

### Microphone privacy boundary

The MCU controller is the sole privacy-policy authority:

1. Physical `micmute` events reach it before WAMP publication, independently
   of router availability.
2. `com.harman.dsp.micMute` reaches that same process-lifetime controller.
3. Confirmed state is atomically retained in mode-0600 RAM state across
   service restarts within the boot.
4. DSP opcode `0x09` travels through the mode-0600
   `/run/reinvoke/dsp-mic-control.sock`.
5. DSP restart restores retained mute before readiness. Indeterminate unmute
   triggers a mute attempt; failed reconciliation retries independently of WAMP.

Moving public Mic-Mute to the MCU reduced the owned DSP service from the
donor's eight WAMP registrations to seven, preventing a policy bypass.

Capture selects the donor's left voice-recognition channel and emits mono
48 kHz `S32_LE`, 256-frame records on
`/run/reinvoke/mic-capture/audio.sock`. The right channel is call audio,
not six additional raw microphones.

The owner polls `/run/reinvoke/microphone-state` every 100 ms and drops periods
when state is muted or invalid. DSP identity changes restart capture.
This is not a synchronous fence for queued audio; ALSA `hw_params` can also
overwrite a previous DSP route. Consumers use the owned socket, not raw ALSA.

Root is trusted. Privacy is neither an electrical disconnect nor protection
from arbitrary root access. Beamforming, AEC, AGC and noise-reduction activation
are unproven. The [capture reference](microphone-capture.md) owns measurements,
the wire format and the [deferred synchronous design](microphone-capture.md#deferred-synchronous-privacy-design).

### Front and rear indicators

Donor disassembly and host tests establish `com.harman.ledSet`: first
positional target `front` or `back`, optional `mode`/`color` keywords.
Extra positional and unknown keyword arguments are ignored.
Modes are `off=0`, `on=1`, `dim=2`, `slow-blink=3`, `fast-blink=4`.
Front white/amber are exclusive; back ignores color. Unknown targets/front
colors send unchanged confirmed state.

The fixed command to I2C address `0x36` is:

```text
09 <front-amber> <front-white> <back> 00 00
```

State and transport are serialized; I2C failure rolls back candidate state and
propagates to the caller. The final zeros replace indeterminate donor stack
residue. These indicators are separate from top animation and privacy.

`com.harman.ledAnimate` uses checksum-gated assets. `ledOff` cancels ordinary
animation with a 41-byte clear packet: opcode `0x0e`, first-chunk flag `0x01`,
three zero 13-byte frames. Generic LED calls cannot extinguish required red
privacy indication. Full native front/rear acceptance remains open.

### Bluetooth policy

BlueZ 5.55 owns BR/EDR and A2DP/AVRCP control. Patched BlueALSA 4.0.0 handles
SBC playback; patched `bluealsa-aplay` supplies donor-compatible ALSA writes,
buffering, underrun recovery and the active-PCM lease. `bluealsa-cli` is the
per-peer volume/mute adapter. Donor Bluedroid and media supervisors do not run.

The pairing agent limits the window, peer and services. Bluetooth short press
uses `SIGUSR2` to toggle the window; long uses `SIGUSR1` to reopen it.
Signal handlers set flags; D-Bus operations stay in the dispatch loop.
Connection state comes from allowlisted `Device1.Connected`, including startup.

Atomic mode-0600 `/run/reinvoke/bluetooth-state` reports `pairing` during a
window, otherwise `connected` or `off`. MCU policy maps these to rear
slow-blink/on/off, deduplicates writes and clears invalid state without altering
top privacy. A generation guard removes stale state on producer exit.
See [Bluetooth evidence](emulation/bluetooth-stack.md).

### Network and local administration

PID 1 loads SD8887 firmware. `reinvoke-networkd` supervises DHCP and validated
RAM route/resolver state for the station supplicant.
`reinvoke-provisiond` parses a token-authenticated TLS request;
`reinvoke-wifi-applyd` revalidates the UID-0 request and starts fixed executables.
Both run as root: process separation is not an unprivileged parser sandbox.
The bounded `p2p0` AP has no gateway, DNS service or forwarding.
See the [provisioning contract](native-provisioning.md#current-replacement-components).

Bonefish binds `0.0.0.0:9998/9999` without WAMP authentication. Firewall policy
allows loopback/configured private operator sources, then drops other sources;
the operator allowlist is empty by default. PID 1 installs rules before the
router and stops autonomous startup if installation fails. Ordering and actual
DROP/source matching were tested in RAM; native enforcement remains unread.

Candidate 03 packages separate key-only Dropbear with firewall-before-listener
startup. [SSH negotiation succeeded, authentication did not](native-nand-platform.md#offline-ssh-implementation-milestone).
WAMP and host helper/ADB ports are not substitutes for native administration.

## Physical controls and factory reset

| Input                         | Implemented action                                   |
| ----------------------------- | ---------------------------------------------------- |
| Rotary                        | Coalesced media volume and compatibility publication |
| Mic-Mute short                | Privacy toggle and confirmed red ring                |
| Mic-Mute long                 | Bounded isolated Wi-Fi setup                         |
| Bluetooth short / long        | Toggle / reopen pairing window                       |
| Action short                  | Play/pause and reviewed one-shot animation           |
| Action long; Reset short/long | Compatibility publication only                       |

Some Mic-Mute presses produce no MCU event under donor and owned services;
software cannot act on an event it never receives.

The [2017 manual, page 8](https://support.harmankardon.com/on/demandware.static/-/Sites-masterCatalog_Harman/default/dwdac694e8/pdfs/Harman%20Kardon%20Invoke%20Owners%20Manual.pdf#page=8)
describes a settings reset/restart, not a firmware reinstall.
Reset at power-on is a different recovery action. reInvoke has no implemented
factory reset; persistence first needs erase scope, power-loss and rollback
semantics. No assistant action is assigned yet.

## Dependency and build boundary

Included donor assets are native boot/kernel payloads, SD8887 firmware and
calibration, host-loaded DSP code and reviewed indicators. They are
checksum-gated dependencies. Persistent MCU firmware remains in place and is
never upgraded by reInvoke. Bonefish supplies compatibility, not product policy.

The runtime excludes vendor `system-manager`, Bluedroid, `audio-ui`,
`music-source-manager`, Cortana, OTA updater, crash-dump writers and flash
utilities. Normal operation needs neither cloud services nor SSH.

Held artifacts support deterministic composition, not complete reconstruction
from a clean public clone. See [build provenance](native-nand-platform.md#build-and-reproducibility-boundary).
Historical experiments belong in the [journal](journal.md), not this contract.
