---
title: Donor control-plane procedures
description: Harman WAMP procedures used by the donor Bluedroid service
---

The donor Bluedroid binary calls twenty-four `com.harman.*` WAMP procedures.
That surface is Harman's original control plane, not something this project
designed: it is how the stock firmware coordinated Bluetooth, the DSP, the LED
ring and Cortana across separate processes.

This runtime keeps the donor binary and re-implements only the procedures it
refuses to start without. Everything else is unanswered, and most of it is
unanswered on purpose.

## What the donor expects, and what answers

| procedure | answered by | note |
| --- | --- | --- |
| `identifiersGet` | identifiers | donor exits without it |
| `deviceNameGet` | identifiers | donor exits without it |
| `stateGet` | identifiers | opens the OOBE gate; see docs/bluetooth-enable-path.md |
| `bluetoothPairing` | donor itself | registered, driven by the pairing agent |
| `bluetooth.{next,prev,pause,resume,stop,skipTo,shuffle,repeat}` | donor itself | full media surface |
| `volumeGet`, `volumeSet`, `volumeChanged` | mcu-interface | reaches the amplifier through the DSP |
| `source.register` | source-manager | records the source and its capabilities |
| `source.get-active` | source-manager | returns the source holding the speaker |
| `source.get-registered` | source-manager | returns every admitted source |
| `source.start` | source-manager | hands the speaker to a source, stopping the incumbent |
| `source.flush` | source-manager | drops registrations |
| `music.{pause,resume,stop,cmdPlayPause}` | source-manager | routed to whichever source is active |
| `source.nowPlayingUpdate` | mcu-interface | called during playback; 33 refusals in one A2DP session |
| `extStateUpdate` | mcu-interface | two calls observed |
| `music.stateChanged` | source-manager | published when the active source changes |
| `error` | source-manager | logged |

## The source family

`com.harman.source.*` is the audio source framework. A source registers itself,
the system asks which source is active, a source is started, and the active
source reports what is playing. On the stock unit that metadata drove Cortana's
answers and the companion application.

`nowPlayingUpdate` carries the AVRCP payload: album, artist, track, and cover
art URLs. The donor pushes it whenever the connected phone changes track.

## What is implemented

Candidate 05.8.10 implements the framework in `reinvoke-source-manager`. Two
earlier stubs in the identifiers service answered `register` and `get-active`
from a fixed table, and `get-active` returned the wrong shape, so a second
source could never have taken the speaker. The service now keeps real
registrations, arbitrates which source holds the speaker, and routes the
source-agnostic `music.*` verbs to whichever source is active.

The semantics follow Harman's own `MusicSourceStart.pm`: starting a source
stops the incumbent first, and the change is published on `music.stateChanged`.
The donor stack is admitted at startup so it can claim the speaker as it always
did.

`nowPlayingUpdate` carries the AVRCP payload. Nothing on this unit displays it,
so mcu-interface accepts and logs it rather than refusing it. Refusing it was
the previous behaviour and produced 33 errors in a single listening session.

## Transport controls are passthrough, not local

`bluetooth.{next,prev,pause,resume}` are AVRCP passthrough to the phone:
`send_pass_through_cmd` with, for example, key id 68 for PLAY, pressed and
released. The phone stops sending audio; nothing local is paused. A local test
player ignores AVRCP entirely and keeps running, which is exactly what was
observed and briefly misread as the command failing.

## A correction worth keeping

An earlier note claimed a mismatch between the donor's `source.get` and this
runtime's `source.get-active`. There is none. The regular expression used to
read the donor's strings stopped at the hyphen, so one name looked like two.
The donor calls `get-active`, which is what is implemented.

That is the second time in this work a malformed pattern produced a confident
and wrong conclusion about the donor. Read the donor's strings with a pattern
that includes hyphens and dots, and confirm against a live log before acting.

## What is left, and why

`tools/donor-contract/compare.sh` reports 106 of the donor's 165 procedures as
unimplemented. Most of that number is not work outstanding. Grouped honestly:

| group | count | status |
| --- | --- | --- |
| Cortana and Spotify | 38 | out of scope for this unit |
| Factory, demo and test line | 21 | out of scope |
| MCU firmware upgrade | 6 | out of scope |
| Registered by the donor itself | 8 | not ours to implement |
| Error URIs, not procedures | 5 | nothing to register |
| Services this runtime does not have | 3 | see below |
| Blocked on unknown MCU opcodes | 10 | see below |
| Namespace prefixes | 11 | not procedures; see below |

Three names belong to donor services with no counterpart here:
`ready.audio-ui`, `heartbeat.connection-manager` and
`connection-manager.shutdown`. Registering a lifecycle topic for a service that
does not exist would assert a readiness nothing can honour, so they are left
alone. `system-manager.shutdown` is the same case: init supervises this
runtime, and stopping it is what `reboot` already does.

`vui.uicommand` belongs to `visual-ui`, which is a terminal test harness that
draws with ANSI escapes rather than the LED ring it was mistaken for. It is
part of the excluded test line.

## Namespace prefixes are not gaps

Eleven of the strings this comparison used to count as unimplemented were never
procedures. The donor builds some names at runtime by concatenating a prefix
with a method, so `com.harman.aui.` sits in `audio-ui` as string-building
material, directly beside `registerVoiceAgent`. Counting it as a gap created
work that could never be completed, because there is nothing behind it.

