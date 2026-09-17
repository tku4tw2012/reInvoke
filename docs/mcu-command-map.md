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
