---
title: MCU boundary
description: Recovered I2C behavior and evidence limits at the Invoke companion-MCU boundary
ms.date: 2026-09-03
ms.topic: concept
---

What the companion-MCU boundary looks like from the preserved rootfs, emulation,
and a later RAM-native hardware run: the host-side transport, startup
transaction order, and limits of each evidence source.

## Evidence classification

Verified facts:

* The held rootfs contains `usr/bin/mcu-interface` and
  `usr/share/mcu/cortana_mcu.bin`.
* `mcu-interface` opens `/dev/i2c-0`, uses raw `I2C_RDWR`, and refers to sysfs
  GPIO paths and `/dev/mem`.
* Under emulation, the service emits the bring-up log shown below and registers
  MCU-related WAMP procedures after startup.
* On the physical unit, the replacement kernel registers `gpio_soc_0` at base 0.
  `mcu-interface` configures GPIO 3 as a falling-edge input and completes its
  MCU queries.
* An on-device log-and-forward capture recorded real `I2C_RDWR` responses:
  register `0x01` at address `0x20` returned `0xfb`, the donor completed its
  mute-first and DAC initialization, and `getmcustatus` returned `000116`.

Artifact-backed findings:

* The emulated startup path issues writes to I2C addresses `0x20`, `0x4c`, and
  `0x36`, aligned to the service log stages recorded below.
* The guest-side ioctl shim can acknowledge the raw `I2C_RDWR` calls without
  exposing a host bus, allowing the service to progress through startup.

Inference:

* The likely roles of `0x20`, `0x4c`, and `0x36` are inferred from address,
  access pattern, and timing. No device part number is established.
* The Linux bus-to-SoC base-address mapping is transferred from sibling
  Berlin-family source and has not been confirmed on Invoke hardware.

Current limits:

* The pass-through log is an on-device capture at the Linux ioctl boundary, not
  an electrical logic-analyzer capture.
* Register meanings and exact MCU/device identities remain unresolved.
* MCU framing after the DAC initialization remains only partly decoded.

## Complete known WAMP API

The physical RAM-native router trace proves that this build registered all 27
procedures below. The final `12.2134.0` binary contains those URIs and adds
`setFactoryResetMode`, bringing its string-level procedure-candidate vocabulary
to 28, but that additional registration has not been observed live.

| Procedures | Contract status |
|---|---|
| `com.harman.ledAnimate`, `ledSet`, `ledOff` | Registered; disassembly shows `ledAnimate` receives a pattern string and one byte, loads `/usr/share/lights/<pattern>.bin`, and sends animation chunks to MCU address `0x36`; `ledSet` and `ledOff` remain unresolved |
| `com.harman.vui.setDeviceColor`, `getDeviceColor`, `SetRGBLEDBrightness` | Registered; arguments and results unknown |
| `com.harman.vui.getmcustatus` | Registered; call is `[]`; verified live success is positional result `["000116"]` with no kwargs; returns `com.harman.error` with `["No MCU version :("]` when no MCU version is available |
| `com.harman.vui.setmcupowermode` | Registered; handler logs one string argument; accepted values and result unknown |
| `com.harman.vui.requestmcuupgrade` | Registered; handler log shows no argument; unsafe persistent operation, not called |
| `com.harman.vui.startmcuupgrade`, `sendfirmwaredata` | Registered; handlers log one string argument; encoding, chunking, checks, and results unknown; unsafe and not called |
| `com.harman.vui.powerdspcontrol` | Registered; handler logs one string argument; accepted values and result unknown |
| `com.harman.vui.mutedaccontrol`, `muteampcontrol` | Registered and live-tested with `["mute"]` and `["unmute"]`; successful result has no args or kwargs |
| `com.harman.vui.terminate`, `restart` | Registered; arguments, result, and scope unknown |
| `com.harman.vui.factorytestled`, `showUpgradeLed` | Registered; the latter logs one string argument; remaining contract unknown |
| `com.harman.vui.SetHWID`, `GetHWID`, `GetRecoveryFlag`, `ClearDemoMode`, `ChangeOTAFlag` | Registered; signatures unknown; setters may alter persistent state and were not called |
| `com.harman.test.stopTCLWTest`, `dacVolUp`, `dacVolDown`, `forceLibreFlash` | Registered test hooks; signatures unknown; not called |

The final-build-only string is
`com.harman.vui.setFactoryResetMode`. A URI string proves vocabulary, not that
the procedure is registered or safe.

