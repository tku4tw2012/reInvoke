---
title: MCU command map
description: Opcodes recovered from the donor mcu-interface binary by disassembly
ms.date: 2026-09-17
ms.topic: reference
---

The microcontroller on this unit speaks a six-byte frame over I2C. This file
records which opcode does what, how each one was established, and what is still
unknown. It exists so the next person does not have to repeat the disassembly,
and so that nobody guesses an opcode.

## Why guessing is not acceptable here

The opcode is byte 0 of a six-byte frame, and the same command space contains
`startmcuupgrade` and `sendfirmwaredata`. A wrong byte can put the controller
that owns power, the buttons and its own firmware into a state this project
cannot recover from. Every entry below is either observed on hardware or read
out of the donor's own code. Nothing here is inferred from a name.

## How the transport was established

The donor's `mcu-interface` is a stripped ARM ELF. Its MCU write function sits
at `0x7dbf0` and ends:

```text
7dc44:  mov r2, r6        ; buffer
7dc48:  mov r3, #6        ; length 6
7dc4c:  mov r1, #54       ; 0x36, the I2C address
```

Six bytes to address `0x36`. That matches the transport this runtime already
uses, which is the first sign the reading is sound.

Callers build the frame on the stack and pass a pointer. The opcode is whatever
byte 0 is set to immediately before the call.

## The map

| opcode | procedure | payload | grade |
| --- | --- | --- | --- |
| `0x02` | `setmcupowermode` | byte 1: 0 standby, 1 operational | disassembly |
| `0x09` | indicator LEDs | byte 1 front amber, 2 front white, 3 back | verified on hardware |
| `0x0A` | `SetRGBLEDBrightness` | byte 1: 0 to 100 | disassembly |
| `0x0B` | `setDeviceColor` | byte 1: 0 black, 1 white | disassembly |
| `0x0C` | `getDeviceColor` | request; colour arrives on the event channel | disassembly |
| `0x0F` | `factorytestled` | byte 1: wifi 0, power 1, bt 2, red 3, green 4, blue 5, white 6 | disassembly |
| `0x07` | `GetHWID` request | reply carries the same opcode; see below | disassembly |
| `0x03` | volume arc on the ring | byte 1: 0 to 100 | **verified on hardware** |
| `0x22` | OTA flag | byte 1: 0 clear, 1 set | disassembly |
| `0x24` | heartbeat | none | verified on hardware |
| `0x01`, `0x23`, `0x25`, `0x26` | startup sequence | not decoded | verified on hardware |

Opcodes `0x03`, `0x05`, `0x08`, `0x0D`, `0x0F`, `0x10`, `0x11`, `0x14`, `0x20`
also appear in the donor's call sites. They are not decoded here because
nothing in scope needs them, and several sit in the firmware-upgrade path.

## Not MCU commands at all

Three procedures look like MCU control and are not:

* `powerdspcontrol` calls a different function with argument `0x20`. It drives a
  GPIO or expander line, not an MCU frame.
* `restart` shells out through `system()`.
* `terminate` and `getmcustatus` never write a frame; the status is answered
  from state the service already holds.

Reading those names as MCU commands would have produced three invented frames.

## Brightness, in full

The validation and the frame are both visible in one place:

```text
b4948:  cmp  r0, #100        ; reject above 100
b494c:  bls  b499c
b499c:  strb r0, [sp, #33]   ; byte 1 = brightness
b49a0:  mov  r3, #10         ; 0x0A
b49a4:  strb r3, [sp, #32]   ; byte 0 = opcode
b49b0:  bl   7dbf0           ; six bytes to 0x36
```

The donor never initialises bytes 2 to 5. They are whatever the stack held, so
the controller ignores them for this opcode. This runtime sends zeros, which is
within what the donor itself demonstrably sends.

## The ring is not an RGB device

It is reasonable to read `SetRGBLEDBrightness` and `LED_RGB=000000` and expect
three colour channels plus a level. The binary says otherwise, and it is worth
recording why so the question does not get reopened from the names alone.

Every frame is six bytes, so there is room for a triple. Across all 34 call
sites of the MCU writer, only five set more than one value byte, and only two
of those are LEDs:

* `0x09` sets three: the donor's own comparisons name them front amber, front
  white and back. Three LEDs, not three colour channels.
* `0x11` sets three, in the firmware upgrade path.

