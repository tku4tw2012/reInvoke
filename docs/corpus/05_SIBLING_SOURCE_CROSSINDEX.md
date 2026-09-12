---
title: Berlin source and community cross-index
description: Useful source-code locators, bounded search results, and community research provenance
ms.date: 2026-09-12
---

## Source coverage

The August 2026 search compared Berlin-family implementations with Invoke's
BG2CDP/Linux 3.8.13-mrvl lineage. Later gmini/PSVR inspection broadened the
audio references. Historical branch searches were not commit-pinned; acquisition
metadata pins held snapshots, not necessarily every subsequent search.
These are source comparisons, not tested replacement kernels.

| Source                  | Reviewed ref                  | Scope and public provenance           |
| ----------------------- | ----------------------------- | ------------------------------------- |
| [Steam Link][steam]     | `master`                      | Vendor BG2CD BSP; [P1-020]            |
| [Acorn kernel][acorn]   | `linux-3.18.7-15t2_acorn-dev` | Mainline-style Berlin 3.18; [P1-031]  |
| [Acorn U-Boot][acornub] | `master`                      | Armada 100, not Berlin; [P1-032]      |
| [Kinoma JS][kinomajs]   | `master`                      | Runtime, not vendor BSP; [P1-030]     |
| [Nest bootloader][nest] | `main`                        | Generic upstream U-Boot; [P0-003]     |
| [gmini Linux][gmini]    | `master` in audio addendum    | Vendor 3.8.13 with BG2CDP paths       |
| [PSVR Linux][psvr]      | `master` in audio addendum    | Vendor 3.10.46 with `amp_core` split  |

The initial sparse mainline Berlin search covered DTS, machine, clock, and
pinctrl files only. Neither it nor the reviewed Steam Link/Acorn trees supplied
`sound/soc/berlin` ASoC. This does not describe gmini: the later [P2-005] snapshot
retains its `sound/soc/berlin/pcm.c` at commit
`764b617b647c91fe969332ceb690282ecdad4e0c`, retrieved 2026-09-03.
Metadata records its 25,405 bytes and full hash. A whole-tree mirror of gmini
or PSVR was not established by that single-file acquisition.

## Audio references

Paths below are relative to `arch/arm/mach-berlin/`; prepend `kernel/` for
Steam Link. They describe the presentation-engine/DHUB audio path, not the
separate SPI-controlled DSP or Invoke's external amplifier wiring.

| Tree       | Path                                                   | Useful interface                    |
| ---------- | ------------------------------------------------------ | ----------------------------------- |
| Steam Link | `modules/pe/pe_driver.c` and `pe_driver.h`             | AOUT/AIP ioctls and command FIFO    |
| Steam Link | `modules/pe/avio_dhub_drv.c`                           | Audio/video DHUB DMA implementation |
| Steam Link | `modules/pe/gsinc/Firmware_Berlin_BG2CD_A0/avioDhub.h` | BG2CD channel map                   |
| gmini      | `modules/pe/pe_driver.c` and `Makefile`                | Explicit `BERLIN_BG2CDP` paths      |
| PSVR       | `modules/amp_core/kernel/drv_aout.c`                   | Split-out 3.10 AOUT implementation  |

Steam Link exposes `AOUT_IOCTL_START_CMD=0xbeef2001`,
`AOUT_IOCTL_STOP_CMD=0xbeef2004`, and `AIP_IOCTL_START_CMD=0xbeef2002`.
`AOUT_PATH_CMD_FIFO` connects userspace and kernel DMA rings. Its `AUDIO_PATH`
values are multi=0, LoRo=1, SPDIF=2, HDMI=3, with `ag_MA0_R..ag_MA3_R` channels.
The vendor path also references on-chip ZSP firmware; it is not a standalone
ASoC codec driver.

PSVR retains the `ag_MA*` map alongside compile-time-selected BG4 `aio64b_MA*`
channels and exposes `ag_PDM_MIC_ch1`. Shared FIFO/DHUB primitives show lineage,
not a completed forward-port or Invoke microphone wiring. gmini is the closer
3.8.13/BG2CDP comparator. Invoke-specific [kernel work](../../tools/kernel/README.md),
[speaker playback](../emulation/owned-speaker-control.md), and
[capture](../microphone-capture.md) have independent evidence.

## I2C, GPIO, NAND, and boot