The service subscribes to `com.harman.volumeChanged` and
`com.harman.test.simulateKeyAction`. Their inbound payload contracts remain
unknown. Its observed and candidate outbound topics are:

| Topic | Evidence and known payload |
|---|---|
| `com.harman.heartbeat.mcu-interface` | Observed repeatedly as no positional args plus `{"bootflag":""}` |
| `com.harman.test.inputEvent` | Observed rotary events as `["volumeup", "<step>"]` or `["volumedown", "<step>"]`; step was a decimal string from `1` through `5` |
| `com.harman.ready.mcu-interface` | Outbound URI string; message kind, payload, and live transmission not retained |
| `com.harman.vui.keypress` | Disassembly shows a one-string positional publication for non-rotary physical keys; names are listed below |
| `com.harman.vui.mcustatus` | Outbound URI string; message kind and payload unknown |
| `com.harman.vui.mcuupgraderesult` | Outbound URI string; message kind and payload unknown |

`com.harman.error` is also used as a WAMP error URI. No authentication or
authorization is visible in the observed MCU session.

### Recovered physical key map

The donor constructor builds two integer-keyed maps. The first maps raw MCU
event codes to log descriptions; the second maps the same codes to WAMP names.
The interrupt handler indexes both maps with byte 1 of an inbound `0x04` frame.

| Code | Physical event | Published name |
|------|----------------|----------------|
| `0x00` | Touch panel short press | `action` |
| `0x01` | Touch panel long press | `action-long` |
| `0x02` | Bluetooth short press | `bluetooth` |
| `0x03` | Bluetooth long press | `bluetooth-long` |
| `0x04` | Microphone short press | `micmute` |
| `0x05` | Microphone long press | `micmute-long` |
| `0x06` | Reset short press | `reset` |
| `0x07` | Reset long press | `reset-long` |
| `0x08` | Rotary clockwise | `volumeup` |
| `0x09` | Rotary counter-clockwise | `volumedown` |
| `0x0a` | Bluetooth plus microphone long press | Disabled in the shipped service |

Codes `0x08` and `0x09` retain the observed second positional step string.
Codes `0x00` through `0x07` publish their single canonical name on
`com.harman.vui.keypress`.

### Recovered LED animation transport

Each held `L_*.bin` animation is an exact multiple of 13 bytes, one intensity
byte for each top-panel LED per animation frame. The donor player reads at most
390 bytes (30 frames) per chunk and sends one I2C message to address `0x36`:

```text
0e <first-chunk-flag> <up to 390 animation bytes>
```

The first-chunk flag is `01` and subsequent chunks use `00`. The player waits
280 ms between chunks. This establishes the `ledAnimate` asset transport
without assigning semantics to the unresolved direct `ledSet` or `ledOff`
procedures.

## Physical RAM-native validation

A custom initramfs booted as PID 1 on `myInvoke-1` with the GCC 4.9
SPI-plus-GPIO kernel. It mounted a RAM copy of the installed
`Barracuda_libre-12.2050.3` SquashFS read-only and started Bonefish,
`mcu-interface`, and `dsp-client`.

The real adapter:

* Opened `/dev/i2c-0`
* Read IO-expander state `0xfb`
* Performed the mute-first amplifier and DAC sequence
* Powered the DSP and initialized the DAC
* Reported MCU application version `000116`
* Reported recovery flag `0`
* Configured GPIO 3 as a falling-edge input
* Joined realm `default`
* Registered LED, MCU status, power, DAC mute, and amplifier mute procedures
* Handled the DSP boot event's DAC and amplifier unmute requests

A later software-only pass-through capture verified the byte-level response
behind those service messages. Evidence is held at
`../reinvoke-archive/hardware/software-captures/20260903T191548Z-mcu-ioctl-record/`.
As elsewhere in the archive, `SHA256SUMS` records each artifact by repository-
relative archive path. In that manifest:

```text
daa68b35f3a3634e45e4073b71c2d075ac4fe2930afd8d68d11af32c1ec4b058  ../reinvoke-archive/hardware/software-captures/20260903T191548Z-mcu-ioctl-record/mcu-ioctl.log
1e9f0453ad7c6444a75f1246376270d6252fa2e8beb3ed810e268aaa1240de8e  ../reinvoke-archive/hardware/software-captures/20260903T191548Z-mcu-ioctl-record/getmcustatus.json
```

