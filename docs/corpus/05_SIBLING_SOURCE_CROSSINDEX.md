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
| courk/gmini-linux | github.com/courk/gmini-linux | master | Vendor Galois BSP, **kernel 3.8.13**, carries **BERLIN_BG2CDP** targets (added later — see Question 1a) |
| fail0verflow/sony-psvr-linux | github.com/fail0verflow/sony-psvr-linux | master | Vendor Berlin BSP, **kernel 3.10.46**, `amp_core` audio split (added later — see Question 1a) |

Key structural fact established up front: the Steam Link SDK ships a full
Marvell vendor Berlin BSP under `kernel/arch/arm/mach-berlin/` (Galois
codebase with `modules/` for pe, nfc, i2c, gpio, cc, shm, bt_sd8897,
fastlogo, gpu, gpu3D, nfc). The Kinoma Acorn kernel's `mach-berlin/` is the
mainline-style tree (berlin.c, platsmp.c) and does not carry the vendor audio
or NAND modules. This makes Steam Link the primary transfer source **among the
trees originally mirrored**. That qualifier matters: two further vendor Berlin
BSP trees were located afterwards, one of which (`courk/gmini-linux`) is both
the same kernel version as the Invoke *and* BG2CDP-aware, making it the closer
source. See Question 1a.

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

Status: **REOPENED then RE-ANSWERED, with a correction.** The original answer
was recorded as ANSWERED on the basis of Steam Link alone. That was premature:
the corpus at the time contained only Steam Link and Acorn, and "not found in
the mirrored trees" was recorded as if it were "does not exist." Two further
vendor Berlin BSP trees have since been located (see Question 1a). One of them
carries a **BG2CDP** build target — the Invoke's exact chip — which Steam Link
does not. The Steam Link findings above remain correct and verbatim; they were
simply not the closest available source.

The BG2CD variant of the DHUB map is present verbatim in Steam Link; the
Invoke's BG2CDP map is the sibling of that file.

## Question 1a — Additional vendor Berlin BSP trees (BG2CDP and 3.10)

Located by GitHub code search on `arch/arm/mach-berlin` module paths, after
Question 1 had already been closed. Both trees were read via the GitHub API;
nothing was executed.

### Corpus addition

| Mirror | Kernel | mach-berlin modules present | Nature |
|---|---|---|---|
| `courk/gmini-linux` | **Linux 3.8.13** | `pe`, `amp`, `gpu`, `gpu3D`, `fastlogo`, `fastlogo_bg2cd` | Vendor Galois BSP, **same kernel version as the Invoke** |
| `fail0verflow/sony-psvr-linux` | **Linux 3.10.46** | `amp_core`, `gpu`, `ir`, `nfc`, `pwm`, `rf4ce`, `sm`, `tee`, `sd8777/8887/8897_mbtc`, `usb8797/8897_mbtc`, `tee` | Vendor Berlin BSP on a **3.10 LTS base**; Sony PlayStation VR breakout unit |

### Findings

| Repo | Path | What it establishes |
|---|---|---|
| courk/gmini-linux @ master | `arch/arm/mach-berlin/modules/pe/pe_driver.c` | **31 occurrences of `BERLIN_BG2CDP`.** Contains explicit BG2CDP conditional paths, e.g. `#if (BERLIN_CHIP_VERSION != BERLIN_BG2CD_A0 && BERLIN_CHIP_VERSION != BERLIN_BG2CDP)` at lines 674, 684, 739, 1157–1189, 1652, and `#if (BERLIN_CHIP_VERSION != BERLIN_BG2CDP)` at 692, 748, 758, 1478, 1523 |
| courk/gmini-linux @ master | `arch/arm/mach-berlin/modules/pe/Makefile` | Same `galois_pe` build (`pe_driver.o avio_dhub_drv.o pe_agent_driver.o`), `mv88de3100.mk`, `gsinc/$(FIRMWARE)` header selection, `-DBERLIN_SINGLE_CPU` |
| fail0verflow/sony-psvr-linux @ master | `arch/arm/mach-berlin/modules/amp_core/kernel/drv_aout.c` | 556-line AOUT driver on 3.10. Same core primitives as Steam Link's `pe_driver.c`: `AOUT_PATH_CMD_FIFO`, `p_ma_fifo`/`p_sa_fifo`/`p_spdif_fifo`/`p_hdmi_fifo`, `aout_start_cmd`, `AoutFifoGetKernelRdDMAInfo`, `AoutFifoKernelRdUpdate`, `AoutFifoCheckKernelFullness` |
| fail0verflow/sony-psvr-linux @ master | same file, lines 46–56, 104–116 | SoC-variant selection is a **compile-time conditional over the same channel-map symbols**: `avioDhubChMap_aio64b_MA0_R..MA3_R` for BG4_CD/BG4_CT, `avioDhubChMap_ag_MA0_R..MA3_R` otherwise |
| fail0verflow/sony-psvr-linux @ master | same file, line 147 | `avioDhubChMap_ag_PDM_MIC_ch1` — a **PDM microphone capture channel** on the `ag_` (BG2-family) map |

Verifiable excerpt (`drv_aout.c`, PSVR 3.10.46):