Others are namespace roots that only ever appear with something after them:
`com.harman.music`, `com.harman.bluetooth`, `com.harman.error`. And
`com.harman.ready.` and `com.harman.heartbeat.` are the lifecycle prefixes this
runtime already builds the same way.

`compare.sh` now recognises both shapes and reports them separately with a
count, rather than leaving them to be hand-filtered by whoever reads the list.
An earlier pass filtered them by hand and called them "truncated fragments",
which described the symptom and left the tool still producing them.

## The MCU command space, recovered

An earlier pass deferred this group on the grounds that the opcodes were
unknown. They were not unknowable: disassembling the donor's `mcu-interface`
yields each one. `SetRGBLEDBrightness`, `setDeviceColor`, `getDeviceColor` and
`setmcupowermode` are implemented, and [the command map](mcu-command-map.md)
records the opcodes, the payloads and the method.

The same reading corrected three assumptions. `powerdspcontrol` is not an MCU
command at all, `restart` shells out through `system()`, and `terminate` never
writes a frame. Implementing those three as MCU frames, which is what the
earlier grouping implied, would have put invented bytes on the bus.

Still unimplemented: `GetHWID` and `SetHWID`, and the firmware upgrade family,
which is out of scope by decision rather than difficulty.

The vendor defaults for the LED group are known from the settings database:
`LED_INTENSITY=50`, `LED_WHITE=50`, `LED_RGB=000000`, `LED_FLASHING=OFF`.
The vocabulary is known too, because the donor binary spells it out: black,
colour, front, amber, back, standby mode, operational mode. What is missing is
the single byte that selects each command.

This runtime already drives the indicator LEDs with opcode `0x09` and the
frame `[0x09, amber, white, back, 0, 0]`, where each colour byte is a mode
rather than a level: off, on, dim, slow blink, fast blink. Brightness is a
different command, and its opcode is unknown.

Guessing is not acceptable here. MCU frames are six bytes with the opcode in
byte 0, and the same command space contains `startmcuupgrade`. An opcode that
is wrong by one could put the microcontroller that owns power, the buttons and
its own firmware into a state this project cannot recover from. These stay
deferred until an opcode is established by observation rather than by guess.

## What aui and vui stand for

Both are user interfaces, distinguished by which sense they use.

| prefix | meaning | evidence | owned by |
| --- | --- | --- | --- |
| `aui` | Audio UI | namespace `aui::AudioUI`, class `WampAudioUI` | `audio-ui` |
| `vui` | Visual UI | class `WampVisualUI` | `visual-ui` |

`mcu-interface` has no `vui::` namespace of its own. It registers the
`com.harman.vui.*` procedures because it owns the hardware those procedures
act on: the LED ring, the buttons, the amplifier mute, MCU power. The Visual UI
is the speaker's light and touch; `visual-ui` is the client that drives it.
This runtime answers the same contract for the same reason.

The Audio UI is the speaker's voice: not music, but everything the device says
back to you. In the donor it is a Boost.Statechart machine, `aui::System`, with
orthogonal regions that run at once:

* `AlertIdle` to `AlertActive` to `AlertPlaying` or `AlertPaused`
* `VoiceIdle`
* `MicmuteIdle`
* `BluetoothOff`
* `UiHandler`

Two dedicated players sit under it, `aui::g_alert_player` and
`aui::g_voice_player`, separate from music. That is what the whole ducking
mechanism exists for: an alert or a prompt plays on its own player while music
is attenuated underneath, then restored.

Knowing this corrects an earlier grouping. `aui.alertPlay`, `aui.alertCancel`,
`aui.callAccept`, `aui.callReject` and `aui.dialogTrigger` were filed as
Cortana-adjacent and dismissed. They are not Cortana procedures; they drive the
alert and voice regions of the Audio UI. The assets are still on the donor
filesystem, and `usr/share/sounds/alarm/default.mp4` and
`usr/share/sounds/timer/default.mp4` are not voice-assistant specific.

What is Cortana-specific is the trigger. Nothing on this unit sets a timer or
receives a call, so the alert player has nothing to announce.

## Ducking

`volume.setDuck` attenuates rather than mutes, which is how a prompt spoke over
music on the stock unit without stopping it. The donor held a map of named duck
requests in `aui::VolumeManager` so that an alert and a voice prompt could
overlap without either one restoring full volume while the other was still
speaking; this runtime keeps the same map and applies the deepest request in
force.

Two duck depths exist, `soft` and `hard`, matching the donor's own names. The
attenuation each one applies is **this project's choice, not the vendor's**: no
file recovered from this unit records the donor's ratios. They are stated here
so that a later capture can correct them rather than leaving the numbers to be
rediscovered.

A duck never changes the level the user chose. Releasing every duck returns to
that level exactly, and mute still silences regardless of ducking.

It is worth stating plainly that **nothing in this runtime currently calls it**.
`com.harman.volume.setDuck` is registered and answerable, and the behaviour
behind it is tested, but there is no alert player and no voice agent here to
duck for. It is working machinery waiting for a caller, which is a different
thing from a working feature. If an alert player is ever added, this is the
piece it will need and it will already be correct.
