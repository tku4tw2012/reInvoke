---
title: Invoke hardware baseline
description: Product specifications, measured unit geometry, and unresolved hardware boundaries
ms.date: 2026-09-12
---

## Scope and sources

Published specifications apply to the product, photographs to the FCC sample,
and project measurements to the observed unit. Source abbreviations below link
to their originals. [Evidence conventions](00_README.md#evidence-conventions)
cover these distinctions; the [native NAND guide](../native-nand-platform.md)
owns current candidate status.

## Product and power

Manufacturer specifications, supplemented by FCC Bluetooth report p.6:

| Property            | Value                                 | Source        |
| ------------------- | ------------------------------------- | ------------- |
| Product/model       | Harman Kardon Invoke / `HKINVOKE`     | [SPEC], [FCC] |
| FCC ID              | `APIHKINVOKE`                         | [FCC]         |
| Dimensions          | 107 mm diameter x 242 mm height       | [SPEC]        |
| Published weight    | 1 kg / 2.3 lb                         | [SPEC]        |
| Rated audio power   | 40 W                                  | [SPEC]        |
| Frequency response  | 60 Hz to 20 kHz, -6 dB                | [SPEC]        |
| Power input         | 19 VDC, 2 A                           | [SPEC], [BT]  |
| FCC-tested adapter  | `DT19V-2C-DC`                         | [BT]          |
| Adapter mains input | AC 100-240 V, 50/60 Hz, 1.5 A maximum | [BT]          |
| Adapter output      | DC 19 V, 2.0 A                        | [BT]          |

The nominal adapter output is `19 V x 2.0 A = 38 W`, not measured consumption.
Harman does not specify the convention behind its 40 W audio rating; neither
figure establishes per-channel amplifier power.

## Audio and microphones

Harman specifies three 45 mm woofers and three 13 mm dome tweeters [SPEC],
plus two passive radiators [NEWS]. Driver impedance, crossover, amplifier
channel allocation, and physical codec/DSP identities remain unresolved.

The seven-microphone SONIQUE system is manufacturer-documented [OM, p.8][OM].
Harman advertises beamforming, echo cancellation, and noise reduction [NEWS].
Project [RAM capture](../microphone-capture.md) exposes two 48 kHz `S32_LE`
ALSA channels. This is not seven-channel raw capture or proof that advertised
algorithms are active. Physical microphone signaling and ADC placement remain
unknown; software mute observations do not prove electrical disconnection.

The recovered [DSP SPI protocol](../emulation/dsp-boundary.md) carries control
messages and a volatile program download, not PCM playback. The separate
[speaker path](../emulation/owned-speaker-control.md) covers ALSA/BlueALSA.
[MCU control](../emulation/mcu-boundary.md) covers indicators, mute controls,
and expander operations without identifying every fitted IC.

## Controls and physical assemblies

The owner's manual documents touch, a volume ring, status illumination,
microphone on/off, Bluetooth pairing, a reset pin, DC power, and a Micro-USB
factory-service connector [OM, pp.7-9][OM].

The internal photographs establish the following certification-sample layout:

| Assembly                | Observation                                        | Photo page |
| ----------------------- | -------------------------------------------------- | ---------- |
| Main electronics board  | Audio/control assembly with cabled antennas        | 3-4        |
| Compute daughterboard   | Removable board on two long board-to-board links   | 4-5        |
| Top UI board            | 12 perimeter LED packages, one center; rotary part | 6          |
| Connector/service board | Micro-USB and DC jack; `40-HKTANA-CNB2G` marking   | 7          |
| Lower key board         | Three switch mechanisms; `40-HKTANA-KYB2G` marking | 8          |
| External power supply   | Switch-mode power components                       | 9          |

The [FCC inventory](04_FCC_EXHIBIT_INVENTORY.md) retains package readings and
connector labels. PCB markings are not established service part numbers.
Thirteen visible LED packages do not establish individually addressed RGB
wiring; the rotary component's exact part and signaling are unknown.

## Compute and memory

[HKHacking][HKHACK] identifies the daughterboard as Marvell `88DE3006 / BG2CDP`.
Invoke firmware independently contains BG2CDP/`berlin2cdp-dongle` metadata;
see [image analysis](../firmware-reference.md#image-formats).
The FCC processor photograph shows a Marvell logo but an obscured part line.

[Linux's silicon reference][LINUX] identifies 88DE3006 as Armada 1500 Mini Plus,
design name BG2CDP, with two ARM Cortex-A7 cores. That is the silicon property
conditional on the Invoke-specific identification, not a measured CPU clock.

[U-Boot measurements](../uboot-access.md) report one DRAM bank at
`0x00000000`, size `0x20000000`: 512 MiB on the project unit.
The FCC sample's tentative `SKhynix H5TC4G63CFR` reading is retained in
[DRAM markings](04_FCC_EXHIBIT_INVENTORY.md#dram-markings).
Its incomplete suffix and unverified sample equivalence do not establish the
exact installed DRAM part or technology on this unit.

## Wireless subsystem

Published and regulatory capabilities:

| Property             | Value                                              | Source    |
| -------------------- | -------------------------------------------------- | --------- |
| Wi-Fi                | 802.11 b/g/n/ac, 2.4 and 5 GHz                     | [SPEC]    |
| 2.4 GHz grant ranges | Includes 2412-2462 MHz and 2422-2452 MHz           | [FCC]     |
| Channel widths       | Relevant 2.4 GHz modes: 20/40; 5 GHz: 20/40/80 MHz | [FCC]     |
| Bluetooth            | 4.1 Dual Mode; cited test covers Classic           | [BT], p.6 |
| Bluetooth radio      | 2402-2480 MHz, 79 channels, FHSS and AFH           | [BT], p.6 |
| Modulations          | GFSK, pi/4-DQPSK, 8DPSK                            | [BT], p.6 |
| Antennas             | PIFA; gains 2.10 dBi and 2.29 dBi                  | [BT], p.6 |

Two cabled antenna elements are visible in FCC photo p.3. Neither their count
nor the 80 MHz certification establishes simultaneous 2x2 MIMO operation.

[RAM Linux](../native-ram-platform.md) enumerated SDIO functions `02df:9135`,
`02df:9136`, and `02df:9137`. Invoke drivers map `9135` to SD8887 Wi-Fi and
`9136` to SD8887 Bluetooth; both loaded controller firmware on the unit.
Wi-Fi loaded LS9AD calibration and completed a scan. This establishes the
88W8887/SD8887 family and SDIO host bus, not a complete package marking.
These identifiers are SDIO IDs, not USB VID/PID pairs.

## NAND and filesystem geometry

Direct U-Boot and RAM Linux observations establish:

| Measurement              | Observed value                  |
| ------------------------ | ------------------------------- |
| NAND ID                  | `98 DA 90 15 76 16`             |
| Manufacturer             | Toshiba, from the read ID       |
| Capacity                 | 268,435,456 bytes (256 MiB)     |
| Page size                | 2 KiB                           |
| Erase size               | 131,072 bytes (128 KiB)         |
| Complete logical capture | 268,435,456 ECC-processed bytes |

The capture used a kernel-enforced read-only MTD node. Logical main data and
exposed OOB records are not a programmer-grade raw restore image. The exact
full NAND part number and cell type remain unresolved.

The older [HKHacking shell report][HKHACK] supplied this partition map.
It is a third-party runtime layout, not the project's native mount table:

```text
dev:    size     erasesize name
mtd0:  00020000 00020000 "block0"
mtd1:  00100000 00020000 "pre-bootloader"
mtd2:  00160000 00020000 "env"
mtd3:  00080000 00020000 "aligned"
mtd4:  00200000 00020000 "post-bootloader"
mtd5:  00200000 00020000 "post-bootloader"
mtd6:  01000000 00020000 "factory_setting"
mtd7:  01000000 00020000 "tz_en"
mtd8:  01000000 00020000 "tz_en-B"
mtd9:  01000000 00020000 "bootimgs"
mtd10: 01000000 00020000 "bootimgs-B"
mtd11: 0a900000 00020000 "rootfs"
mtd12: 00000000 00000000 "app"
mtd13: 00000000 00000000 "localstorage"
mtd14: 00000000 00000000 "BDlocalstorage"
mtd15: 00000000 00000000 "bbt"
mtd16: 10000000 00020000 "mv_nand"
```

`mv_nand` is the aggregate device, not additional capacity. A generic 512 MiB
flashing-bundle partition string is not this unit's geometry. Paired names
suggest redundancy without establishing Android-style A/B or rollback rules.

The [Aristoddle compact map](05_SIBLING_SOURCE_CROSSINDEX.md#community-projects)
instead places an 8 MiB `kernel` at `0x00a20000` and a 105 MiB `rootfs` at
`0x01220000`. The 2026-09-02 pre-installation readback places a high-entropy
kernel container at `0x00a20000` and active SquashFS at `0x02920000`, inside
that proposed rootfs region. This corroborates geometry, not every label.
The active filesystem identifies as `Barracuda_libre-12.2050.3`.

Community reports include read-only YAFFS2 mounts and SquashFS structures.
Record filesystem type per image/partition; neither describes the entire NAND.
Image generations and offsets are indexed in the
[firmware reference](../firmware-reference.md#firmware-generations).

## Boot and recovery boundary

Harman's final update notes specify release `12.2314.0`, dated 2021-09-08,
distributed through a USB flashing tool with a Windows driver [FINAL].
The project independently reached a USB U-Boot console and host-loaded Linux
with a custom PID 1 and root ADB gadget before native installation.

Successful recovery after particular experiments does not prove recovery
from arbitrary boot-chain corruption. The native image retains byte-identical
vendor bootloader, TrustZone, and encrypted kernel payloads, but installation
erased and reprogrammed the whole good-block set. Retained bytes are not
untouched boot-chain regions. See the [write decision](../nand-write-decision.md)
and [U-Boot access](../uboot-access.md) for the established recovery boundary.

## Open questions

Software interface work has resolved control and capture behavior without
requiring a complete board schematic. Remaining questions are:

| Area               | Unresolved detail                                                    |
| ------------------ | -------------------------------------------------------------------- |
| Compute and memory | CPU clock; installed DRAM type/part; full NAND part and cell type    |
| Module boundary    | Connector pinout, rails, sequencing, clocks, reset and enables       |
| Boot/storage       | Complete slot selection, rollback and arbitrary-corruption recovery  |
| Radio              | Full package marking and simultaneous spatial-stream topology        |
| Audio              | DSP, amplifier, codec/ADC/DAC identities; routing and impedances     |
| Microphones        | Part, electrical interface, ADC location, seven raw channels         |
| UI                 | LED driver/protocol, touch controller, encoder part                  |
| Debug              | UART voltage/pinout, JTAG/SWD status and test-pad functions          |

WM8904 appears in an ACast reference device tree, not a verified Invoke
component identification. Daughterboard modularity motivated an August 2026
replacement-compute proposal; electrical compatibility remains unproved.
Actual native kernel, PID 1, and mounts remain unread via shell. RAM microphone
privacy/capture and supervision results still require native acceptance.

## Sources

Manufacturer: [specification sheet][SPEC], [owner's manual][OM],
[launch announcement][NEWS], and [final update notes][FINAL].
Regulatory: [filing index][FCC], [Bluetooth report][BT], and
[acquired exhibit metadata](04_FCC_EXHIBIT_INVENTORY.md).
Platform: [HKHacking Discussion #3][HKHACK] and [Linux Marvell reference][LINUX].

[SPEC]: https://www.harmankardon.com/on/demandware.static/-/Sites-masterCatalog_Harman/default/dwf63bd00a/pdfs/HK_Invoke_Spec_Sheet_English.pdf
[OM]: https://support.harmankardon.com/on/demandware.static/-/Sites-masterCatalog_Harman/default/dwdac694e8/pdfs/Harman%20Kardon%20Invoke%20Owners%20Manual.pdf
[NEWS]: https://news.harman.com/releases/harman-reveals-the-harman-kardon-invokeTM-intelligent-speaker-with-cortana-from-microsoft
[FINAL]: https://support.harmanaudio.com/howto/invoke-final-software-update-release-notes-us/000018514.html
[FCC]: https://fccid.io/APIHKINVOKE
[BT]: https://fccid.io/APIHKINVOKE/Test-Report/Test-report-3374512.pdf
[HKHACK]: https://github.com/coggy9/HKHacking/discussions/3
[LINUX]: https://docs.kernel.org/5.19/arm/marvell.html