The recorder logged requests before invoking the real ioctl and read buffers
after successful return. The physical expander sequence began with `03 00`,
then five register-pointer/read pairs returned `fb`; each was followed by the
donor's `01 fb` write. The same run entered the established ten-write DAC
initialization at `0x4c`, while the service log recorded completion of that
stage and MCU application version `000116`. The retained ioctl log contains six
complete DAC records and the beginning of the seventh record before its final
line ends; the full ten-write byte sequence remains independently established
by the repeatable emulation captures below.

A bounded `strace` run captured:

```text
open("/sys/class/gpio/export", O_WRONLY)
open("/sys/class/gpio/gpio3/direction", O_WRONLY)
open("/sys/class/gpio/gpio3/edge", O_WRONLY)
open("/sys/class/gpio/gpio3/value", O_RDONLY|O_NONBLOCK)
```

The earlier recovery kernel assigned `gpio_soc_0` base `224`, whereas the normal
userspace expects GPIO `3`. The replacement DTB adds the Invoke driver's
required `base-gpio = <0>` property. The resulting kernel registers
`gpiochip0` at base 0, and the unmodified service successfully configures its
expected GPIO 3 path.

Physical rotation then produced 63 volume-up and 57 volume-down messages. Every
MCU message produced one matching Bonefish publication, for 120 paired
observations such as:

```text
MCU key event Volume up received!
com.harman.test.inputEvent ["volumeup", "2"]
```

This verifies the GPIO interrupt, MCU command decoding, rotary hardware, and
WAMP publication path. The event payload's second value varies with the
observed rotation step.

## What the MCU is

The preserved software treats the peer as a separate microcontroller from the
Marvell SoC, reached over I2C. The host side is `usr/bin/mcu-interface`, an ARM
executable that opens `/dev/i2c-0` and drives interrupt and handshake lines
through the sysfs GPIO interface.

Its firmware ships inside the rootfs at `usr/share/mcu/cortana_mcu.bin`, 13,312
bytes. The running service reports the version it expects:

```text
MCU firmware /usr/share/mcu/cortana_mcu.bin, version is 000116
```

The firmware image is byte-identical across all three known builds, including
Harman's final `Barracuda_libre-12.2134.0`. The MCU was never updated in the
field across the product's shipping life.

## Why it matters

The preserved software model places the MCU at the audio bring-up boundary. The
host-side service mutes and powers amplifier, DAC, and DSP stages before the
audio path is considered ready, so ALSA control alone is not treated as
sufficient by the stock software.

The recovered WAMP control surface reflects this. `com.harman.vui.muteampcontrol`,
`com.harman.vui.mutedaccontrol`, and `com.harman.vui.powerdspcontrol` are the
procedures that gate the speaker path, and they are served by `mcu-interface`.

For any revival that keeps the original board, the MCU is the component that
must be commanded correctly. It is more critical than the amplifier, which only
becomes the limiting factor if the stock electronics are bypassed entirely.

## Transport

| Property | Value | Source |
|---|---|---|
| Bus device | `/dev/i2c-0` | String and open call in `mcu-interface` |
| ioctl used | `I2C_RDWR` (`0x0707`) | Observed under emulation and in an on-device log-and-forward capture |
| Interrupt path | sysfs GPIO `export`, `direction`, `edge`, `value` | Strings in `mcu-interface`, confirmed by runtime errors |
| Direct register access | `/dev/mem` | Runtime log, open attempt observed |

The service is an unauthenticated WAMP client of Bonefish in realm `default`.
Bonefish listens on TCP 9999 for WAMP RawSocket and TCP 9998 for WAMP
WebSocket. RawSocket serializer negotiation accepts only MessagePack serializer
2 in the held build. The observed MCU session advertises caller, callee,
publisher, and subscriber roles. The WebSocket serializer and subprotocol have
not been tested.

At the hardware edge, every captured transfer uses `I2C_RDWR` (`0x0707`);
single-message writes return 1 and register-pointer/read pairs return 2. GPIO 3
is exported, configured as input with falling-edge trigger, and polled through
its nonblocking `value` file. The donor also reads and writes three SoC register
locations through `/dev/mem`: `0xF7EA8008`, `0xF7E80408`, and `0xF7E80404`.
The reason those direct writes are needed is not established.

