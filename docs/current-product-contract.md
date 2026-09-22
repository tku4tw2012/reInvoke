---
title: Current reInvoke product and architecture contract
description: Runtime ownership, local policy and interface contracts for the Invoke
---

reInvoke targets a local assistant endpoint on existing Invoke hardware.
The implemented milestone is an owned runtime, not a complete assistant.
The [native result ledger](native-nand-platform.md#current-result) is the
authority for acceptance on a given build; the contracts below describe
implementation unless a measurement is explicitly identified.

## Current product contract

Normal operation starts from NAND. Host-loaded U-Boot/RAM Linux remains the
development and observed recovery path. Settings are held in `/persist`, a
yaffs2 volume on the `app` partition, covering Wi-Fi credentials, Bluetooth
stack configuration and selected preferences; see its
[scope and limits](native-nand-platform.md#settings-persistence-and-bounded-network-adb).

The running build serves a root SSH login and USB ADB from a cold boot, so the
native kernel command line, process table and mount table are directly
readable on the device rather than inferred from composition. Earlier
candidate builds reached SSH negotiation without a login, and their audio and
control results were separately scoped; that history is in the
[decision record](journal.md).

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
| MCU service                   | Inputs, LEDs, amplifier/DAC mute, public Mic-Mute        | I2C, private DSP socket, WAMP                                  |
| DSP service                   | Firmware, reset, command correlation and readiness       | SPI/GPIO, seven WAMP registrations, private microphone control |
| Donor Bluedroid stack         | A2DP sink, AVRCP and SBC playback                        | Android property service, binder, ALSA                         |
| Capture owner                 | Supervise `arecord`, select left channel, gate records   | ALSA `hw:1,0`, root-only Unix socket                           |
| Network/provisioning services | Station lifecycle, setup window and credentials          | TLS parser, Unix handoff, fixed executables                    |
| Bonefish                      | MessagePack WAMP compatibility routing                   | RawSocket 9999 and WebSocket 9998                              |

PCM uses ALSA, not the DSP control daemon's SPI channel. The
[system diagram](../README.md#system-overview) shows these boundaries.

### MCU and DSP ownership

`reinvoke-mcu-interface` owns MCU transactions, input decoding, animation and
speaker and mic-mute state. Shared expander updates preserve the DSP reset bit.

`reinvoke-dsp-interface` owns `/dev/spidev0.0`, handshake GPIOs, GPIO5
pin-function transition, expander reset and host-loaded `dsp-img.ldr`.
Download uses manual chip select, then restores message mode through a
read-modify-write preserving MCU GPIO3. The DSP service itself never touches
the amplifier or DAC mute bits; the MCU service opens them once, after the DSP
accepts its first volume. MCU transport is I2C, not an inferred UART.

The [MCU](emulation/mcu-boundary.md) and [DSP](emulation/dsp-boundary.md)
references retain transport details. Exact part models remain unresolved.

### Speaker output and volume

The amplifier and DAC initialise muted and are opened once, after the DSP
accepts its first volume. Nothing polls and nothing re-mutes; after that they
change only through `muteampcontrol` and `mutedaccontrol`, and on shutdown.
The donor does the same: its initialisation mutes both and never unmutes, and
`system-manager`, which this runtime replaces with `/init`, is the only donor
binary that calls `muteampcontrol`.

Two earlier designs are withdrawn. One held the amplifier closed unless ALSA
was `RUNNING` and a lease thread resolved to one packaged player, which was
this project's invention and silenced every sound the runtime did not itself
render, including the vendor's startup chime. The other opened the outputs
during initialisation, which put the amplifier live for DSP bootup and the
first gain change and was audible on this unit as pops at startup.

`com.harman.volumeSet` takes `[value, "music"]`; raw
`com.harman.dsp.volumeSet` takes one gain byte. Rotary control changes media
volume. See [speaker control](emulation/owned-speaker-control.md).

Candidate 05.8.6 and earlier recorded the level without applying it, so the
speaker played at whatever gain the DSP booted with and the rotary control
moved a number that reached no hardware. 05.8.7 pushed the level to
`com.harman.dsp.volumeSet`, which is audible and is what ships.

The donor carried the user's volume on an ALSA softvol control instead:
`aui::VolumeManager` calls `add_softvol` and steps it from
`softvol_fading_tick`, and the vendor settings database records
`current_volume=80`. That control does not exist in this runtime. Nothing
defines a softvol plugin, and the default pointed at card 0, which is the
Loopback device, so every fade failed and logged while the DSP call carried
the level. The fade is retained behind `--softvol-control` and is off by
default. Restoring it is open work: it would make volume changes fade rather
than step, and move the user's level off DSP gain.

Because the level lands on DSP gain, the dial carries the donor's curve
itself. That control was declared in `etc/asound-product.conf` as a plain
`type softvol` with no `min_dB`, `max_dB` or `resolution`, so it took the
plugin defaults of 0..255 over 51 dB, which this project confirmed against
the hardware at numid 97 in 0.2 dB steps. Percent went onto it linearly, so
the dial was linear in decibels and logarithmic in amplitude.

The curve here is anchored to this unit rather than copied: its range is
solved so that dial 34 produces gain 5, measured as comfortable on
2026-09-19, and dial 100 produces 90, reported as loud. Copying the plugin's
own 51 dB put the comfortable point at gain 2. `defaultVolume` is 34 and the
startup chime follows the same curve, with `cueTargetPeak` set so it renders
at the peak measured as right at that position.

Passing percent straight through, as builds before 05.8.13 did, made the dial
linear in amplitude: the comfortable point sat at 5 of 100, so almost the
whole travel was above it.

Those two numbers only mean anything together. The build shipped
`defaultVolume` of 80 for several releases, which was the vendor's
`current_volume` and correct while the level rode softvol; when softvol was
disabled the number stayed, so the unit booted at 80 on a scale where 3 is
comfortable and 90 is loud. Nothing revealed it because music was never
played on those builds and the chime had its own attenuation.

### Bluetooth audio rendering

The donor stack calls `defaultServiceManager()` from its A2DP
`connection_state_cb`. The donor `libbinder` spins there until the Android
property `service.servicemanager` reads back set, and `servicemanager` only
sets it once a property service answers. Without one the callback never
returns: traced on 05.8.6, the callback and the first
`Waiting for initialization` share a millisecond on the BTIF thread, which
then spins 1257 times and emits no further `btif_av` event. The state machine
never leaves `opening`, the media task never decodes, and all 707 delivered
SBC packets are discarded while the amplifier stays muted because no renderer
ever opens the PCM.

Candidate 05.8.7 therefore publishes an Android property area and the
`property_service` socket before `servicemanager` and the donor stack start.
The layout is the donor's, recovered from its own `lib/libglibc_bridge.so`:
the magic at `+8` is `PROP`, but the version at `+12` is `0x45434F76` where
upstream bionic uses `0xFC6ED0AB`, so an implementation written from the
published specification is unmapped without a diagnostic. Readers never open
the area by path; the loader parses `ANDROID_PROPERTY_WORKSPACE` as
`<fd>,<size>` and maps that inherited descriptor read-only.

The kernel already provides `/dev/binder` and `ashmem`, so nothing else was
required. See [propertyd](../tools/propertyd/main.go).

### Microphone mute boundary

The MCU controller owns microphone mute state:

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
donor's eight WAMP registrations to seven, so a single controller holds the
state the button and the DSP procedure both change.

Capture selects the donor's left voice-recognition channel and emits mono
48 kHz `S32_LE`, 256-frame records on
`/run/reinvoke/mic-capture/audio.sock`. The right channel is call audio,
not six additional raw microphones.

The owner polls `/run/reinvoke/microphone-state` every 100 ms and drops periods
when state is muted or invalid. DSP identity changes restart capture.
This is not a synchronous fence for queued audio; ALSA `hw_params` can also
overwrite a previous DSP route. Consumers use the owned socket, not raw ALSA.

The mute is a software state, not an electrical disconnect. Beamforming, AEC, AGC and noise-reduction activation
are unproven. The [capture reference](microphone-capture.md) owns measurements,
the wire format and the [deferred synchronous design](microphone-capture.md#deferred-synchronous-mute-design).

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
residue. These indicators are separate from top animation and mic mute.

`com.harman.ledAnimate` uses checksum-gated assets. `ledOff` cancels ordinary
animation with a 41-byte clear packet: opcode `0x0e`, first-chunk flag `0x01`,
three zero 13-byte frames. Generic LED calls cannot extinguish required red
mic-mute indication. Full native front/rear acceptance remains open.

### Bluetooth policy

The donor Bluedroid stack owns BR/EDR, A2DP/AVRCP and SBC playback, and
renders in-process through `BtSocketHandler::OpenAlsa`. It registers the
`com.harman.bluetooth` transport procedures itself, which is why they work
without this project implementing them. See
[Bluetooth audio rendering](#bluetooth-audio-rendering) for what it needs from
the property service.

This paragraph previously described BlueZ 5.55, BlueALSA 4.0.0,
`bluealsa-aplay` and `bluealsa-cli` as the audio path, and stated that donor
Bluedroid does not run. That was reversed by commit 225183e: BlueZ and
BlueALSA were removed and Bluedroid is what runs. The text was not updated,
and the error survived several releases.

The pairing agent limits the window, peer and services. Bluetooth short press
uses `SIGUSR2` to toggle the window; long uses `SIGUSR1` to reopen it.
Signal handlers set flags; D-Bus operations stay in the dispatch loop.
Connection state comes from allowlisted `Device1.Connected`, including startup.

Atomic mode-0600 `/run/reinvoke/bluetooth-state` reports `pairing` during a
window, otherwise `connected` or `off`. MCU policy maps these to rear
slow-blink/on/off, deduplicates writes and clears invalid state without altering
top mic-mute ring. A generation guard removes stale state on producer exit.
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
| Mic-Mute short                | Mute toggle and confirmed red ring                   |
| Mic-Mute long                 | Bounded isolated Wi-Fi setup                         |
| Bluetooth short / long        | Toggle / reopen pairing window                       |
| Action short                  | Play/pause and reviewed one-shot animation           |
| Action long; Reset short/long | Compatibility publication only                       |

Some Mic-Mute presses produce no MCU event under donor and owned services;
software cannot act on an event it never receives.

The [2017 manual, page 8](https://support.harmankardon.com/on/demandware.static/-/Sites-masterCatalog_Harman/default/dwdac694e8/pdfs/Harman%20Kardon%20Invoke%20Owners%20Manual.pdf#page=8)
describes a settings reset/restart, not a firmware reinstall.
Reset at power-on is a different recovery action. reInvoke has no implemented
factory reset; 04's settings layer does not implement a reset or reflash
migration operation. No assistant action is assigned yet.

## Where things live on the device

Adopted from the donor rather than invented, because the donor had already
answered this and two different answers had drifted into place here.

| kind | location | donor precedent |
| --- | --- | --- |
| identity | flat in `/etc` | `/etc/version`, `/etc/distro_version`, `/etc/build.info` |
| product config and manifests | `/etc/reinvoke/` | `/etc/podium/`, named for the vendor's platform |
| helper scripts init sources | `/usr/libexec/reinvoke/` | `/usr/libexec/dbus-daemon-launch-helper` |
| runtime state | `/run/reinvoke/` | flat `/run/dnsmasq.pid`; the directory is ours |

**reinvoke** is the product. **nand-pilot** is one subsystem of it, the part
that boots and writes NAND, and it remains the name of the builder's source
directory because that is what it builds. It should not name files that
describe the whole image, and it did: `/etc/nand-pilot/build-id` sat beside
`/etc/reinvoke-release`, `/usr/libexec/nand-pilot` beside `/run/reinvoke`.

Two facts about the build appear twice, in `/etc/reinvoke-release` and in
`/etc/reinvoke/build-id`. That is not a defect to tidy away: the donor does
the same, shipping `/etc/distro_version` and `/etc/version.txt` with
identical contents, because a human-readable banner and a machine-readable
token have different readers.

### Builder paths in shipped binaries

No shipped binary carries the builder's absolute paths. Getting there was not
obvious, so the reason is recorded.

`-trimpath` rewrites source paths to the **module** path. In GOPATH mode there
is no module path to rewrite to, so the flag is accepted and does nothing, and
the builder's home directory survives into the binary. Three binaries were
clean and four were not, and the difference was entirely whether the build
script happened to `cd` into a directory holding a `go.mod` first:

| binary | before | after |
| --- | --- | --- |
| `reinvoke-status` | 3 | 0 |
| `reinvoke-propertyd` | 3 | 0 |
| `reinvoke-source-manager` | 7 | 0 |
| `reinvoke-identifiers` | 5 | 0 |
| `reinvoke-mcu-interface` | 0 | 0 |

`build.sh` now builds each from inside its own module, and `status` gained the
`go.mod` it was missing. A subshell `cd` rather than `go build -C`: that flag
arrives in Go 1.20 and the reviewed toolchain here is 1.18, which rejects it.

It is cosmetic on the device and it is not cosmetic in a repository. The same
leak put three x86-64 binaries into git history carrying the builder's home,
which had to be rewritten out of every commit.

## Dependency and build boundary

Included donor assets are native boot/kernel payloads, SD8887 firmware and
calibration, host-loaded DSP code and reviewed indicators. They are
checksum-gated dependencies. Persistent MCU firmware remains in place and is
never upgraded by reInvoke. Bonefish supplies compatibility, not product policy.

The runtime excludes vendor `system-manager`, `audio-ui`,
`music-source-manager`, Cortana, OTA updater, crash-dump writers and flash
utilities. Normal operation needs neither cloud services nor SSH. The donor
Bluedroid stack is shipped and supervised; this list said otherwise until
05.8.11, which was stale rather than a change of intent.

`system-manager` is excluded because it is the vendor's service supervisor:
it reads `/etc/podium/podium.conf` and starts the router and daemons, which
`/init` does here instead. It also carried one responsibility unrelated to
supervision. It is the only donor binary that calls
`com.harman.vui.muteampcontrol`, so it, and not `mcu-interface`, is what
opened the amplifier after startup. That duty has to be placed deliberately
in this runtime; see the amplifier note under "Speaker output".

Two of those exclusions carry contracts this runtime partially answers.
`audio-ui` owned the system state, registering `com.harman.stateGet` and
publishing `com.harman.stateChanged`. `music-source-manager` owned source
arbitration: `com.harman.source.register`, `.start`, `.get-active`,
`.get-registered`, `.flush`, `.nowPlayingUpdate`, `.trackPositionUpdate`,
`.volumeSet` and `.volumeChanged`. This runtime answers `stateGet`,
`source.register` and `source.get-active` from a fixed table in the
identifiers service and implements none of the rest, so source switching and
published state are stubs rather than a state machine. The donor's own test
suite documents the intended behaviour, including that `source.get-active`
returns a positional URI rather than the keyword map answered here.

Held artifacts support deterministic composition, not complete reconstruction
from a clean public clone. See [build provenance](native-nand-platform.md#build-and-reproducibility-boundary).
Historical experiments belong in the [journal](journal.md), not this contract.
