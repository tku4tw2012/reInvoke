---
title: Invoke kernel build
description: Pinned RAM-kernel profiles, MTD cleanup variant and reproducibility boundaries
ms.date: 2026-09-12
ms.topic: how-to
---

The hardware-validated custom RAM/recovery path builds Linux 3.8.13 with NDK
GCC 4.9 and a pinned incremental DTB. GCC 9/11 controls did not boot.
Native candidates 02/03 instead retain vendor 12.2134.0 boot payload bytes;
these builds are not observations of their unread running kernels.
See the [native guide](../../docs/native-nand-platform.md).

Commands run from the repository root. `REINVOKE_ARCHIVE` must contain the
retained source, toolchain and accepted DTBs; a public clone is insufficient.
Use fresh outputs according to each builder's checks.

## Inputs

| Input                     | Identity                                                           |
| ------------------------- | ------------------------------------------------------------------ |
| Source                    | `https://archive.org/download/invoke-kernel/Invoke-kernel.tar`     |
| Size                      | 545,398,784 bytes                                                  |
| SHA-256                   | `bd19dff0f8ef8879b82d4cdeec9f127a105905ea0aa47e76de31192a79a79126` |
| Source license/provenance | GPL-2.0-only archive; [P1-041](../../metadata/P1-041.json)         |
| Target compiler           | Android NDK r10e GCC 4.9, explicit BFD linker                      |
| NDK archive SHA-256       | `ee5f405f3b57c4f5c3b3b8b5d495ae12b660e03d2112e4ed5c728d349f1e520c` |
| Toolchain provenance      | [P1-042](../../metadata/P1-042.json)                               |
| Host tools                | `lzop` 1.04, `mkimage` from u-boot-tools 2022.01                   |

The NDK supplies kernel compilation tools, not Android target libraries.
Modules require `KCFLAGS="-fno-pic -fno-pie"` to avoid unresolved
`_GLOBAL_OFFSET_TABLE_` references.

## Build profiles

```bash
tools/kernel/build-native-kernel.sh --profile spi-gpio \
  --archive-root "${REINVOKE_ARCHIVE}" \
  --dtb "<accepted-spi-gpio.dtb>" --dtb-sha256 "<reviewed-dtb-sha256>" \
  --output-dir "<new-kernel-output>"
```

| Profile/builder                 | Contract                                                         |
| ------------------------------- | ---------------------------------------------------------------- |
| `baseline`, `spi-gpio`, `audio` | `berlin2cdp_amp_defconfig`, GCC 4.9, supplied pinned DTB         |
| `audio-sd8887`                  | Native SD8887 STA/uAP pair instead of recovery-compatible SD8801 |
| `build-invoke-kernel.sh`        | GCC 11 ACast build control, not hardware-accepted                |

The native builder gates the source archive, full patched tree, compiler,
linker, NDK archive, patches, `lzop`, `mkimage` and DTB. It consumes an already
extracted retained source tree; extraction and complete patch reconstruction
are not automated from a fresh clone.

Patches 2-4 reconstruct the 42,321-file accepted native source manifest
`6ae65ab02757536de83e489b4db967bd39e0969d40ae5bcce7fb478cadd1b42f`.
Patch 1, `0001-modern-host-toolchain.patch`, is deliberately absent: it changes
ARM `uaccess` as well as host compatibility. GCC 4.9 uses `HOSTCFLAGS=-fcommon`
for the old DTC instead.

The Bluetooth directory is selected by `BERLIN_SDIO_BT_8887`, but its local
Makefile incorrectly keys `bt8xxx.o` on `BERLIN_SDIO_WLAN_8887`. The builder
supplies that selector only for the Bluetooth subdirectory, not a second Wi-Fi
driver. STA/uAP availability does not change the RAM loader's station-only
default; select `--wifi-mode sta-uap` explicitly for AP provisioning.

## Opt-in MTD removal cleanup fix

`--mtd-cleanup-fix` applies
[patch 5](../../patches/invoke-kernel/0005-fix-mtdblock-removal-lifetime.patch)
only in a fresh source copy. It saves the bad-block map before
`del_mtd_blktrans_dev()` quiesces/releases the private allocation, then frees
the saved map without dereferencing or freeing that private object again.
The changed file retains its David Woodhouse GPL-2.0-or-later notice.

