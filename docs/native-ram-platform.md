---
title: Native RAM platform
description: Working RAM inputs, hardware dependencies, accepted measurements, and replacement-runtime boundaries
ms.date: 2026-09-12
ms.topic: reference
---

## Execution boundary

Native RAM means ARM Linux and owned userspace executing on the Invoke after
a host loads the kernel and initramfs into DRAM. It is not autonomous NAND
startup. September 2026 RAM validation established acoustic playback, rotary
volume, microphone capture/privacy, provisioning and supervised services.
These results do not transfer automatically to an installed native candidate.

The [product contract](current-product-contract.md) defines runtime behavior;
the [native NAND platform](native-nand-platform.md) records installed pins and
acceptance. The assistant integration and persistent configuration remain
[unfinished work](revival-roadmap.md#remaining-work).

## Working input contract

The public tree contains builders and owned service source, not release firmware
or all donor inputs. Deterministic composition from pinned held artifacts is
narrower than a complete firmware build from a clean public clone.

| Input                         | Required property                                                |
| ----------------------------- | ---------------------------------------------------------------- |
| Invoke GPL kernel source      | Berlin 3.8.13 tree and Invoke board configuration                |
| Compiler                      | Verified Android NDK GCC 4.9 for hardware kernels and modules    |
| Device tree                   | Checksum-gated, proven load layout with SPI/GPIO/audio additions |
| Kernel modules                | Matching release and ABI, built with `-fno-pic -fno-pie`         |
| Recovery initramfs            | Held source for the sanitized RAM startup base                   |
| Donor rootfs                  | Extracted, identified filesystem for libraries and board assets  |
| Firmware and calibration      | SD8887 WLAN/BT firmware, board calibration and DSP loader        |
| Owned userspace               | PID 1, hardware services, networking and Bluetooth replacements  |

The known recovery pairing is the MTD-cleanup-corrected kernel with unchanged
RC12 initramfs. [Recovery access](uboot-access.md#known-ram-recovery-pair) retains
its pins and handoff. The unchanged release string
`3.8.13-reinvoke-audio-sd8887` is insufficient to distinguish the corrected
kernel: its build version contains `#1-mtd-cleanup`.

The established kernel builder accepts a pinned DTB:

```bash
tools/kernel/build-native-kernel.sh \
  --profile spi-gpio \
  --dtb <archive>/build/artifacts/reinvoke-spi-gpio.dtb \
  --dtb-sha256 <reviewed-dtb-sha256> \
  --output-dir <archive>/build/artifacts/invoke-native-spi-gpio
```

That SPI/GPIO profile is a bring-up increment, not the complete audio profile.
Use the [kernel build reference](../tools/kernel/README.md) for the selected
profile and cleanup fix. Do not mix its modules with another kernel.
The initramfs builder's donor-input interface is:

```bash
tools/usb-boot/build-native-initramfs.sh \
  --source-initramfs <archive>/extracted/ota2/OTA2/82_IMAGE \
  --donor-rootfs <archive>/hardware/dumps/<snapshot>/rootfs-extracted/primary \
  --kernel-modules <matching-kernel-modules> \
  --output <boot-dir>/82_IMAGE.native-ram
```

This interface describes the donor-assisted base, not a one-command build of
the latest owned runtime. [USB tools](../tools/usb-boot/README.md) owns staging
and loader usage. The generated initramfs size must be reflected in bootargs.

The RAM builder replaces PID 1 and removes vendor `flash_custk` and
`/home/galois/run.sh`. NAND stays unmounted, but inherited BusyBox still has
low-level applets. RAM loading is not a privilege boundary against an operator
issuing a storage command.

## Kernel bring-up constraints

GCC 11.4 and 9.5 builds compiled after compatibility work but did not return
the expected USB gadget. No trace identified their failure stage. NDK GCC 4.9
with pristine source and the proven recovery layout did return USB; subsequent
peripheral additions retained that baseline.

* The recovery layout uses load/entry `0x02008000`; the initial ACast-layout
  replacement at `0x01108000` did not meet the USB-return criterion.
* Android GCC's default module output referenced `_GLOBAL_OFFSET_TABLE_`.
  Rebuilding with `-fno-pic -fno-pie` let the SD8887 modules load.
* DesignWare SPI at `0xF7E81C00` with a 1 MHz `spidev0.0` child exposed transport.
  Successful SPI transfers alone did not produce a DSP response.
* The retained `gpio-dwapb` driver requires `base-gpio`. Adding
  `base-gpio = <0>` to the first bank registered GPIOs 0-31 and enabled
  bidirectional MCU/DSP startup.
* The Invoke ASoC machine path supplied audio. The older direct-card initializer
  is deliberately bypassed in the Invoke source, unlike the sibling driver.
* The RAM kernel lacks working `kexec`: `sys_kexec_load` aliases
  `sys_ni_syscall`. Load a replacement kernel through the verified U-Boot path.

Modern-compiler success on the host therefore does not qualify a device kernel.
Likewise, acceptance of a standard RAM `uImage` does not establish acceptance
of arbitrary replacement kernels by the persistent signed/encrypted boot path.

## Board assets and persistence

The observed unit has 512 MiB DRAM and 256 MiB NAND. The early captured active
rootfs was `Barracuda_libre-12.2050.3`, not the separately acquired final
`12.2134.0`. Retained strings elsewhere in NAND do not identify active firmware.
See [firmware generations](firmware-reference.md#firmware-generations).

| Component             | Asset or interface                         | Persistence boundary                         |
| --------------------- | ------------------------------------------ | -------------------------------------------- |
| Wi-Fi                 | `sd8887_wlan_a2_p78.bin`, SDIO `02df:9135` | Downloaded at module load                    |
| Board calibration     | `WlanCalData_ext-LS9AD-20160725.conf`      | Held donor input                             |
| Bluetooth             | `sd8887_bt_a2_new.bin`, native `bt8xxx.ko` | Downloaded before `hci0` becomes usable      |
| MCU                   | I2C protocol and existing application      | Upgrade surface excluded                     |
| DSP                   | `dsp-img.ldr`, 160,484 bytes               | Reloaded through SPI at each service start   |
| Credentials and bonds | RAM-backed runtime paths                   | Lost with power                              |
| SPI NOR               | 16 MiB M25P128                             | Sampled only, not proved unused or backed up |

A byte-exact September 3 trace recorded 40,121 four-byte transfers matching
every bit-reversed DSP loader byte, then message traffic in one-byte transfers.
This established a host-loaded volatile DSP program. SPI carries firmware and
control, not the ALSA/BlueALSA PCM stream. The
[DSP boundary](emulation/dsp-boundary.md) records framing and GPIO ownership.

The MCU returned application `000116`, recovery flag `0`, and input events via
GPIO 3 as a falling-edge input. With GPIOs available, DSP startup returned
`EVENT_DSP_BOOTUP` and version `0x6458` / `25688`. The
[MCU boundary](emulation/mcu-boundary.md) owns the shared expander contract.

## Audio and microphone evidence

The audio kernel exposed card 0 `Loopback` and card 1 `marvell-wm8904`, with
one playback and one capture substream on card 1 PCM 0. The WM8904 probe at
I2C `0x1a` returned `-121`; the machine-link name is not physical codec
identification. MCU-controlled DAC/amplifier gates and the SPI-loaded DSP
participated in verified output.

Muted zero-sample playback at 48 kHz stereo `S32_LE` completed without xrun,
DMA error or kernel fault. A guarded low-level tone, with explicit unmute only
for the playback window and remute afterward, was audibly confirmed.
That established output independently of transport negotiation.

Capture requires the Berlin buffer geometry: 2,048-byte periods, 16 periods,
32 KiB total. The working TinyALSA shape was:

```text
tinycap <ram-output.wav> -D 1 -d 0 -c 2 -r 48000 -b 32 -p 256 -n 16
```

This is 256 frames per period and 4,096 frames per buffer. TinyALSA defaults
and sweeps omitting all 16 periods failed. Later attended capture correlated
speech/taps while unmuted and returned all-zero samples while muted.
The [capture reference](microphone-capture.md) owns privacy and restart details;
these RAM measurements do not prove electrical microphone disconnection,
beamforming or AEC activation.

Physical rotary events matched WAMP publications and actual mixer changes.
The donor Bluetooth stack negotiated A2DP and received SBC from two sources,
but never delivered decoded PCM to ALSA. A metadata-only module workaround also
created `hci0` without a usable first HCI command. Neither was accepted as
working playback.

BlueZ 5.55 and patched BlueALSA 4.0.0 replaced donor Bluedroid. The verified
path received SBC, decoded to PCM, advanced ALSA/DMA and produced attended
audible output. PCM travels through BlueALSA/ALSA, not through the DSP daemon.

## Owned runtime and safety

PID 1 supervises the runtime. `reinvoke-mcu-interface` owns MCU input, LEDs,
public Mic-Mute policy and speaker gates. `reinvoke-dsp-interface` owns DSP
SPI/GPIO/reset, seven public WAMP registrations and a private root-only
microphone socket. Bonefish remains a narrow compatibility router, not the
product policy owner. Donor supervisor, updater and hardware daemons are
comparison material rather than owned-runtime dependencies.

The first automatic-unmute candidate reached ALSA `RUNNING` but was inaudible
and coincided with sustained I2C arbitration loss. It was rejected.
The replacement gate requires positive PCM FIFO reads, a live RAM lease and
the expected ALSA-owning worker TID/executable while PCM is `RUNNING`.
MCU reads require a real `POLLPRI` edge, avoiding the prior I2C read flood.
An ALSA state alone is not evidence of useful audio or permission to unmute.

Media volume and DSP gain are different interfaces:

* `com.harman.volumeSet` takes `[value, "music"]`; `[value]` is invalid.
  Media percent maps to BlueALSA as `(percent * 127 + 50) / 100`.
* `com.harman.dsp.volumeSet` sends raw opcode `0x04` gain and receives
  `EVENT_NEW_DAC_GAIN`; it cannot overcome muted or zero media volume.
* A new BlueALSA transport defaulted to maximum. Owned startup caps a new peer
  without raising an already quieter setting. The cap is not an acoustic
  safety guarantee, and the scale is amplitude-linear, not loudness-linear.

## Network lifecycle

RAM tests established station association, DHCP, routing and DNS, then cleanup
on service termination or station loss and restoration after reconnect.
`reinvoke-networkd` owns the DHCP child and volatile resolver state.
Its lifetime lock rejects a second supervisor; owner-token checks prevent a
stale record or DHCP callback from disturbing an unrelated live process.
Credential replacement restarts the relevant children without replacing the
supervisor. PID 1 retries networkd after five seconds; `reinvoke.networkd=off`
supports manual recovery.

Provisioning uses an isolated temporary AP, ephemeral TLS and authenticated
credential delivery. The network-facing parser lacks radio/shell privileges;
the privileged apply socket requires a UID-0 peer. Station credentials and
bonds remain volatile. [Provisioning](native-provisioning.md) defines the exact
protocol, rather than the donor's unauthenticated HTTP setup flow.

RAM firewall, supervision, capture and restart acceptance remain RAM-scoped.
Candidate 03 later completed physical Wi-Fi provisioning and reached its SSH
listener, but authentication closed before a native shell. Its actual kernel,
PID 1, mounts and firewall have not been inspected through a successful login.