No opcode anywhere takes red, green and blue. Colour is chosen by name:
`setDeviceColor` accepts black or white, and `factorytestled` accepts a
seven-value enum that mixes LED selection with colour. "RGB" in the brightness
procedure names the LED part, not the protocol.

What `LED_RGB=000000` in the vendor settings means is unresolved. It is a
stored setting, and nothing observed here sends it to the controller as a
triple.

## Hardware identity

`GetHWID` is a request and wait, not a register read. Send `0x07` and the
controller answers on the same event channel that carries button presses,
using the same opcode. The donor polled for the answer ten milliseconds at a
time, up to 101 times, then logged "get HW ID timerout".

The reply, from the event dispatch at `0xd2cec`:

| byte | meaning |
| --- | --- |
| 0 | `0x07` |
| 1 | board revision: 0 is DV1, 1 is DV2; anything else logged "HW ID Error!" |
| 2, 3, 4 | version fields, printed by the donor as `%02d%02d%02d` |

A revision byte outside those two is refused here rather than named. This
runtime has never seen a real reply: the decode is read out of the donor's code
and the values on this unit are still unobserved.

## The event channel has a jump table

Incoming frames are dispatched on byte 0 through a table at `0xd2568`, indexed
by opcode minus one and bounds-checked at 37. That table is the authoritative
list of which opcodes the controller can send back. Most entries point at the
default case. The ones that do not include `0x07` for hardware identity and
`0x0C` for device colour, which independently confirms both request opcodes.

## The ring draws its own volume arc

Turning the dial on a stock unit grew an arc around the top ring. No animation
asset draws it: the lights directory has a cue for every other event and none
for volume. The donor sent the level to the microcontroller and the
microcontroller rendered the arc itself.

From `com.harman.volumeChanged` at `0xb4750`:

```text
b47a0:  cmp  r0, #100        validate 0 to 100
b47e8:  strb r4, [sp, #57]   byte 1 is the level
b47ec:  mov  r3, #3          opcode
b47f0:  strb r3, [sp, #56]
b47fc:  bl   7dbf0           six bytes to 0x36
```

Only bytes 0 and 1 are written; the donor left 2 to 5 as stack residue, so the
controller ignores them for this opcode. This runtime sends zeros there, which
makes the frame byte-identical in every field the controller reads.

Confirmed on hardware: four distinct arcs at 10, 100, 50 and 80.

Both devices share one I2C bus, so the arc is written after the DSP call it
accompanies returns rather than alongside it. Writing both at once produced
"lost arbitration" bursts that the retry loop absorbed silently; ordering them
took the same sweep from six arbitration losses to none.

## The front lamp is the network indicator

`audio-ui` drove it, and its own state strings give the mapping:

| state | front lamp |
| --- | --- |
| `online` | white solid |
| `wifi-setup` | amber fast-blink |
| `downloading` | white fast-blink |
| not online | amber solid |

All four combinations were confirmed by eye on this unit. Nothing in this
runtime drove the lamp before, so it sat at whatever the controller lit at
power-up, which reads as "online" whether or not the speaker has a network.

The `downloading` state is not implemented because this runtime fetches no
updates, so it could never occur.

## Vendor defaults

From `/caldata/FENV.bin`, the vendor's own settings store:

| id | name | value |
| --- | --- | --- |
| `0x73` | `LED_INTENSITY` | 50 |
| `0x77` | `LED_WHITE` | 50 |
| `0x72` | `LED_RGB` | `000000` |
| `0x74` | `LED_FLASHING` | `OFF` |

## How to extend this map

1. Disassemble with `arm-linux-gnueabihf-objdump -d`. Section `.rodata` has
   VMA = file offset + `0x10000` in this binary, which is how strings resolve.
2. Find the call sites of `0x7dbf0`.
3. For each, find the `add r1, sp, #N` or `mov r1, sp` that sets the frame
   pointer, then the `strb` into that offset, then the `mov` that loaded it.
4. Confirm the procedure by finding where its name string is registered: the
   handler address follows as a `movw`/`movt` pair into `r2`.

Check any new reading against opcode `0x09` first. Its layout is known
independently, because the lights physically respond to it, so a method that
cannot reproduce `0x09` is not to be trusted on anything else.

## Still unknown

`GetHWID` and `SetHWID`, and the upgrade path `requestmcuupgrade`,
`startmcuupgrade`, `sendfirmwaredata`, `mcuupgraderesult`. The upgrade family is
out of scope by decision, not by difficulty.