```bash
nice -n 10 tools/kernel/build-native-kernel.sh --profile audio-sd8887 \
  --archive-root "${REINVOKE_ARCHIVE}" --mtd-cleanup-fix \
  --source-work-dir "<new-source-copy>" --build-dir "<new-build-dir>" \
  --dtb "<accepted-audio-sd8887.dtb>" --dtb-sha256 "<reviewed-dtb-sha256>" \
  --output-dir "<new-fixed-kernel-output>" --jobs 1
```

The variant refuses existing or overlapping source/build/output paths and
does not clean an existing build directory. Baseline profiles are unchanged
without the flag. Extra gates cover patch 5, original/corrected
`mtdblock_ro.c` and the complete corrected tree. Manifest ordering uses the
recorded `en_US.UTF-8` locale.

Configuration and `LOCALVERSION` stay unchanged for ABI comparison.
`KBUILD_BUILD_VERSION=1-mtd-cleanup` distinguishes `uname -v`/`/proc/version`
without changing `uname -r` or module vermagic. `build-manifest.txt` records
the variant, tree identities, source path, hashes and expected version;
`kernel-compile.h` retains the compiled header.

The identified recovery handoff is
`81_IMAGE.reinvoke-audio-sd8887-mtd-cleanup`, 3,548,071 bytes, SHA-256
`708c8a17a2817b1d44216b1597e2e3cf6d59d366d95d804bdb96b16d2ecaf32e`.
Its compiled version is
`#1-mtd-cleanup SMP PREEMPT Thu Jan 1 00:00:00 UTC 1970`.
The earlier unlabelled fixed build is not this handoff.

`ram-handoff-manifest.txt` records compatibility with unchanged RC12
`82_IMAGE`, SHA-256
`a0f273ddfb4a7a3844078b88f4ff826a1f1d1c7ae8c01b77b87533e3bf8985de`.
Offline comparisons retained configuration, DTB, module/symbol ABI and verified
the compiled callback's single free. Three modules matched RC12 exactly;
`sd8xxx.ko` differed in diagnostic source paths, not executable disassembly,
relocations, imports or module metadata. CRC symbol versioning is disabled;
`Module.symvers` remained identical. These checks are not runtime acceptance.

## Reproducibility

Builders pin `KBUILD_BUILD_TIMESTAMP`, user, host and `SOURCE_DATE_EPOCH`.
Patch 3 removes YAFFS `__DATE__`/`__TIME__`; patch 4 pins the lzop input's
mode/mtime. Without patch 4, identical `vmlinux`/`System.map` can still produce
different `zImage` bytes because pipe-fed lzop embeds the current clock.
The kernel decompressor skips these header fields.

NDK GCC 4.9 embeds absolute source paths in `__FILE__` diagnostics. Byte-exact
comparison therefore needs the same recorded absolute source-copy path,
not just matching contents. Use new build/output directories; do not mutate
the preserved baseline source to obtain matching strings.

Before reusing an initramfs, compare configuration, DTB, release, modules and
symbol ABI with its accepted set. Full native-profile rebuilds have reproduced
accepted image and module digests from held inputs; that is narrower than a
clean public-source rebuild.

## Incremental hardware device tree

The ACast reference changes several subsystems together and failed the initial
RAM boot. Incremental builders extend the known-good recovery DTB:

```bash
tools/kernel/build-spi-dtb.sh \
  --input "<accepted-recovery.dtb>" --output "<new-spi-gpio.dtb>"
tools/kernel/build-audio-dtb.sh \
  --input "<accepted-spi-gpio.dtb>" --output "<new-audio.dtb>"
```

Both gate their inputs. SPI additions are DesignWare at `0xF7E81C00`, APB
interrupt 7, four chip selects, `spidev0.0` on CS0 at 1 MHz, and
`base-gpio = <0>` on the first GPIO bank. Without that property the acquired
driver unregisters the bank, losing MCU GPIO3 and DSP GPIO4/5/12/13/15.

The audio extension adds source-backed WM8904, Berlin I2S/GDMA and ASoC machine
nodes. Donor `modules.builtin` independently lists WM8904, Berlin ASoC and
ALSA loopback. This does not establish every electrical limit or DSP identity.

## GCC 11 research control

```bash
tools/kernel/build-invoke-kernel.sh --archive-root "${REINVOKE_ARCHIVE}" \
  --output-dir "<new-acast-output>"
```

This uses `berlin2cdp_a0_amp_acast_defconfig` and appended
`berlin2cdp-a0-acast.dts`, applies modern-host/SPI/reproducibility patches,
enables the Marvell UDC/Android gadget, and writes image/module manifests.
It refuses existing output and work trees with a mismatched patch marker.
Its reproducible image did not boot; use the GCC 4.9 path for RAM development.