The Linux bus number maps to the SoC's first DesignWare I2C master. The
cross-index of Berlin-family sources places `i2c-0` at `i2c0`, base address
`0xF7E81400`, described as `APB_I2C_INST0_BASE`. See
[05_SIBLING_SOURCE_CROSSINDEX.md](../corpus/05_SIBLING_SOURCE_CROSSINDEX.md).

That mapping is an inference transferred from a sibling platform. It has not
been confirmed on Invoke hardware.

## MCU command vocabulary

Recoverable strings from `cortana_mcu.bin` show a small command interpreter
with the prompt `cortana_mcu #`:

| Verb | Apparent purpose |
|---|---|
| `rgb r`, `rgb g`, `rgb b`, `rgb w` | LED colour channels |
| `led bt`, `led wf`, `led pw` | Bluetooth, Wi-Fi, and power indicators |
| `ver` | Report firmware version |
| `up app` | Enter application update mode |
| `flash_libre` | Flash operation, named for the `libre` build lineage |

The image also contains I2C status and error strings (`i2c_stat`, `i2c_t!`,
`i2c_r_c!`, `i2c_r_a!`, `i2c_err1`, `i2c_err2`), consistent with the MCU acting
as an I2C peripheral that reports transfer state.

The host carries an apparent firmware update path over WAMP:
`com.harman.vui.requestmcuupgrade`, `startmcuupgrade`, `sendfirmwaredata`, and
`mcuupgraderesult`.

## Observed bring-up sequence

Observed by running `mcu-interface` under emulation with a placeholder file
standing in for `/dev/i2c-0`. In this trace every transfer fails harmlessly, but
the service still announces each stage in order:

```text
mcu_interface start io_expander_initialize()
MCU init io expander. mute amp and dac!!!
Muting AMP -- Current Register val (before mute) = 0
Muting DAC -- Current Register val (before mute) = 0
mcu_interface start power_on_dsp()
mcu_interface start DAC_initialize()
mcu_interface start sleep 2S.
mcu_interface end sleep 2S.
```

Read plainly, the power-on order is:

1. Initialize an I2C IO expander.
2. Mute the amplifier and the DAC before anything else can make noise.
3. Read back the pre-mute register value, which the service logs.
4. Power on the DSP.
5. Initialize the DAC.
6. Wait two seconds for the analogue stage to settle.

After that the service starts an interrupt poll thread on a GPIO line, connects
to the WAMP router, and registers its procedures.

Inference: the mute-first ordering is consistent with pop suppression. Any
replacement control software should preserve the ordering unless physical
measurement proves another safe sequence.

The service also attempts to remove `/tmp/reg01.conf` through `/tmp/reg03.conf`
at startup, indicating three register configuration files are staged somewhere
in normal operation. Their contents are not present in the rootfs.

## Recovered I2C transactions

The complete transaction set was first captured by running `mcu-interface`
under emulation and decoding the `I2C_RDWR` request structures out of the
emulator's memory. `qemu-user` relocates the guest, so trace addresses were
shifted by `guest_base`, derived at runtime from where the guest ELF landed.
The binary is `ET_EXEC` with its first `LOAD` at `0x10000`, so the base is the
mapped address minus `0x10000`. The later physical pass-through capture
verified the real `0x20` response and the beginning of the same DAC sequence.

Three slave addresses appear, each aligned to a distinct bring-up stage:

| Stage in service log | Slave | Direction | Data |
|---|---|---|---|
| `io_expander_initialize()` | `0x20` | write | `03 00` |
| mute amp and dac | `0x20` | write | `01` |
| mute amp and dac | `0x20` | read | `fb` on the physical unit (`00` in the initial failing/synthetic trace) |
| `DAC_initialize()` | `0x4c` | write | `00 00` |
| `DAC_initialize()` | `0x4c` | write | `01 11` |
| `DAC_initialize()` | `0x4c` | write | `0d 10` |
| `DAC_initialize()` | `0x4c` | write | `25 08` |
| `DAC_initialize()` | `0x4c` | write | `41 04` |
| `DAC_initialize()` | `0x4c` | write | `41 07` |
| `DAC_initialize()` | `0x4c` | write | `08 3f` |
| `DAC_initialize()` | `0x4c` | write | `28 00` |
| `DAC_initialize()` | `0x4c` | write | `3d 30` |
| `DAC_initialize()` | `0x4c` | write | `3e 30` |
| after the two-second settle | `0x36` | write | `01 f7 7f 40 01 00` |
| after the two-second settle | `0x36` | write | `23 00 00 00 6c ba` |
| after the two-second settle | `0x36` | write | `25 f7 7f 40 01 00` |
| after the two-second settle | `0x36` | write | `26 00 00 00 00 00` |

