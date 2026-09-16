---
title: Vendor button semantics
description: Button-to-action table recovered from the stock audio-ui state machine
ms.date: 2026-09-16
ms.topic: reference
---

What each physical control did on the retail Invoke, recovered from the stock
firmware rather than assumed. The [MCU boundary](emulation/mcu-boundary.md)
covers the frame decoding that produces these event names; this page covers
what the vendor did with them afterwards.

## Why this exists

An earlier answer in this project claimed the top tap woke Cortana. That was
inferred from seeing `com.harman.cortana.dialogTrigger` in the same binary, and
it was wrong: the table below assigns `voice-trigger` to the *long* press. The
short tap pauses music and cancels whatever is in progress.

## How it was recovered

Stock `usr/bin/audio-ui` subscribes to the keypress topic and drives a
`boost::statechart` machine. Its transitions are built as
`std::map<std::string, std::string>` initialiser lists, one map per state.

The binary is `EXEC`, not PIE, with `.rodata` at vaddr `0x117688` and file
offset `0x107688`, so a string's virtual address is its file offset plus
`0x10000`. String addresses reach the `std::pair` constructors through
`movw`/`movt` immediate halves rather than literal pools or pointer tables,
which is why neither a `.word` search nor a pointer scan finds them. Replaying
those register writes while walking the disassembly recovers both sides of
every pair: 38 entries whose key begins with `btn-`.

## Recovered table

| Event | Actions the vendor dispatched |
| --- | --- |
| `btn-action` | `voice-cancel`, `call-reject`, `music-pause`, `call-accept`, `alert-cancel`, `voice-surprise`, `play-intro` |
| `btn-action-long` | `voice-trigger`, `call-reject`, `voice-surprise` |
| `btn-bluetooth` | `bluetooth-pair`, `bluetooth-cancel`, `alert-cancel`, `voice-surprise` |
| `btn-micmute` | `micmute`, `alert-cancel` |
| `btn-micmute-long` | `wifisetup-enter`, `wifisetup-reinit` |
| `btn-volumeup` / `btn-volumedown` | `volumeup` / `volumedown`, `alert-cancel` |

A button appears several times because each entry belongs to a different
state, among them `music:playing` and `music:buffering`. The short tap is
therefore context sensitive: pause what is playing, cancel what is speaking,
answer or reject a call, dismiss an alert.

`btn-bluetooth-long` is absent from `audio-ui` because the vendor handled it
inside `mcu-interface`, which carries the strings `Bluetooth button
long-pressed. Sending bugreport.` and `Bluetooth button long pressed, but
bugreport is disabled in this speaker`. It was a diagnostic upload gated behind
a flag, not a pairing control.

## Evidence grade

Treat these differently, because they were established differently.

* Event to action name is **recovered from the binary's own table**.
* Bluetooth long press as bugreport is **quoted from literal strings**.
* Action name to WAMP procedure is **inference from name correspondence**.
  `voice-trigger`, `music-pause` and `bluetooth-pair` line up with
  `com.harman.cortana.dialogTrigger`, `com.harman.music.pause` and
  `com.harman.bluetoothPairing`, all present in the same binary, but the call
  sites were not traced.
* `voice-surprise` and `play-intro` are **names only**; their behaviour is
  unknown.

## What this means for reInvoke

Cortana was retired, so `voice-*` and `call-*` have no counterpart here. Two
consequences follow.

The vendor routed the short tap to `music-pause` through
`com.harman.music.pause`, which its `music-source-manager` forwarded to the
active source. This runtime has no such router, so the equivalent call is
`com.harman.bluetooth.pause` and `com.harman.bluetooth.resume`, which the donor
Bluedroid stack registers directly.

Wi-Fi setup belonged to `btn-micmute-long`. The LED player currently starts
`L_302_d_wifisetup` on `bluetooth-long`, which puts the Wi-Fi setup animation
on the wrong control; that mapping arrived in `217d052` without a recorded
rationale.
