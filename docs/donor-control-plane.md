---
title: Donor control-plane procedures
description: Harman WAMP procedures used by the donor Bluedroid service
ms.date: 2026-09-14
ms.topic: reference
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
| `source.register` | identifiers | stub returning `{}` |
| `source.get-active` | identifiers | stub returning `bluetooth` |
| `source.start` | nobody | never observed being called |
| `source.nowPlayingUpdate` | nobody | called during playback; 33 refusals in one A2DP session |
| `extStateUpdate` | nobody | two calls observed |
| `music.stateChanged` | nobody | two calls observed |
| `error` | nobody | |

## The source family

`com.harman.source.*` is the audio source framework. A source registers itself,
the system asks which source is active, a source is started, and the active
source reports what is playing. On the stock unit that metadata drove Cortana's
answers and the companion application.

`nowPlayingUpdate` carries the AVRCP payload: album, artist, track, and cover
art URLs. The donor pushes it whenever the connected phone changes track.

## Why it is not implemented

Nothing on this unit consumes it. There is no screen and no companion
application, so an implementation would write track names into a log that
nothing reads. It is recorded here rather than built.

It becomes worth building if any of these appear:

* a display or LED behaviour that should reflect what is playing
* a local web or API surface for the speaker
* voice interaction that needs to answer "what is playing"

## A correction worth keeping

An earlier note claimed a mismatch between the donor's `source.get` and this
runtime's `source.get-active`. There is none. The regular expression used to
read the donor's strings stopped at the hyphen, so one name looked like two.
The donor calls `get-active`, which is what is implemented.

That is the second time in this work a malformed pattern produced a confident
and wrong conclusion about the donor. Read the donor's strings with a pattern
that includes hyphens and dots, and confirm against a live log before acting.