Three independent runs produced the same transaction set. The `0x20` and
`0x36` payloads vary slightly between runs in the bytes that look like
pointers or counters, which is expected for values assembled at runtime.

## Reading the transactions

The following are interpretations. The table above is the evidence.

`0x20` is addressed during `io_expander_initialize()` and again while muting.
Its access pattern is a one-byte register pointer followed by a one-byte
read, and `0x20` is the base address of the common PCA9555 and TCA6416 style
I2C GPIO expanders. Taken together this supports reading `0x20` as the IO
expander, with the amplifier and DAC mute lines behind expander pins rather
than behind codec registers. The first write, `03 00`, addresses what would be
the configuration register for port 1 on that device family.

`0x4c` receives ten two-byte writes during `DAC_initialize()`, which is the
register-plus-value form used by most audio codecs. The registers touched are
`0x00`, `0x01`, `0x08`, `0x0d`, `0x25`, `0x28`, `0x3d`, `0x3e`, and `0x41`
twice with two different values, consistent with a staged power-up.

`0x36` is written only after the two-second settle, in six-byte frames whose
first byte varies while the remainder looks like a payload. That framing
differs from both the expander and the codec, and its timing places it after
the analogue path has stabilised.

No part numbers are claimed for any of the three. Address plus access pattern
narrows the device class; it does not identify a part.

## What is not established

The identity of the devices at `0x20`, `0x36`, and `0x4c`. Their access
patterns and bring-up positions narrow the device classes, but no part number
is claimed for any of them.

The register maps behind each write. Register indices and values are recorded;
their meanings are not documented anywhere in the corpus.

The handshake GPIO line and confirmation that recovery-kernel GPIO `227`
corresponds to the normal-kernel interrupt GPIO `3`.

The exact MCU part number. Firmware size and command set are consistent with a
small microcontroller, but no identification is claimed.

The six-byte MCU framing is only partly known. Physical logs identify opcodes
`0x01` (version exchange), `0x23`, `0x25`, `0x26` (recovery-flag exchange),
`0x24` (heartbeat), and inbound `04 08 <step> 00 00 00` /
`04 09 <step> 00 00 00` rotary events. Disassembly of
`mcu_heartbeat_timer_handler` proves that the donor initializes only byte zero
to `0x24`, sends all six stack bytes, and rearms the timer for 5,000
milliseconds. The varying trailing bytes are unspecified, not protocol fields.
The meanings of opcodes `0x23` and `0x25`, other field widths, checksums, and
retry behavior remain unresolved.

The exact IO-expander bit assignments are not proven. Synthetic emulation
observed register `0x01` progress through `0x00`, `0x02`, `0x03`, `0x13`, and
`0x1b`, but only live mute/unmute behavior establishes that some of those bits
gate the DAC and amplifier. The DSP power bit and safe reset/default behavior
still need an attributable trace.

## Safe software-only instrumentation

`tools/control/wamp-monitor.mjs` is an owned, dependency-free passive monitor.
It sends only WAMP `HELLO` and `SUBSCRIBE`, defaults to the six known MCU
topics, and emits newline-delimited JSON. It never sends `CALL` or `PUBLISH`
and never opens I2C, GPIO, `/dev/mem`, MTD, or an upgrade path.

With TCP 19999 forwarded to a RAM-booted unit's loopback port 9999:

```bash
node tools/control/wamp-monitor.mjs --duration 60
```

This is the smallest safe next component: it can refine event payloads while
changing only volatile Bonefish session state. It is not a hardware driver.

The remaining software-only probes, in safety order, are:

1. Run the passive monitor against the existing qemu sandbox and during a
   bounded RAM-native session; retain only newline-delimited event records.
2. In qemu with the existing synthetic I2C shim, call only `getmcustatus`,
   `getDeviceColor`, `GetHWID`, and `GetRecoveryFlag`, recording Bonefish
   `CALL`, `INVOCATION`, `YIELD`, and `ERROR` frames. Do not probe upgrade,
   flash, OTA, factory-reset, restart, terminate, or setter procedures.
3. Interpose the WAMP handler under qemu and log the MessagePack invocation
   object before each handler. This can recover type checks and error results
   without guessing arguments or touching hardware.
