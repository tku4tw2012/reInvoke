---
title: Invoke kernel build
description: Reproducible RAM-boot kernel build for the Invoke BG2CDP platform
ms.date: 2026-09-12
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
source archive, complete patched source-tree manifest, compiler, linker, NDK
archive, SPI and reproducibility patches, `lzop`, `mkimage`, and supplied DTB,
and build modules with `-fno-pic -fno-pie`.

The source-tree gate prevents a modified extracted tree from silently producing
a candidate. The builder still consumes that retained extracted tree rather
than reconstructing it from the source archive and applying every patch in a
new work directory, so the input is content-locked but the clean-room extraction
step remains future work.

A fresh full build through the gated path reproduced the accepted
`d29a0075...` kernel, supplied DTB, and all four module digests exactly. That
third independent deployable set is retained under
`build/artifacts/reinvoke-kernel-v14-9-provenance-20260906/`.

The GCC 9/11 compatibility patch `0001-modern-host-toolchain.patch` is
deliberately absent from this NDK GCC 4.9 path. The hardware kernel uses
`HOSTCFLAGS=-fcommon` for its host-side DTC instead, and applying patch 1 would
also change target ARM `uaccess` code. A scratch reconstruction from the
original 521 MB source archive plus patches 2–4 produced the exact 42,321-file
source manifest above, with no remaining file differences.

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

### Opt-in MTD removal cleanup fix

Existing profiles remain unchanged unless `--mtd-cleanup-fix` is supplied.
This variant applies
[`0005-fix-mtdblock-removal-lifetime.patch`](../../patches/invoke-kernel/0005-fix-mtdblock-removal-lifetime.patch)
only in a fresh `--source-work-dir` copy. It saves the bad-block map before
`del_mtd_blktrans_dev()` quiesces the device and releases its private allocation,
then frees the saved map without dereferencing or freeing the private object
again. The original source header credits David Woodhouse and licenses this
file under GPL-2.0-or-later; archive provenance remains
[`P1-041.json`](../../metadata/P1-041.json).

```bash
nice -n 10 tools/kernel/build-native-kernel.sh \
  --profile audio-sd8887 \
  --mtd-cleanup-fix \
  --source-work-dir ../reinvoke-archive/build/mtd-cleanup-source \
  --build-dir ../reinvoke-archive/build/mtd-cleanup-build \
  --dtb ../reinvoke-archive/build/artifacts/reinvoke-kernel-v14-9-provenance-20260906/reinvoke-audio-sd8887.dtb \
  --dtb-sha256 4dd7a39aa8c8d23ee824724e3f633ec16bb3a0f28c46cb096f0552dc28737dbb \
  --output-dir ../reinvoke-archive/build/artifacts/invoke-mtd-cleanup \
  --jobs 1
```

The fixed variant refuses existing or overlapping source-work, build, and output
paths; unlike the baseline path, it never cleans an existing build directory.
Its default build directory and image filenames gain `-mtd-cleanup`. Kernel
configuration and `LOCALVERSION` remain unchanged to permit comparison with
existing modules, not to assert untested compatibility.
The fixed variant pins `KBUILD_BUILD_VERSION=1-mtd-cleanup`, distinguishing it
in `uname -v` and `/proc/version` without changing `uname -r` or module vermagic.
The exact compiled `UTS_VERSION` and expected `/proc/version` line are captured
in `build-manifest.txt`; `kernel-compile.h` retains the generated header.

All existing checksum gates remain mandatory. The additional gates pin patch 5,
the original and corrected `mtdblock_ro.c`, and the full corrected source tree.
`build-manifest.txt` records the variant, both tree identities, patch and source
hashes, and the exact image digest and size. The retained tree-manifest ordering
uses the build host's `en_US.UTF-8` locale; use that locale when reproducing these
pins.

NDK GCC 4.9 embeds absolute source paths in some `__FILE__` diagnostics,
including `sd8xxx.ko`. Thus an isolated copy can change binary hashes even with
identical source/configuration. The fixed manifest records its source directory;
byte-for-byte reproduction requires the same absolute source-copy path, not
just identical contents. Archive the old copy before recreating that path for
a second clean build with new build/output directories. Do not repoint or
replace the preserved baseline source to obtain matching diagnostic strings.

Before pairing the result with an unchanged initramfs, compare configuration,
DTB, kernel release, module hashes and symbol versions against that initramfs's
accepted kernel/module set, and inspect the compiled removal callback. Offline
verification does not authorize a RAM reload or NAND write; hardware acceptance
remains a separate gate.

