# MCU Boundary

What the companion microcontroller is, how the main SoC talks to it, and the
order in which it brings the audio hardware up. Derived from static analysis of
the preserved rootfs and from running `mcu-interface` under emulation.

No physical unit was involved. No real I2C bus was accessed.

## What the MCU is

A separate microcontroller from the Marvell SoC, reached over I2C. The host
side is `usr/bin/mcu-interface`, an ARM executable that opens `/dev/i2c-0` and
drives interrupt and handshake lines through the sysfs GPIO interface.

Its firmware ships inside the rootfs at `usr/share/mcu/cortana_mcu.bin`, 13,312
bytes. The running service reports the version it expects:

```text
MCU firmware /usr/share/mcu/cortana_mcu.bin, version is 000116
```

The firmware image is byte-identical across all three known builds, including
Harman's final `Barracuda_libre-12.2134.0`. The MCU was never updated in the
field across the product's shipping life.

## Why it matters

The MCU gates the audio path. The host cannot produce sound by writing to ALSA
alone, because the amplifier, DAC, and DSP are held in a muted or unpowered
state until the MCU is commanded otherwise.

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

The host carries a matching firmware update path over WAMP:
`com.harman.vui.requestmcuupgrade`, `startmcuupgrade`, `sendfirmwaredata`, and
`mcuupgraderesult`.

## Observed bring-up sequence

Captured by running `mcu-interface` under emulation with a placeholder file
standing in for `/dev/i2c-0`. Every transfer fails harmlessly, but the service
still announces each stage in order:

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

The mute-first ordering is a deliberate pop-suppression design. Any replacement
control software should preserve it.

The service also attempts to remove `/tmp/reg01.conf` through `/tmp/reg03.conf`
at startup, indicating three register configuration files are staged somewhere
in normal operation. Their contents are not present in the rootfs.

## What is not established

The I2C slave addresses of the IO expander, amplifier, DAC, DSP, and the MCU
itself. These are not recoverable from strings and were not captured.

The register maps behind each bring-up stage. The sequence order is known; the
specific register writes are not.

The GPIO line numbers used for the MCU interrupt and handshake.

The exact MCU part number. Firmware size and command set are consistent with a
small microcontroller, but no identification is claimed.

## Recovering the register traffic

Two viable approaches, neither yet completed.

Load the kernel's `i2c-stub` module to create a harmless virtual I2C bus, bind
it into the sandbox as `/dev/i2c-0`, and let `mcu-interface` transact against it
normally. Transfers would then succeed and the register state can be read back.
This requires root to load the module.

Alternatively, resolve the qemu guest base address and decode the `I2C_RDWR`
request structures directly out of the emulator's memory. A first attempt read
`/proc/<pid>/mem` at raw guest addresses and recovered nothing, because guest
addresses require the base offset applied before they are valid host addresses.

A warning for either approach. The host used for this work has real
`/dev/i2c-*` devices belonging to its own SMBus and GPU. The sandbox must
continue to shim that path. Exposing the host's real I2C bus to firmware that
expects to write amplifier and DAC registers risks writing to unrelated
hardware.
