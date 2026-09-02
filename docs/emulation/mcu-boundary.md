---
title: MCU boundary
description: Recovered I2C behavior and evidence limits at the Invoke companion-MCU boundary
ms.date: 2026-09-02
ms.topic: concept
---

What the companion-MCU boundary looks like from the preserved rootfs and from
running `mcu-interface` under emulation: the host-side transport, the observed
startup transaction order, and the limits of that evidence.

No physical unit was involved. No real I2C bus was accessed.

## Evidence classification

Verified facts:

* The held rootfs contains `usr/bin/mcu-interface` and
  `usr/share/mcu/cortana_mcu.bin`.
* `mcu-interface` opens `/dev/i2c-0`, uses raw `I2C_RDWR`, and refers to sysfs
  GPIO paths and `/dev/mem`.
* Under emulation, the service emits the bring-up log shown below and registers
  MCU-related WAMP procedures after startup.

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

* The trace is not a physical bus capture.
* Synthetic acknowledgements do not prove that the same values are accepted by
  the real devices.
* Register meanings, GPIO line numbers, and exact MCU/device identities remain
  unresolved.

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
| ioctl used | `I2C_RDWR` (`0x0707`) | Observed under emulation with syscall tracing |
| Interrupt path | sysfs GPIO `export`, `direction`, `edge`, `value` | Strings in `mcu-interface`, confirmed by runtime errors |
| Direct register access | `/dev/mem` | Runtime log, open attempt observed |

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

Captured by running `mcu-interface` under emulation and decoding the
`I2C_RDWR` request structures out of the emulator's memory. `qemu-user`
relocates the guest, so trace addresses were shifted by `guest_base`, derived
at runtime from where the guest ELF landed. The binary is `ET_EXEC` with its
first `LOAD` at `0x10000`, so the base is the mapped address minus `0x10000`.

Three slave addresses appear, each aligned to a distinct bring-up stage:

| Stage in service log | Slave | Direction | Data |
|---|---|---|---|
| `io_expander_initialize()` | `0x20` | write | `03 00` |
| mute amp and dac | `0x20` | write | `01` |
| mute amp and dac | `0x20` | read | `00` |
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

Whether the recovered register writes match real-device responses. Both the
failing-bus trace and an acknowledged synthetic-bus trace are captured. The
synthetic register file returns coherent values, but it does not model the
unknown physical parts.

The register maps behind each write. Register indices and values are recorded;
their meanings are not documented anywhere in the corpus.

The GPIO line numbers used for the MCU interrupt and handshake.

The exact MCU part number. Firmware size and command set are consistent with a
small microcontroller, but no identification is claimed.

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
