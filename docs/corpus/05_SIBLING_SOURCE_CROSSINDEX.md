---
doc_id: hki-sibling-crossindex
title: Harman Kardon Invoke — Sibling Source Cross-Index
version: "0.1"
date: "2026-08-28"
status: working-crossindex
---

# Sibling Source Cross-Index

## Purpose and method

This document records a first-pass cross-reference of locally mirrored
sibling-platform source against the Harman Kardon Invoke (Marvell 88DE3006 /
BG2CDP, Berlin family, Linux 3.8.13-mrvl). The goal is to find real Berlin
source that transfers to the Invoke, and to record misses explicitly so the
work is not repeated.

Evidence discipline governs this document. Every finding cites repository,
git ref, and exact path. Where a statement is an inference rather than a
direct quote, it is labeled as inference. Nothing here was executed; files
were read only.

### Corpus and refs actually searched

| Mirror | Path | Ref used | Nature |
|---|---|---|---|
| Steam Link SDK | git-mirrors/valve/steamlink-sdk.git | master | Vendor Berlin BG2CD BSP + kernel 3.8-era |
| Kinoma Acorn kernel | git-mirrors/kinoma/acorn_kernel.git | linux-3.18.7-15t2_acorn-dev | Near-mainline 3.18 Berlin |
| Kinoma Acorn U-Boot | git-mirrors/kinoma/acorn_uboot.git | master | Armada 100 (arm926ejs), not Berlin |
| Kinoma JS | git-mirrors/kinoma/kinomajs.git | master | JS runtime |
| Nest bootloader | git-mirrors/google-nest/bootloader.git | main | Generic upstream U-Boot (Copybara), not Berlin |
| Mainline berlin (sparse) | git-mirrors/linux/linux-berlin-sparse | master | dts/mach-berlin/clk/pinctrl only |
| HKHacking | git-mirrors/community/HKHacking.git | main + tags | Community docs, no source |

Key structural fact established up front: the Steam Link SDK ships a full
Marvell vendor Berlin BSP under `kernel/arch/arm/mach-berlin/` (Galois
codebase with `modules/` for pe, nfc, i2c, gpio, cc, shm, bt_sd8897,
fastlogo, gpu, gpu3D, nfc). The Kinoma Acorn kernel's `mach-berlin/` is the
mainline-style tree (berlin.c, platsmp.c) and does not carry the vendor audio
or NAND modules. This makes Steam Link the primary transfer source.

## Question 1 — Audio path

Mainline Linux has no Berlin audio driver. Neither Steam Link nor Acorn has a
`sound/soc/berlin` ASoC tree. However, Steam Link ships a real vendor Berlin
audio-out (AOUT) driver inside the presentation-engine module. It drives the
BG2CD audio DMA hub (DHUB) channels, exposes a userspace command-FIFO
protocol, and enumerates the multichannel/I2S, SPDIF, and HDMI audio paths.

### Findings

| Repo | Path | What it establishes |
|---|---|---|
| valve/steamlink-sdk.git @ master | kernel/arch/arm/mach-berlin/modules/pe/pe_driver.c | Real AOUT driver: `p_ma_fifo`/`p_sa_fifo`/`p_spdif_fifo`/`p_hdmi_fifo`, `aip_i2s_pair`, IOCTLs `AOUT_IOCTL_START_CMD 0xbeef2001`, `AOUT_IOCTL_STOP_CMD 0xbeef2004`, `AIP_IOCTL_START_CMD 0xbeef2002` |
| valve/steamlink-sdk.git @ master | kernel/arch/arm/mach-berlin/modules/pe/pe_driver.h | `AUDIO_PATH` enum `MULTI_PATH=0, LoRo_PATH=1, SPDIF_PATH=2, HDMI_PATH=3`; `pri_audio_chanId[4]` = `ag_MA0_R..ag_MA3_R`; `AOUT_PATH_CMD_FIFO` struct (kernel/user DMA ring) |
| valve/steamlink-sdk.git @ master | kernel/arch/arm/mach-berlin/modules/pe/gsinc/Firmware_Berlin_BG2CD_A0/avioDhub.h | BG2CD-specific DHUB channel map: `avioDhubChMap_ag_MA0_R..MA3_R`, `ag_SPDIF_R=0x5`, `vpp_SPDIF_W=0x8`, `vip_MIC0_W..MIC3_W` |
| valve/steamlink-sdk.git @ master | kernel/arch/arm/mach-berlin/modules/pe/avio_dhub_drv.c | DHUB DMA engine driver used by the audio/video paths |

Verifiable excerpt (pe_driver.h):

