---
title: Invoke kernel build
description: Reproducible RAM-boot kernel build for the Invoke BG2CDP platform
ms.date: 2026-09-02
ms.topic: how-to
---

The replacement-kernel track builds Linux `3.8.13-reinvoke` from the archived
Invoke GPL source. It produces a U-Boot legacy image with the
`berlin2cdp-a0-acast` device tree appended.

The source archive and all generated binaries remain outside Git. The
repository contains only provenance metadata, the compatibility patch, and the
build driver.

## Inputs

| Input | Identity |
|-------|----------|
| Source URL | `https://archive.org/download/invoke-kernel/Invoke-kernel.tar` |
| Source size | 545,398,784 bytes |
| Source SHA-256 | `bd19dff0f8ef8879b82d4cdeec9f127a105905ea0aa47e76de31192a79a79126` |
| Source license | GPL-2.0-only |
| Defconfig | `berlin2cdp_a0_amp_acast_defconfig` |
| Device tree | `berlin2cdp-a0-acast.dts` |
| Compatibility patch | `patches/invoke-kernel/0001-modern-host-toolchain.patch` |

The source archive metadata is recorded in
[`P1-041.json`](../../metadata/P1-041.json).

## Host requirements

```bash
sudo apt-get install -y \
  gcc-arm-linux-gnueabihf \
  lzop \
  u-boot-tools
```

The verified build used:

* `arm-linux-gnueabihf-gcc` 11.4.0
* `lzop` 1.04
* `mkimage` from `u-boot-tools` 2022.01

A second compiler control uses the official Google Android NDK r10e ARM
toolchain:

* `arm-linux-androideabi-gcc` 4.9, 2014-08-27 prerelease
* Archive SHA-1 `f692681b007071103277f6edc6f91cb5c5494a32`
* Archive SHA-256 `ee5f405f3b57c4f5c3b3b8b5d495ae12b660e03d2112e4ed5c728d349f1e520c`

Ubuntu's `google-android-ndk-r10e-installer` package independently verifies the
SHA-1 for the Google-hosted archive. See
[`P1-042.json`](../../metadata/P1-042.json). The NDK compiler is used only for
kernel compilation; no Android target library is linked into the kernel.

This GCC 4.9 toolchain produced the first rebuilt kernel that booted on the
physical Invoke. USB returned seven seconds after `bootm`. GCC 9 and GCC 11
control images using the same load address and byte-identical recovery DTB did
not return USB.

For loadable modules, pass `KCFLAGS="-fno-pic -fno-pie"`. The Android compiler
otherwise leaves `_GLOBAL_OFFSET_TABLE_` references that the Linux module
loader cannot resolve.

The compatibility patch adds the Linux 3.8 GCC-major wrapper, fixes the old DTC
`yylloc` definition for modern host compilers, and backports the later ARM
`put_user` register-binding pattern. It does not modify board drivers.

## Build

Use the hardware-verified NDK GCC 4.9 path for native RAM kernels:

```bash
tools/kernel/build-native-kernel.sh \
  --profile spi-gpio \
  --dtb ../reinvoke-archive/build/artifacts/reinvoke-spi-gpio.dtb \
  --dtb-sha256 <reviewed-dtb-sha256> \
  --output-dir ../reinvoke-archive/build/artifacts/invoke-native-spi-gpio
```

The `baseline`, `spi-gpio`, and `audio` profiles all start from
`berlin2cdp_amp_defconfig`, use the explicit NDK BFD linker, checksum-gate the
source, compiler, NDK archive, and supplied DTB, and build modules with
`-fno-pic -fno-pie`.

The separate `audio-sd8887` profile disables the recovery-compatible SD8801
module and builds the disclosed native SD8887 STA/uAP pair. It is not the
hardware default until a RAM boot verifies it. Use
`boot-native-ram.sh --wifi-mode sta-uap` only for that attended validation;
station-only remains the safe default.

