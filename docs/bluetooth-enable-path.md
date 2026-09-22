---
title: Bluetooth enable path
description: What gates the donor radio, what is solved, and what still blocks it
---

Recovered by disassembling the donor `bluetooth` binary and confirmed on the
unit. Every claim here is either read out of machine code or observed live;
nothing is inferred from documentation.

## The gate: an out-of-box-experience flag

`BtSystemControl::wamp_on_pair` logs its entry and then returns immediately
unless one object byte is set:

```asm
ldrb r3, [r4, #149]   ; byte 0x95
cmp  r3, #0
bne  <continue pairing>
pop  {..., pc}        ; otherwise do nothing
```

That byte is written only by `handle_oobe_state_change`, whose log strings are
`OOBE is finished, initializing...`, `system map doesn't exist` and
`system map doesn't contain state param`, and which compares values against
`"system"` and `"normal"`.

This matches the observed symptom exactly: a Bluetooth long press logged
`wamp_on_pair` and nothing further, and the radio never transmitted.

## What opens it

The donor **calls** `com.harman.stateGet` during startup and gates on the
reply. The working exchange, captured live:

```text
call [15] com.harman.stateGet
yield [63, {}, [], {"system":{"state":"normal"}}]
system state is normal
BtSystemControl::handle_oobe_state_change
OOBE is finished, initializing...
```

Two details matter and were both learned the hard way:

* It is a **procedure the donor calls**, not an event to publish. Publishing
  the same payload on `com.harman.stateChanged` reaches
  `wamp_on_state_change` but never satisfies the gate.
* The payload travels as **kwargs**, with empty positional args.

`reinvoke-identifiers` registers the procedure and returns that payload.

## The second gate: HAL module resolution

`libhardware.so` resolves HAL modules through hardcoded absolute paths,
`/system/lib/hw` and `/vendor/lib/hw`. The donor tarball is root-relative, but
earlier candidates extracted it into `/opt/bluedroid`, so `hw_get_module`
failed:

```text
ERROR: failed to load BT HAL module!
```

Installing everything at `/` is not the alternative: the donor ships its own
`libc.so.6`, `liblog.so`, `libglibc_bridge.so` and six more that collide with
the runtime's glibc. The builder therefore splits the install:

| Donor content | Collides | Destination |
| ------------- | -------- | ----------- |
| `system/lib/*` (13 libraries plus `hw/`) | No | `/system/lib` — required, hardcoded |
| `lib/*` (donor glibc) | **Yes** | private, reached via `--library-path` |
| `usr/bin`, `usr/lib`, `etc` | No | private |

With the HAL reachable, the stack proceeds into real Bluedroid calls:
`enable(true)`, `set_adapter_property` with class of device `0x00600414`, and
`tuned on bt interface`.

## What still blocks it

Nothing does. This section records the blocker as it stood and how it was
cleared, because the symptom is easy to mistake for a live fault: while the
stack is off, `hci0` still reads an all-zero address, which looks identical to
the failure below.

The radio was dark even with both gates cleared:

```text
address       = 00:00:00:00:00:00
hci_version   = 0
manufacturer  = 0
features      = 0x0000000000000000
```

All zeros means the controller never completed HCI initialisation, so
`enable(true)` has nothing to enable and no adapter-state callback arrives.

Measured:

* `bt8xxx` loads and reports `Driver loaded successfully`.
* rfkill is not blocking: `hci0 type=bluetooth soft=0 hard=0`.
* A purpose-built `HCIDEVUP` ioctl opens an `AF_BLUETOOTH` socket but fails
  with **`no such device`**, so the kernel BT core has no registered
  controller despite the `hci0` sysfs node existing.
* On module reload the driver logs `Delete hci0` but never `Create hci0`, and
  creates only `mfmchar0` and `mnfcchar0`.
* `drv_mode` (bit 0 BT, bit 1 FM, bit 2 NFC) changed nothing at `1` or `7`.
* `fw=1` did not force a re-download: the driver reports
  `BT FW is active(0)` and `FW already downloaded!`.

Reading: the SD8887 combo firmware is resident and serving FM and NFC, but its
Bluetooth function is not attached, and a warm module reload cannot re-attach
it because the driver declines to re-download active firmware. The surviving
`hci0` node is a leftover from the original cold boot.

## How it was cleared

The cold-boot measurement was taken, and the answer was the first branch:
something in the running runtime was tearing the controller down. Three
packaging defects, each found by reading a core dump rather than by inference:

* BlueZ opened the adapter and this runtime then removed it, so `hci0` sat
  DOWN at version 0 and the donor stack dereferenced state it never
  populated. The controller is now handed to the donor stack rather than
  opened first.
* `bt_stack.conf` shipped as `etc/bluetooth_orig` to dodge a BlueZ path
  collision, so the donor never found it. It is installed at `/etc/bluetooth`,
  the path compiled into the donor binary.
* The state directory was `/data/bluetooth`, which nothing reads. The donor
  uses `/data/misc/bluedroid`.

Measured after the fix: `hci_version` 8 and `manufacturer` 72, where both had
read zero. The radio and the driver were never at fault.

`/run/reinvoke/bluetooth-state` reports whether the stack is currently on. An
all-zero `hci0` address while that file reads `off` is the stack being
disabled, not a return of the fault above.
