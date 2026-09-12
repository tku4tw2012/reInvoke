---
title: MCU boundary
description: Owned controls and privacy policy, recovered I2C frames and donor API reference
ms.date: 2026-09-12
ms.topic: reference
---

`reinvoke-mcu-interface` owns MCU I2C, physical input, rotary volume,
indicators, amplifier/DAC power and mute policy, and the compatibility
microphone API. It replaces Harman's `mcu-interface`; the
[current contract](../current-product-contract.md) is normative.

The wire and restart observations below are historical RAM tests, not native
introspection. Candidate 02 independently reported MCU `000116` and
demonstrated rotary, indicator and provisioning controls. See the
[native evidence](../native-nand-platform.md) for candidate-specific acceptance.

## Current owned boundary

Both physical `micmute` and WAMP `com.harman.dsp.micMute` enter one
process-lifetime privacy controller before publication. It atomically retains
mode-`0600` RAM state, updates the protected red ring only after confirmed DSP
state, retries failed mute reconciliation without Bonefish, and fails safe
after indeterminate unmute. Raw DSP opcode `0x09` is reachable only through
the DSP-owned mode-`0600` `/run/reinvoke/dsp-mic-control.sock`.

| Physical input                     | Local action                                                |
| ---------------------------------- | ----------------------------------------------------------- |
| Rotary clockwise/counter-clockwise | Coalesced BlueALSA volume, then compatibility publication   |
| Mic-Mute short                     | Toggle DSP privacy and confirmed red indication             |
| Bluetooth short                    | Open bounded pairing window when idle; cancel when active   |
| Bluetooth long                     | Reopen bounded allowlisted pairing window                   |
| Action short                       | Toggle Bluetooth play/pause and reviewed one-shot animation |
| Mic-Mute long                      | Request provisioning window in STA/uAP mode                 |
| Action long, reset short/long      | Compatibility publication only; product actions incomplete  |

Some physical Mic-Mute attempts produced no MCU frame under both donor and
owned services. Software can handle received events, not manufacture missing
ones. The privacy boundary is software-enforced, not an established electrical
microphone disconnect.