The Invoke source's Bluetooth directory is selected by
`BERLIN_SDIO_BT_8887`, but its local Makefile mistakenly keys `bt8xxx.o` on
`BERLIN_SDIO_WLAN_8887`. The builder supplies that selector only to the
Bluetooth subdirectory build. It does not enable or package the second Wi-Fi
driver.

The GCC 11 ACast research control remains available separately:

```bash
tools/kernel/build-invoke-kernel.sh \
  --output-dir ../reinvoke-archive/build/artifacts/invoke-kernel-acast
```

The script:

1. Verifies the archived source SHA-256.
2. Copies the preserved source to a disposable work directory.
3. Applies the pinned compatibility patch.
4. Loads the ACast defconfig.
5. Enables the Marvell UDC and Android USB composite gadget.
6. Builds the appended-DTB U-Boot image and modules.
7. Writes an artifact manifest and SHA-256 file.

It refuses an existing output directory and a work directory with a different
patch marker.

## Verified first build

| Artifact | Value |
|----------|-------|
| Kernel release | `3.8.13-reinvoke` |
| Image | `81_IMAGE.reinvoke` |
| Image size | 3,816,179 bytes |
| Image SHA-256 | `f2fdec3a09e3c8c90045c2d15281bd0d9b8b4c26a98404554bd4a730234ab8e1` |
| U-Boot load address | `0x01108000` |
| U-Boot entry point | `0x01108000` |
| Loadable modules | 5 |

The image enables Berlin ALSA, DesignWare SPI with `spidev`, SD8887 Wi-Fi and
Bluetooth modules, NAND with the Berlin randomizer, I2C, GPIO, and the Android
USB gadget. Hardware behavior must still be validated from RAM before the
image is considered a usable board kernel.

The GCC 11 ACast image did not boot on hardware. It remains an attributed build
artifact, not the active kernel baseline.

The next GCC 4.9 image adds only DesignWare SPI and the checksum-gated
SPI diagnostic DTB. It returned USB in six seconds and created
`/dev/spidev0.0`. The donor `dsp-client` then opened SPI and registered its
WAMP procedures.

## Incremental hardware device tree

The ACast reference device tree changes memory, shared-memory, timer, GPIO,
I2C, SPI, and audio nodes at once. Its first RAM boot did not reach USB, so
reInvoke now adds hardware boundaries to the exact known-good recovery DTB one
at a time.

Build the SPI-only diagnostic DTB:

```bash
tools/kernel/build-spi-dtb.sh \
  --input ../reinvoke-archive/build/artifacts/known-good-recovery.dtb \
  --output ../reinvoke-archive/build/artifacts/reinvoke-spi-only.dtb
```

The input is checksum-gated. The output adds only:

* DesignWare SPI at `0xF7E81C00`, APB interrupt 7
* Four chip selects
* `spidev0.0` on chip select 0
* A conservative 1 MHz maximum frequency
* `base-gpio = <0>` on the first SoC GPIO bank

This diagnostic node exposes the transport expected by `dsp-client`; it does
not identify the DSP part or prove that chip select 0 and the selected frequency
are correct.

The low-bank property is required by the acquired Invoke `gpio-dwapb` driver.
Without it the platform driver binds but unregisters the bank, leaving no
gpiochip for MCU GPIO 3 or DSP GPIOs 4, 5, 12, 13, and 15.

After SPI and the first GPIO bank are verified, build the audio DTB:

```bash
tools/kernel/build-audio-dtb.sh \
  --input ../reinvoke-archive/build/artifacts/reinvoke-spi-gpio.dtb \
  --output ../reinvoke-archive/build/artifacts/reinvoke-audio.dtb
```

This adds the WM8904, Berlin I2S/GDMA, and ASoC machine nodes found in the
Invoke GPL source. The active 12.2050.3 rootfs's `modules.builtin` independently
confirms that its normal kernel includes WM8904, Berlin ASoC, and ALSA loopback.