Initial offline verification on 2026-09-09 retained a successful clean rebuild under
`build/artifacts/invoke-kernel-audio-sd8887-mtd-cleanup-20260909T1310Z-repro/`
in the external archive. This earlier image predates the distinct
`#1-mtd-cleanup` build identifier and is not the identified recovery handoff:

* Image: `81_IMAGE.reinvoke-audio-sd8887-mtd-cleanup`, 3,548,079 bytes.
* SHA-256: `d64f6da60f957c6308e81c200179c7ea8b9b0e54b701ebbd0af5c7c1f4ebe985`.
* Clean rebuild matched the first artifact's image, DTB, configuration,
  `vmlinux`, `System.map`, `Module.symvers`, and all four modules exactly.
* Configuration and DTB match RC12. Three modules match RC12 byte-for-byte;
  `sd8xxx.ko` differs in diagnostic source paths, but its executable disassembly,
  relocations, imports, export sizes, and module metadata match. Symbol version
  CRCs are disabled in this configuration; `Module.symvers` remains identical.
* Linked callback disassembly saves the map before generic deletion and frees
  it once afterward. Original source/build hashes and RC12's `82_IMAGE` remain
  unchanged. This establishes offline ABI compatibility, not hardware acceptance.

Verification scripts, disassembly, logs, provenance, and source manifests are
under `build/invoke-mtd-cleanup-20260909T1310Z/verification/` in the archive.

The identified recovery handoff is
`build/artifacts/invoke-kernel-audio-sd8887-mtd-cleanup-20260909T1337Z-identified/81_IMAGE.reinvoke-audio-sd8887-mtd-cleanup`
(3,548,071 bytes), SHA-256
`708c8a17a2817b1d44216b1597e2e3cf6d59d366d95d804bdb96b16d2ecaf32e`.
Its compiled `UTS_VERSION` is
`#1-mtd-cleanup SMP PREEMPT Thu Jan 1 00:00:00 UTC 1970`, unlike RC12's
`#1 SMP PREEMPT Thu Jan 1 00:00:00 UTC 1970`. The clean build retains identical
configuration, DTB, symbol ABI, and all four modules from the ABI-verified fixed
build above. `ram-handoff-manifest.txt` records the full expected `/proc/version`
line and compatibility with the unchanged `pre-nand-rc12-20260908/82_IMAGE`
(SHA-256 `a0f273ddfb4a7a3844078b88f4ff826a1f1d1c7ae8c01b77b87533e3bf8985de`).
A forced version-object incremental rebuild reproduced `vmlinux`, the compiled
header, configuration, symbol table, `zImage`, and the packaged image exactly.
Evidence is in the adjacent build's `verification-identified/` directory.
No recovery bootstrap directory, live device, or NAND-write gate was modified.

The GCC 11 ACast research control remains available separately:

```bash
tools/kernel/build-invoke-kernel.sh \
  --output-dir ../reinvoke-archive/build/artifacts/invoke-kernel-acast
```

The script:

1. Verifies the archived source SHA-256.
2. Copies the preserved source to a disposable work directory.
3. Applies the pinned compatibility patch.
4. Applies the pinned fail-fast SPI GPIO patch and the two reproducibility
   patches.
5. Loads the ACast defconfig.
6. Enables the Marvell UDC and Android USB composite gadget.
7. Builds the appended-DTB U-Boot image and modules.
8. Writes an artifact manifest and SHA-256 file.

It refuses an existing output directory and a work directory with a different
patch marker.

## Reproducibility

The builder pins `KBUILD_BUILD_TIMESTAMP`, `KBUILD_BUILD_USER`,
`KBUILD_BUILD_HOST`, and `SOURCE_DATE_EPOCH`. Two further patches remove build
timestamps the kernel embeds on its own:

| Patch | Removes |
|---|---|
| `0003-reproducible-yaffs-build-id.patch` | `__DATE__` and `__TIME__` strings compiled into the YAFFS driver |
| `0004-reproducible-lzo-piggy.patch` | The lzop header modification time in the compressed kernel payload |

The LZO case is worth knowing about, because it hides well. `vmlinux` and
`System.map` can be byte-identical while `zImage` still differs. `cmd_lzo`
originally piped the payload into lzop, and lzop reading a pipe has no source
file to take a modification time from, so it stored the current clock and the
header checksum covering it. That produced six differing bytes at a constant
image size. The patch compresses the input as a named file with its mode and
modification time pinned first.

Nothing the kernel reads changes. `parse_header` in `lib/decompress_unlzo.c`
skips the mode, both mtime words, the file name, and the header checksum.

Confirm reproducibility by building twice into separate output and work
directories and comparing `81_IMAGE.*`.

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