Steam Link's `kernel/arch/arm/boot/dts/berlin2cd.dtsi` and vendor
`modules/i2c/i2c_master.c` describe four DesignWare I2C masters:
`0xF7E81400`, `0xF7E81800`, `0xF7FC7000`, `0xF7FC8000`, FIFO depths
64/64/8/8 and a 400 kHz default. The last two are in the System Manager domain.
`include/mach/galois_platform.h` supplies the APB base `0xF7E80000`.
GPIO banks have eight lines each: four APB banks and two SM banks. Linux bus
and GPIO numbering still depend on registration; Invoke's actual handshake
and commands are in the [MCU reference](../emulation/mcu-boundary.md).

`modules/nfc/pxa3xx_nand_debu.c` provides PXA3xx-derived NAND geometry,
BCH ECC and read-retry references. `nand_randomizer.c/.h` and `prbs15.c`
implement the Marvell scrambler. Their presence does not establish which
options ran on Invoke or make a logical capture a raw restore image.

The Steam Link dongle DTS and `modules/gpu3D/Kbuild` reference the shared
Berlin/BG2CDP lineage, but its SoC Kconfig selects BG2CD. The initial set lacked
a BG2CDP DTS/defconfig, Berlin U-Boot/BootROM, and WAMP/bonefish/autobahn source.
Acorn U-Boot and Nest's generic import do not fill those gaps. Later
[U-Boot observations](../uboot-access.md) and
[control-plane analysis](../emulation/control-plane-emulation.md) are separate.

## Community projects

* [HKHacking][hkhack] supplies Invoke USB/U-Boot/ADB reports, firmware releases,
  and the historical MTD map. Discussion #3 also records a `99_IMAGE` failure
  with loss of expected recovery. That image remains excluded from reInvoke's
  serving set; the report does not identify a modified ROM or universal failure.
* [Aristoddle][aristoddle] was reviewed at
  `948e85e2ddbdd560e186913cdfaad3f57f118c93` (2026-06-23), retained as [P2-004].
  MIT covers its documentation/tooling, not vendor firmware. Useful prior art
  includes split-marker USB-console handling, kernel read-only MTD flags,
  and RAM-initramfs inspection. Its compact partition proposal is compared
  with readback in the [hardware baseline](01_CANONICAL_HARDWARE_BASELINE.md#nand-and-filesystem-geometry).
  Wi-Fi/DHCP success relied on uncommitted run evidence; speaker integrations
  remained goals at that pin. WM8904 identity and immutable-ROM recovery
  guarantees were unsupported. Loader input mutation (`07_IMAGE`) and an
  incomplete command blocklist also limit direct reuse.
* The Fable 5 synthesis imported on 2026-09-02 usefully linked
  [ARM flasher][flasher], [PodiumFlashing][podium], [Chromecast tools][chromecast],
  and [Courk's Home Mini work][courk]. It was not a raw Invoke boot trace.
  `1286:8001`, exactly five re-enumerations, an `82_IMAGE`-only RAM boot,
  and universal recovery were unsupported; retained RAM boot uses both
  `81_IMAGE` and `82_IMAGE`. Later project recovery superseded its earlier
  rejection based on repeated `0xFE` sessions. Sibling module pinouts and
  host/adapter restrictions are leads, not measured Invoke requirements.

[steam]: https://github.com/ValveSoftware/steamlink-sdk
[acorn]: https://github.com/kinoma/acorn_kernel
[acornub]: https://github.com/kinoma/acorn_uboot
[kinomajs]: https://github.com/Kinoma/kinomajs
[nest]: https://nest-open-source.googlesource.com/manifest_repos/bootloader
[gmini]: https://github.com/courk/gmini-linux
[psvr]: https://github.com/fail0verflow/sony-psvr-linux
[hkhack]: https://github.com/coggy9/HKHacking/discussions/3
[aristoddle]: https://github.com/Aristoddle/hk-invoke-opensource-speaker/tree/948e85e2ddbdd560e186913cdfaad3f57f118c93
[flasher]: https://github.com/jryruegas92/hk-invoke-arm-flasher
[podium]: https://github.com/CaramelKat/PodiumFlashing
[chromecast]: https://github.com/tchebb/chromecast-tools
[courk]: https://courk.cc/running-custom-code-google-home-mini-part1
[P0-003]: ../../metadata/P0-003.json
[P1-020]: ../../metadata/P1-020.json
[P1-030]: ../../metadata/P1-030.json
[P1-031]: ../../metadata/P1-031.json
[P1-032]: ../../metadata/P1-032.json
[P2-004]: ../../metadata/P2-004.json
[P2-005]: ../../metadata/P2-005.json