The physical-RAM `LD_PRELOAD` pass-through probe has now been completed. It
forwarded the donor's normal ioctls unchanged and recorded the real response
buffers without synthesizing data. The remaining probes above require no
provisioning-file or NAND change.

## Owned replacement validation

The static reInvoke MCU service was run directly from the RAM platform. It
reproduced the captured expander and DAC initialization with both outputs
muted, exposed the minimum WAMP status and mute procedures, and rejected an
unmute request under its default policy.

The first build omitted opcode `0x24`; the SoC later reset into the Marvell USB
stage. The corrected service sends `24 00 00 00 00 00` immediately and every
five seconds. It remained connected and published these physical ring events:

```text
com.harman.test.inputEvent ["volumeup", "1"]
com.harman.test.inputEvent ["volumedown", "2"]
com.harman.test.inputEvent ["volumedown", "1"]
```

This validates the closed-unit software replacement path for MCU startup,
mute safety, liveness, status, and rotary input. Artifact and test provenance
is recorded in [P1-048](../../metadata/P1-048.json).

## Refining this further

An earlier version of this document suggested loading the kernel's `i2c-stub`
module to provide a bus that acknowledges transfers. That was tried and does
not work, for a reason worth recording so it is not attempted again.

`i2c-stub` implements only the SMBus protocol. Querying its capabilities with
`I2C_FUNCS` returns `0x0c7f0000`, which sets the `I2C_FUNC_SMBUS_*` bits but
not `I2C_FUNC_I2C`. Since `mcu-interface` issues raw `I2C_RDWR` transfers, the
stub rejects every one of them and the service behaves exactly as it does
against a placeholder file.

The viable emulation route was an `LD_PRELOAD` shim built for ARM that intercepts
`ioctl()` inside the guest, answers `I2C_RDWR` with synthetic responses, and
logs the exchange. It is implemented and reproduced for the emulated startup
path. All 30 bring-up messages return success without binding any real I2C
device into the sandbox.

The acknowledged trace reproduces the startup writes from the failing-bus
capture and adds synthetic coherent reads. Register `0x01` at address `0x20`
progresses through `0x00`, `0x02`, `0x03`, and `0x13` as the service sets mute
and power-control bits. The DAC still receives ten writes at `0x4c`, followed
by four six-byte messages at `0x36`.

The full acknowledged synthetic expander sequence is:

```text
write 03 00
write 01; read 00; write 01 02
write 01; read 02; write 01 02
write 01; read 02; write 01 03
write 01; read 03; write 01 13
write 01; read 13; write 01 1b
```

This establishes the donor's read-modify-write order. It does not establish the
reset value or physical meaning of each bit.

The shim was compiled with Ubuntu's `arm-linux-gnueabihf-gcc`. An initial
implementation used `dlsym` and accidentally required `GLIBC_2.34`. Replacing
that forwarding path with `syscall(SYS_ioctl, ...)` reduced the requirement to
`GLIBC_2.4`, compatible with the firmware's glibc 2.23. Installing or upgrading
glibc is neither needed nor recommended.

A warning that applies to any such work. The host used here has real
`/dev/i2c-*` devices belonging to its own SMBus and GPU. The failed kernel-stub
experiment exposed only `/dev/i2c-12`, scoped to a group rather than made
world-writable. The successful shim uses `/dev/null` only as an openable
placeholder; every I2C ioctl is handled inside the ARM guest. Never bind a real
bus into this sandbox. Firmware that expects to write amplifier and DAC
registers would be writing to unrelated hardware.

## Reproducing the capture

The failing-bus capture script lives outside the repository at
`reinvoke-archive/emulation/mcu-i2c-capture.py`, alongside the sandbox it
drives. It spawns the sandbox itself, because `ptrace_scope` restricts memory
reads to descendant processes.

```bash
python3 reinvoke-archive/emulation/mcu-i2c-capture.py
```

The acknowledged-bus shim source and launcher are versioned under
`tools/emulation/`. Build the ARM library into the sibling archive, then run
the MCU service:

```bash
arm-linux-gnueabihf-gcc -shared -fPIC -O2 -Wall -Wextra -Werror \
  -o ../reinvoke-archive/emulation/invoke-ioctl-shim.so \
  tools/emulation/invoke-ioctl-shim.c

unshare --user --map-root-user --net -- \
  bash -c 'ip link set lo up && tools/emulation/run-final-shim.sh \
    /usr/bin/mcu-interface 127.0.0.1 9999'
```