Speaker unmute separately requires the active-PCM lease, ALSA owner thread,
packaged player executable and `RUNNING` state to agree. See
[speaker safety](owned-speaker-control.md#pcm-and-speaker-safety);
volume state and DSP boot do not authorize physical unmute.

## Transport and ownership

| Interface          | Contract                                                             |
| ------------------ | -------------------------------------------------------------------- |
| I2C                | `/dev/i2c-0`, raw `I2C_RDWR` (`0x0707`)                              |
| Completion         | Single-message write returns 1; register-pointer/read pair returns 2 |
| Interrupt          | GPIO 3, input, falling edge, nonblocking sysfs `value` polling       |
| Shared expander    | Address `0x20`, output register `0x01`                               |
| MCU frames/LEDs    | Address `0x36`                                                       |
| DAC initialization | Address `0x4c`                                                       |

The Invoke GPIO driver requires `base-gpio = <0>` for these userspace numbers.
The donor also accesses `/dev/mem` registers `0xF7EA8008`, `0xF7E80408` and
`0xF7E80404`; their complete MCU-side purpose is unresolved. Sibling-source
mapping of `i2c-0` to `APB_I2C_INST0_BASE` at `0xF7E81400` remains inference,
not an Invoke measurement.

The owned controller uses expander register `0x01` masks `0x02` for amplifier
mute and `0x04` for DAC mute, with opposite assertion polarities; power masks
are `0x10` and `0x08`. It preserves DSP reset bit `0x01`. MCU and DSP services
serialize read-modify-write with `/run/reinvoke/expander.lock`; preserving bits
alone would not prevent cross-process races. Register `0x03` is treated as
direction/configuration. No expander, MCU or DAC part number is established.
See [controller.go](../../tools/mcu-interface/controller.go).

## Bring-up transactions

The donor order is expander initialization -> mute amplifier/DAC -> DSP power
-> DAC initialization -> two-second settle -> MCU queries/interrupt polling
-> WAMP registration. The owned service preserves mute-first sequencing.

The complete startup writes were recovered under emulation. A physical
log-and-forward capture confirmed expander response `fb`, service completion
and the beginning of the same DAC sequence; its final line truncates during
the seventh DAC record, so the full ten-write list is not a complete physical
capture.

| Stage                  | Address | Bytes                                                                                    |
| ---------------------- | ------- | ---------------------------------------------------------------------------------------- |
| Expander configuration | `0x20`  | Write `03 00`                                                                            |
| Output read            | `0x20`  | Write pointer `01`, read one byte (`fb` physically)                                      |
| DAC writes, in order   | `0x4c`  | `00 00`, `01 11`, `0d 10`, `25 08`, `41 04`, `41 07`, `08 3f`, `28 00`, `3d 30`, `3e 30` |
| Post-settle query      | `0x36`  | `01 f7 7f 40 01 00`                                                                      |
| Post-settle command    | `0x36`  | `23 00 00 00 6c ba`                                                                      |
| Post-settle command    | `0x36`  | `25 f7 7f 40 01 00`                                                                      |
| Recovery-flag query    | `0x36`  | `26 00 00 00 00 00`                                                                      |

The `0x36` trailing bytes above are captured examples, not all defined fields.
They vary between donor runs. Synthetic expander reads produced this
read-modify-write sequence:

```text
write 03 00
write 01; read 00; write 01 02
write 01; read 02; write 01 02
write 01; read 02; write 01 03
write 01; read 03; write 01 13
write 01; read 13; write 01 1b
```

That establishes donor order, not physical reset values or bit assignments.
The physical capture instead began with repeated `fb` reads and `01 fb`
writes. Exact cold defaults and complete register maps remain unknown.

## MCU framing and liveness

Known six-byte frame opcodes are `0x01` (version), `0x26` (recovery flag),
`0x24` (heartbeat) and inbound `0x04` (key event). Meanings of `0x23` and
`0x25`, remaining fields, checksums and retries are unresolved.

The donor heartbeat initializes only byte 0 to `0x24`, sends six stack bytes
and rearms for 5,000 ms. The owned service instead sends
`24 00 00 00 00 00` immediately and every five seconds. Omitting the heartbeat
in the first owned RAM build was followed by reset into the Marvell USB stage;
later tests with the heartbeat retained service liveness.

The held `usr/share/mcu/cortana_mcu.bin` is 13,312 bytes and byte-identical
across three known builds, including final `12.2134.0`. Expected and physically
reported application version is `000116`; the donor reported recovery flag
`0`. This does not prove no field update ever occurred.

The image contains interpreter prompt `cortana_mcu #`, verbs `rgb r/g/b/w`,
`led bt/wf/pw`, `ver`, `up app` and `flash_libre`, plus I2C status/error strings.
These are vocabulary evidence, not a supported console or update recipe.

## Physical key map

The donor indexes the maps with byte 1 of inbound `0x04` frames.
Codes `0x00`-`0x07` publish one string on `com.harman.vui.keypress`;
rotary codes publish on `com.harman.test.inputEvent`.

| Code | Physical event                 | Published name    |
| ---- | ------------------------------ | ----------------- |
| `00` | Touch panel short              | `action`          |
| `01` | Touch panel long               | `action-long`     |
| `02` | Bluetooth short                | `bluetooth`       |
| `03` | Bluetooth long                 | `bluetooth-long`  |
| `04` | Microphone short               | `micmute`         |
| `05` | Microphone long                | `micmute-long`    |
| `06` | Reset short                    | `reset`           |
| `07` | Reset long                     | `reset-long`      |
| `08` | Rotary clockwise               | `volumeup`        |
| `09` | Rotary counter-clockwise       | `volumedown`      |
| `0a` | Bluetooth plus microphone long | Disabled in donor |

Rotary frames are `04 08 <step> 00 00 00` and `04 09 <step> 00 00 00`;
publications are `["volumeup","<step>"]` or `["volumedown","<step>"]`.
Observed steps are decimal strings `1`-`5`. A physical RAM trace matched all
120 MCU rotary events to router publications, validating interrupt, decoding
and publication, not a universal acceleration curve.

## Recovered LED transport

`L_*.bin` assets contain 13 intensity bytes per top-panel frame. `ledAnimate`
accepts a pattern string and one byte; the donor sends at most 390 asset
bytes (30 frames) per I2C message to `0x36`, waiting 280 ms between chunks:

```text
0e <first-chunk-flag> <up to 390 animation bytes>
```

The first chunk uses `01`, later chunks `00`. Owned `ledOff` cancels ordinary
animation and sends 41 bytes: `0e 01` followed by three zero 13-byte frames.
RAM testing physically confirmed clearing after microphone unmute.
Generic animations and `ledOff` are rejected while privacy requires the red
indication.

### Recovered ledSet contract

The small front Wi-Fi diffuser and rear Bluetooth indicator use:

```text
com.harman.ledSet(<target>, mode=<state>, color=<colour>)
```

Only the required first positional argument is read; extra positional and
unknown keyword arguments are ignored. Missing/empty mode means `off`.

| Mode         | Value |
| ------------ | ----- |
| `off`        | `00`  |
| `on`         | `01`  |
| `dim`        | `02`  |
| `slow-blink` | `03`  |
| `fast-blink` | `04`  |

Three process-local states are retained: front amber, front white and back.
`front` with `white` clears amber; `front` with `amber` clears white.
`back` ignores color. Unknown targets/front colors send unchanged state;
unknown mode on a valid channel resolves to off. `wifi`, `bluetooth`, `green`,
`blue` and `black` are not `ledSet` values.

The owned six-byte write is:

```text
09 <front-amber> <front-white> <back> 00 00
```

The donor's last two bytes are indeterminate; the owned service zeroes them,
serializes selection/write and commits state only after success.
`com.harman.vui.SetRGBLEDBrightness` is separate and rejects values outside
0-100. Static recovery and host tests establish these contracts; candidate
02's indicator evidence is not exhaustive physical validation of every value.

### Bluetooth indicator policy

Donor `audio-ui`, not `bluetooth`, calls rear `ledSet`: pairing -> slow blink,
connected -> on, otherwise -> empty mode/off. Pairing state has precedence.
Its short-button policy calls `bluetoothPairing(true)` from idle and
`bluetoothPairing(false)` while pairing. The donor's physical-key topic
bridge is unresolved: MCU publishes `vui.keypress`, but `audio-ui` subscribes
to `test.inputEvent`, and no local binary references both.

The replacement directly signals the generation-checked pairing agent:
`SIGUSR2` toggles/cancels; `SIGUSR1` reopens. Signal handlers only set flags;
adapter changes run in the bounded D-Bus loop. The agent follows the
allowlisted `Device1.Connected` property and exact-path change signals.
It writes `pairing`, `connected` or `off` atomically with mode `0600` to
`/run/reinvoke/bluetooth-state`, with pairing precedence.

Shutdown, Agent1 release, D-Bus loss and generation replacement attempt `off`;
the generation guard removes stale state after process loss. MCU polling is
250 ms, limited to 32 bytes. Missing/oversized/invalid state maps to off.
Confirmed unchanged state avoids repeat writes; failures remain retryable.
The serialized watcher corrects a WAMP rear write on the next poll without
using top-ring animation or bypassing privacy.

## Historical donor WAMP API

The physical RAM trace registered 27 procedures. Names below use prefix
`com.harman.`; braces group exact suffixes, not wildcard registrations.

| Procedures                                                          | Recovered contract                                                                                    |
| ------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `ledAnimate`, `ledSet`, `ledOff`                                    | Transport above                                                                                       |
| `vui.{setDeviceColor,getDeviceColor,SetRGBLEDBrightness}`           | Color signatures unknown; brightness range above                                                      |
| `vui.getmcustatus`                                                  | `[]` -> `["000116"]`, no kwargs; unavailable version returns `com.harman.error ["No MCU version :("]` |
| `vui.{setmcupowermode,powerdspcontrol}`                             | One string logged; accepted values/results unknown                                                    |
| `vui.{mutedaccontrol,muteampcontrol}`                               | `["mute"]`/`["unmute"]`; empty successful result                                                      |
| `vui.requestmcuupgrade`                                             | No argument logged; persistent operation, not called                                                  |
| `vui.{startmcuupgrade,sendfirmwaredata}`                            | One string logged; encoding/checks/results unknown, not called                                        |
| `vui.{terminate,restart,factorytestled}`                            | Signatures/scope unknown                                                                              |
| `vui.showUpgradeLed`                                                | One string logged; remaining contract unknown                                                         |
| `vui.{SetHWID,GetHWID,GetRecoveryFlag,ClearDemoMode,ChangeOTAFlag}` | Signatures unknown; persistent setters not called                                                     |
| `test.{stopTCLWTest,dacVolUp,dacVolDown,forceLibreFlash}`           | Test hooks, not called                                                                                |

Final `12.2134.0` adds string `com.harman.vui.setFactoryResetMode` (28
candidates), without an observed registration. No donor session authentication
was visible; registration does not establish safe invocation.

Subscriptions are `com.harman.volumeChanged` and
`com.harman.test.simulateKeyAction`, with unrecovered inbound contracts.
Observed `com.harman.heartbeat.mcu-interface` has no args and
`{"bootflag":""}` kwargs. `com.harman.ready.mcu-interface`,
`com.harman.vui.mcustatus` and `com.harman.vui.mcuupgraderesult` are outbound
URI strings whose live payloads were not retained.

## Instrumentation and limits

The [passive monitor](../../tools/control/README.md#query-and-monitor-wamp)
sends only HELLO/SUBSCRIBE. A missing publication does not prove the MCU sent
no frame. Physical log-and-forward capture instead forwards real startup
operations and adds timing overhead; it is not a read-only hardware action
or electrical bus capture.

For off-device work use the
[isolated donor sandbox](control-plane-emulation.md#isolated-reproduction).
`i2c-stub` supports SMBus, not `I2C_FUNC_I2C`, so it cannot satisfy raw
`I2C_RDWR`. The ARM preload shim supplies synthetic responses without exposing
a real host bus. Its successful startup does not measure physical behavior.

Owned RAM tests cover mute-first startup, heartbeat, rotary, pairing, LEDs,
speaker authorization and microphone privacy across router/DSP restarts;
provenance begins at [P1-048](../../metadata/P1-048.json). MCU identity,
undecoded frame fields, complete register maps, cold-state behavior and missing
physical key events remain unresolved.