```
typedef enum {
	MULTI_PATH = 0,
	LoRo_PATH = 1,
	SPDIF_PATH = 2,
	HDMI_PATH = 3,
	MAX_OUTPUT_AUDIO = 5,
} AUDIO_PATH;

INT32 pri_audio_chanId[4] =
    { avioDhubChMap_ag_MA0_R, avioDhubChMap_ag_MA1_R, avioDhubChMap_ag_MA2_R,
	avioDhubChMap_ag_MA3_R };
```

Scope note (inference): this is the kernel half of a firmware-driven pipeline.
The kernel feeds PCM through the AOUT command FIFO and DHUB DMA channels; the
actual I2S serializer and clocking are handled by the on-chip audio firmware
(ZSP), referenced by `gsinc/.../zspWrapper.h`. The driver proves the DMA
channel map, the I2S pair concept (`aip_i2s_pair`), and the exact IOCTL
framing, but it is not a self-contained ASoC codec driver and does not by
itself un-mute an external amplifier.

Status: ANSWERED (existence and location of transferable Berlin audio-out
source). The BG2CD variant of the DHUB map is present verbatim; the Invoke's
BG2CDP map is the sibling of this file.

## Question 2 — I2C and MCU

The Berlin I2C controllers are Synopsys DesignWare cores. Steam Link provides
both a device-tree description (four `snps,designware-i2c` masters) and the
vendor register-level driver, and the base-address arithmetic lets us map the
Linux bus index to a physical controller.

### Findings

| Repo | Path | What it establishes |
|---|---|---|
| valve/steamlink-sdk.git @ master | kernel/arch/arm/boot/dts/berlin2cd.dtsi | `i2c0: i2c@0` reg `0xF7E81400` IRQ16 (apb_ictl); `i2c1` `0xF7E81800`; `i2c2` `0xF7FC7000` (sm_ictl); `i2c3` `0xF7FC8000`. Also legacy `berlin,apb-twsi`@F7E81400 and `berlin,sm-twsi`@F7FC7000 |
| valve/steamlink-sdk.git @ master | kernel/arch/arm/mach-berlin/modules/i2c/i2c_master.c | `I2C_MASTER_NUM 4`; `I2C_MASTER_BASEADDR[] = {INST0,INST1,INST2,INST3}`; RX/TX FIFO sizes `{64,64,8,8}`; default speed 400 kHz |
| valve/steamlink-sdk.git @ master | kernel/arch/arm/mach-berlin/include/mach/galois_platform.h | `MEMMAP_APBPERIF_REG_BASE 0xF7E80000`; `APB_I2C_INST0_BASE = base+0x1400`; INST2/3 = `SM_APB_I2C0/1_BASE` |
| valve/steamlink-sdk.git @ master | kernel/arch/arm/boot/dts/berlin2cd.dtsi | GPIO: banks A-F, each `snps,dw-apb-gpio-bank`, `nr-gpio = <8>`; APB GPIO at `0xF7E80400/0800/0C00/1000`, SM GPIO at `0xF7FCC000` and `0xF7FC5000` |

Bus mapping (inference, deterministic from the two files above): Linux
`/dev/i2c-0` corresponds to the `i2c0` DT node at APB base `0xF7E81400`
(= `APB_I2C_INST0_BASE`, master id 0), the primary APB-domain I2C. Masters 2
and 3 live in the System-Manager (SM) domain. GPIO numbering convention:
eight lines per bank, four APB banks (A-D) then SM banks (E-F); sysfs
gpiochip base ordering follows registration order of these controllers
(exact base offsets are board/kernel-version dependent and not fixed by these
files).

Status: PARTIALLY ANSWERED. Controller driver, DT bus-to-address mapping, and
GPIO bank convention are established. The Invoke's actual MCU command protocol
on i2c-0 and the specific GPIO handshake line numbers are Invoke-specific and
are not present in any sibling mirror.

## Question 3 — BG2CDP specifically

No mirror contains a `berlin2cdp` DTS, a `berlin2cdp_amp_defconfig`, or a
`bg2cdp` board file. What does exist is directly adjacent: a `berlin2cd-dongle`
board (the naming lineage the Invoke flash tool uses) and a `CONFIG_BERLIN2CDP`
build symbol inside the shared Marvell BSP, proving the codebase family is
BG2CDP-aware even though Steam Link itself builds BG2CD.

### Findings