```c
static INT32 pri_audio_chanId[4] = {
#if ((BERLIN_CHIP_VERSION == BERLIN_BG4_CD) || (BERLIN_CHIP_VERSION == BERLIN_BG4_CT))
    avioDhubChMap_aio64b_MA0_R,
    avioDhubChMap_aio64b_MA1_R,
    avioDhubChMap_aio64b_MA2_R,
    avioDhubChMap_aio64b_MA3_R,
#else
    avioDhubChMap_ag_MA0_R,
    avioDhubChMap_ag_MA1_R,
    avioDhubChMap_ag_MA2_R,
    avioDhubChMap_ag_MA3_R,
#endif
};
```

Compare Steam Link 3.8 (`pe_driver.h`), which has the same four `ag_MA*_R`
entries **unconditionally**. The 3.10 tree did not replace the BG2-family map;
it added a newer BG4 map alongside it and selected between them at compile
time.

### Thesis — what the 3.8 → 3.10 transition actually cost Marvell

Derived by reading both trees. Labeled inference where it is inference.

1. **The audio core did not change.** The FIFO protocol, the kernel/user DMA
   ring, the `p_*_fifo` path pointers, the start/resume command flow, and the
   DHUB channel-map symbol names are common to the 3.8 and 3.10 drivers. This
   is the same driver lineage, not a rewrite. *(Observed.)*

2. **What changed is packaging, not mechanism.** The 3.8 trees carry AOUT
   inside `modules/pe/` (presentation engine, ~3,035 lines in `pe_driver.c`,
   mixing audio with video/VPP concerns). The 3.10 tree splits it into
   `modules/amp_core/kernel/` with discrete `drv_aout.c` (556 lines),
   `drv_vpp.c`, `drv_vpu.c`, `drv_avif.c`, `drv_msg.c`. The restructuring is a
   separation of concerns within the vendor codebase. *(Observed.)*

3. **The driver barely touches the kernel API.** Across `drv_aout.c` the only
   in-tree kernel API surface is a single `irqreturn_t` ISR signature. There
   are no `platform_*`, `of_*`, `dma_*`, `ioremap`, or `copy_*_user` calls in
   this file; DMA is handled through the vendor DHUB layer and its own
   `dma_info` structures (51 references). **The port surface across kernel
   versions is therefore very small for this file specifically.** The API
   churn a forward-port must absorb lives in the surrounding module glue —
   DHUB, shm, cc, the character-device and ioctl registration, and the build
   system — not in the audio logic. *(Observed for this file; inference for
   the module as a whole, which has not yet been read in full.)*

4. **Chip variance is expressed as compile-time conditionals over a stable
   symbol vocabulary.** Both generations select channel maps via
   `BERLIN_CHIP_VERSION` against `avioDhubChMap_*` names that persist across
   versions. A BG2CDP target is therefore a *configuration* of an existing
   code path rather than a new driver. *(Observed.)*

5. **`courk/gmini-linux` is the closest known source to the Invoke.** It is
   the same kernel version (3.8.13) *and* carries explicit `BERLIN_BG2CDP`
   conditionals, which Steam Link's BG2CD tree does not. For any question
   about what the Invoke's own audio/video path does, this tree should be
   consulted before Steam Link. *(Observed.)*

6. **A PDM microphone DHUB channel exists on the BG2-family map.**
   `avioDhubChMap_ag_PDM_MIC_ch1` appears in the 3.10 AOUT ISR. Steam Link's
   BG2CD `avioDhubChMap` header also lists `vip_MIC0_W..MIC3_W`. This is
   directly relevant to the unrecovered microphone contract in roadmap
   stage 4. *(Observed. Whether these channels correspond to the Invoke's
   physical mic array is **not established** and requires device evidence.)*

### Consequence for the reuse decision

The practical case for staying on 3.8.13 is **unchanged but better
understood**. It does not rest on "no newer vendor audio source exists" — that
claim was wrong and is retracted. It rests on:

- a working, hardware-verified `3.8.13-reinvoke-gcc49` that already produces
  audible output;
- a BG2CDP-aware 3.8.13 vendor tree (`courk/gmini-linux`) now available as a
  reference for the *current* kernel, with no port required;
- a 3.10 path whose benefit is packaging and LTS lineage rather than new
  capability, and whose cost is unquantified module-glue churn plus blind
  bring-up on hardware with no early console.

A 3.10 forward-port is therefore **viable and no longer speculative**, but it
remains an optional future project rather than a prerequisite. Its strongest
argument is not modernity; it is that `amp_core` is a cleaner starting point
than `pe` if the audio path ever needs substantial modification.

### Open items created by this finding

| # | Item | Status |
|---|---|---|
| 1a-1 | Read `courk/gmini-linux` BG2CDP conditionals in full and diff against the Invoke's own GPL `pe_driver.c` | not started |
| 1a-2 | Determine which BG2CDP code paths are *excluded* by those conditionals and why | not started |
| 1a-3 | Read `amp_core` module glue (ioctl/chardev/DHUB registration) to quantify real port cost | not started |
| 1a-4 | Establish whether `avioDhubChMap_ag_PDM_MIC_ch1` / `vip_MIC*_W` correspond to the Invoke mic array | requires device evidence |
| 1a-5 | Mirror both trees locally under the repository's storage policy | not started |

### Method note

This entry exists because a closed question was re-opened by a direct
challenge to an unverified assertion. The original ANSWERED status generalized
from an incomplete corpus. Recording the correction is required by the
governing rule: *a claim must be traceable to evidence, or it remains
explicitly unknown or hypothetical.* Absence of evidence in a partial mirror
set is not evidence of absence, and should be recorded as corpus scope rather
than as a finding.

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