| Repo | Path | What it establishes |
|---|---|---|
| valve/steamlink-sdk.git @ master | kernel/arch/arm/boot/dts/berlin2cd-dongle.dts | A real "dongle" board variant: `model = "MARVELL BG2CD Dongle board based on BERLIN2CD"`, `compatible = "marvell,berlin2cd-dongle"`. Confirms the `berlin2cd(p)-dongle` naming lineage referenced by the Invoke flash tooling |
| valve/steamlink-sdk.git @ master | kernel/arch/arm/mach-berlin/modules/gpu3D/Kbuild | `ifeq ($(CONFIG_BERLIN2CDP), y)` -> `-DSOC_BERLIN2CDP=1`. The shared BSP anticipates a BG2CDP SoC selection |
| valve/steamlink-sdk.git @ master | kernel/arch/arm/mach-berlin/modules/gpu3D/hal/os/linux/kernel/gc_hal_kernel_driver.c | `#if SOC_BERLIN2CDP` conditional code paths in the GPU driver |
| valve/steamlink-sdk.git @ master | kernel/arch/arm/configs/bg2cd_penguin_mlc_defconfig | Berlin BG2CD defconfig present (no CDP/amp variant); siblings `mv88de3100_ax_bg2cd_eureka_evt_defconfig` etc. |
| valve/steamlink-sdk.git @ master | kernel/arch/arm/mach-berlin/Kconfig | SoC choice offers `BERLIN2CD` (BG2 CD) only; `BERLIN2CDP` is not a declared Kconfig option here, only referenced by the GPU Kbuild |

Verifiable excerpt (berlin2cd-dongle.dts):

```
model = "MARVELL BG2CD Dongle board based on BERLIN2CD";
compatible = "marvell,berlin2cd-dongle", "marvell,berlin2cd";
```

Status: PARTIALLY ANSWERED. No BG2CDP DTS or `berlin2cdp_amp_defconfig` was
found in the corpus. However the exact board-naming lineage
(`berlin2cd-dongle`) and a live `CONFIG_BERLIN2CDP`/`SOC_BERLIN2CDP` symbol in
the shared Marvell BSP are established, so the Invoke's kernel-config lineage
is confirmed to descend from this codebase even though the CDP board files
themselves are absent.

## Question 4 — NAND and boot

The designated Nest bootloader mirror is generic upstream U-Boot (Copybara
import, DENX header, boards aspenite/LaCie/Seagate, no berlin/galois content).
It does not contain a Marvell Berlin bootloader, so the bootloader-specific
sub-questions cannot be answered from it. The kernel-side NAND controller,
including ECC strength and the Marvell scrambler, is present in Steam Link.

### Findings

| Repo | Path | What it establishes |
|---|---|---|
| google-nest/bootloader.git @ main | (whole tree) | MISS. Generic upstream U-Boot; no `berlin`, `galois`, `mv88de`, or `dhub` source. `git log` shows only "Project import generated by Copybara". No usbload/tftp2nand-to-Berlin, no OTP/secure-boot gate found here |
| valve/steamlink-sdk.git @ master | kernel/arch/arm/mach-berlin/modules/nfc/pxa3xx_nand_debu.c | Real Berlin NAND controller (PXA3xx-derived): full NAND ID/geometry table (pages 2048/4096/8192, blocks, ECC strength incl. `BCH_STRENGTH`), read-retry support, integrated `nand_randomizer` (scrambler) hook |
| valve/steamlink-sdk.git @ master | kernel/arch/arm/mach-berlin/modules/nfc/nand_randomizer.c / .h | Marvell NAND data randomizer/scrambler API (`mv_nand_randomizer_init`, `mv_nand_randomizer_randomize_page`), buffer max 4096. Directly relevant to reading raw NAND dumps |
| valve/steamlink-sdk.git @ master | kernel/arch/arm/mach-berlin/modules/nfc/prbs15.c, prbs.h | PRBS15 sequence generator used by the randomizer |
| kinoma/acorn_uboot.git @ master | arch/arm/cpu/arm926ejs/armada100/* | MISS for Berlin. This U-Boot is Armada 100 (arm926ejs), not Berlin; not applicable to Invoke boot |

Status: PARTIALLY ANSWERED. Kernel-side NAND ECC/BBT/randomizer layout for
Berlin is found and transferable. The bootloader questions (usbload /
tftp2nand implementations, BootROM USB-download entry conditions, secure-boot
/ OTP fuse gating) remain UNANSWERED because the Nest mirror is not the Berlin
bootloader. The single most important gap: no Marvell Berlin U-Boot / BootROM
source exists anywhere in the current corpus.

## Question 5 — WAMP / bonefish / autobahn-cpp

### Findings

| Repo | Result | What it establishes |
|---|---|---|
| all mirrors | MISS | No `bonefish`, `autobahn`, `wamp`, or `crossbar` (router) source in any mirror. The only `crossbar` hits are the unrelated OMAP `irq-crossbar` in acorn_kernel. HKHacking contains only docs (Devices/, docs/, Invoke.md), no WAMP source |

Status: UNANSWERED. The WAMP control-plane framing (bonefish router,
autobahn-cpp clients) is not recoverable from this corpus. It must come from
the Invoke rootfs itself or upstream bonefish/autobahn-cpp repositories, which
are not mirrored here.

## Highest-value files found

Exact locators so anyone can retrieve these later with
`git --git-dir=<repo> show <ref>:<path>`.

| Rank | Repo | Ref | Path | Why it matters |
|---|---|---|---|---|
| 1 | valve/steamlink-sdk.git | master | kernel/arch/arm/mach-berlin/modules/pe/pe_driver.c | The only real Berlin audio-out driver in the corpus; AOUT/AIP IOCTL framing and DHUB audio DMA. Direct lineage to Invoke speaker path |
| 2 | valve/steamlink-sdk.git | master | kernel/arch/arm/mach-berlin/modules/pe/pe_driver.h | AUDIO_PATH enum, `pri_audio_chanId` -> MA0..MA3, AOUT command-FIFO struct |
| 3 | valve/steamlink-sdk.git | master | kernel/arch/arm/mach-berlin/modules/pe/gsinc/Firmware_Berlin_BG2CD_A0/avioDhub.h | BG2CD DHUB channel/semaphore map (audio MA/SPDIF/MIC). Sibling of the Invoke BG2CDP map |
| 4 | valve/steamlink-sdk.git | master | kernel/arch/arm/boot/dts/berlin2cd.dtsi | I2C base addresses and bus indexing, GPIO bank layout, SoC memory map |
| 5 | valve/steamlink-sdk.git | master | kernel/arch/arm/mach-berlin/modules/i2c/i2c_master.c | DesignWare I2C master driver, 4 masters, FIFO sizes, base-address table |
| 6 | valve/steamlink-sdk.git | master | kernel/arch/arm/mach-berlin/modules/nfc/pxa3xx_nand_debu.c | Berlin NAND controller: ID table, BCH ECC, read-retry |
| 7 | valve/steamlink-sdk.git | master | kernel/arch/arm/mach-berlin/modules/nfc/nand_randomizer.c | NAND scrambler needed to interpret raw NAND dumps |
| 8 | valve/steamlink-sdk.git | master | kernel/arch/arm/boot/dts/berlin2cd-dongle.dts | Confirms the `berlin2cd(p)-dongle` board-naming lineage |
| 9 | valve/steamlink-sdk.git | master | kernel/arch/arm/mach-berlin/modules/gpu3D/Kbuild | Live `CONFIG_BERLIN2CDP` symbol proving BSP BG2CDP awareness |
| 10 | valve/steamlink-sdk.git | master | kernel/arch/arm/mach-berlin/include/mach/galois_platform.h | Canonical Berlin peripheral memory map (APB base 0xF7E80000) |

## Summary of answers

| Q | Topic | Status |
|---|---|---|
| 1 | Audio path | ANSWERED (Steam Link ships a real Berlin AOUT/DHUB driver; BG2CD DHUB map present) |
| 2 | I2C + MCU | PARTIALLY ANSWERED (controller, bus mapping, GPIO convention found; MCU protocol Invoke-specific) |
| 3 | BG2CDP | PARTIALLY ANSWERED (no CDP DTS/defconfig; dongle lineage and CONFIG_BERLIN2CDP symbol confirmed) |
| 4 | NAND / boot | PARTIALLY ANSWERED (kernel NAND ECC/randomizer found; Nest mirror is generic U-Boot, no Berlin bootloader) |
| 5 | WAMP / bonefish | UNANSWERED (no WAMP/bonefish/autobahn source in corpus) |

## Explicit misses (recorded to save future effort)

- Steam Link and Acorn have no `sound/soc/berlin` ASoC tree.
- Kinoma Acorn `mach-berlin/` is mainline-style and carries no vendor audio or NAND module.
- Kinoma Acorn U-Boot is Armada 100, not Berlin.
- Nest `bootloader.git` @ main is generic upstream U-Boot with no Berlin/galois content.
- No `berlin2cdp` DTS, no `berlin2cdp_amp_defconfig`, no `bg2cdp` board file anywhere.
- No bonefish, autobahn-cpp, or WAMP router source anywhere in the corpus.
- No Marvell Berlin BootROM/U-Boot source anywhere (the largest single gap).
