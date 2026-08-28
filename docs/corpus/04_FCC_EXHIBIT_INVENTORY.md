---
corpus_id: hki-hardware
title: FCC Exhibit Inventory for APIHKINVOKE
version: "0.1"
date: "2026-08-28"
status: evidentiary
format_goal: LLM-ingestion
fcc_id: APIHKINVOKE
grantee_code: API
product_code: HKINVOKE
---

# FCC Exhibit Inventory for APIHKINVOKE

## Purpose

This document records the FCC exhibit set for FCC ID `APIHKINVOKE` (Harman Kardon Invoke) that is now held locally, hash verified, and available as load-bearing evidence. Before this acquisition the corpus cited FCC internal photographs and the FCC Bluetooth test report as a proxy teardown, but no FCC artifact was actually stored. That gap is now closed.

Evidence classes follow `00_README.md`. The exhibit files themselves are regulatory primary evidence (`REG`). Direct visual readings taken from the internal photographs in this document are direct visual observation (`OBS-PHOTO`).

## Acquisition summary

The exhibit list was retrieved from the fccid.io mirror, which returned HTTP 200. The primary FCC source apps.fcc.gov returned HTTP 503 and was not used. Each exhibit page exposes a stable public URL of the form `https://fccid.io/APIHKINVOKE/<Type>/<Name>.pdf`, which redirects to a content addressed mirror URL `https://fccid.io/m/<sha256>.pdf`. The mirror filename equals the file SHA-256, so download integrity is self checking.

Twenty exhibit PDFs were downloaded. All returned HTTP 200 with MIME `application/pdf` and a valid `%PDF` header. Thirteen are byte unique. The remaining seven are byte identical duplicates that fccid.io lists because two application filings exist under the same FCC ID.

Totals.

| Metric | Value |
|---|---|
| Exhibit PDFs downloaded | 20 |
| Total bytes on disk | 48505793 (48.5 MB) |
| Unique files by SHA-256 | 13 |
| Unique bytes | 31183043 (31.2 MB) |
| HTTP failures | 0 |

The most important integrity result is that the downloaded Internal Photos PDF (document 3374744) has SHA-256 `bd9ad8dbda90b5ae76454a06545369b75b79b19265896388414e80b64d56ef61`, which exactly matches the hash the corpus previously only reported from the filing index. The proxy teardown evidence is now held and verified rather than merely cited.

Storage locations follow the project split. Binaries live outside git under the archive root at `originals/fcc/APIHKINVOKE/`. One small provenance sidecar JSON per exhibit lives in the git repo at `metadata/FCC-APIHKINVOKE-<slug>.json`. A captured HTML snapshot of the fccid.io index page is stored under the archive at `web-pages/APIHKINVOKE-fccid-index.html`.

## Exhibit table

Availability values. HELD means the artifact is downloaded and hash verified. DUPLICATE means the artifact is HELD but is byte identical to another row from the second application filing. UNAVAILABLE means the artifact was never publicly filed or is confidential.

| Exhibit | Type | FCC doc id | Date | Size | SHA-256 prefix | Availability |
|---|---|---|---|---|---|---|
| Internal Photos | Internal Photos | 3374744 | 2017-07-27 | 3.27 MB | bd9ad8dbda90 | HELD |
| Internal Photos (filing 2) | Internal Photos | 3374549 | 2017-07-27 | 3.27 MB | bd9ad8dbda90 | DUPLICATE |
| External Photos | External Photos | 3374547 | 2017-07-27 | 0.93 MB | 684bacf1d145 | HELD |
| External Photos (filing 2) | External Photos | 3374742 | 2017-07-27 | 0.93 MB | 684bacf1d145 | DUPLICATE |
| Label and Location | ID Label/Location Info | 3374817 | 2017-04-28 | 1.03 MB | 8672c9f755ac | HELD |
| Test Report 200801 BT 2.4 GHz | Test Report | 3374512 | 2017-04-28 | 5.65 MB | 83e7b0b1f354 | HELD |
| Test Report 200803 WLAN 2.4 GHz | Test Report | 3374820 | 2017-04-28 | 5.77 MB | ada569586684 | HELD |
| Test Report 200804 WLAN 5 GHz | Test Report | 3374792 | 2017-04-28 | 5.00 MB | a3c6b74d6196 | HELD |
| Test Report 200804 DFS 5 GHz | Test Report | 3374800 | 2017-04-28 | 1.29 MB | 28894686778e | HELD |
| RF Exposure 200805 | RF Exposure Info | 3374821 | 2017-04-28 | 0.18 MB | bf02d7a5344b | HELD |
| Test Setup Photos | Test Setup Photos | 3374511 | 2017-07-27 | 0.43 MB | 483be0de2cdc | HELD |
| Test Setup Photos (filing 2) | Test Setup Photos | 3374787 | 2017-07-27 | 0.43 MB | 483be0de2cdc | DUPLICATE |
| Test Setup Photos (filing 3) | Test Setup Photos | 3374818 | 2017-07-27 | 0.43 MB | 483be0de2cdc | DUPLICATE |
| Users Manual | Users Manual | 3374805 | 2017-07-27 | 5.58 MB | e6bda895d5ed | HELD |
| Users Manual (filing 2) | Users Manual | 3374825 | 2017-07-27 | 5.58 MB | e6bda895d5ed | DUPLICATE |
| Users Manual (filing 3) | Users Manual | 3374517 | 2017-07-27 | 5.58 MB | e6bda895d5ed | DUPLICATE |
| Authorization Letter | Cover Letter(s) | 3374739 | 2017-04-28 | 0.22 MB | 02d97c5f6e49 | HELD |
| Cover Letter | Cover Letter(s) | 3374545 | 2017-04-28 | 0.09 MB | 6b642cb371a5 | HELD |
| Confidentiality Letter | Cover Letter(s) | 3374546 | 2017-04-28 | 0.31 MB | 9f6988115d8d | HELD |
| Confidentiality Letter (filing 2) | Cover Letter(s) | 3374741 | 2017-04-28 | 0.31 MB | 9f6988115d8d | DUPLICATE |

## Exhibits never filed or confidential

The following exhibit types are commonly requested for a preservation project but are not present in the APIHKINVOKE public exhibit list. The confidentiality request letter (document 3374546) is consistent with these being withheld under short term or long term confidentiality. They are recorded as unavailable rather than invented.

| Exhibit | Availability | Reason |
|---|---|---|
| Schematics | UNAVAILABLE | Not in public exhibit list. Typically confidential. |
| Block diagram | UNAVAILABLE | Not in public exhibit list. Typically confidential. |
| Operational description | UNAVAILABLE | Not in public exhibit list. Typically confidential. |
| Parts list or BOM | UNAVAILABLE | Not in public exhibit list. Typically confidential. |
| Grant of Equipment Authorization PDF | UNAVAILABLE | Held on apps.fcc.gov, which returned HTTP 503. Grant data is corroborated by the captured fccid.io index snapshot only. |

## What each held exhibit establishes

Internal Photos, document 3374744. Nine pages of Invoke specific internal photographs. This is the proxy teardown. It establishes the multi PCB architecture, the removable compute daughterboard with two board to board connectors, the service and connector PCB carrying the Micro-USB and DC barrel jack, the key PCB, the top user interface and LED PCB, the two cabled antenna elements, and the external switch mode power supply internals. Class REG.

Test Report 200801, document 3374512. Part 15 Subpart C, section 15.247, 2.4 GHz. This is the Bluetooth 4.1 report the corpus cites as FCC-BT. It records the AC to DC adapter identity and its 19 V, 2.0 A output rating and the PIFA antenna gains. Class REG.

Test Report 200803, document 3374820. Part 15 Subpart C, section 15.247, 2.4 GHz 802.11b/g/n WLAN. Establishes the 2.4 GHz Wi-Fi certification. Class REG.

Test Report 200804 WLAN, document 3374792. 5 GHz U-NII bands, 802.11a/n/ac. Establishes the 5 GHz Wi-Fi certification including the 20, 40, and 80 MHz channel modes. Class REG.

Test Report 200804 DFS, document 3374800. Dynamic Frequency Selection, radar detection, for the 5 GHz U-NII bands. Class REG.

RF Exposure 200805, document 3374821. MPE and RF exposure evaluation for the intentional radiators. Class REG.

External Photos, document 3374547. External appearance of the finished product. Class REG.

Label and Location, document 3374817. FCC ID label artwork and its placement. Class REG.

Test Setup Photos, document 3374511. Photographs of the measurement setup. Supporting evidence for the test reports. Class REG.

Users Manual, document 3374805. End user manual. Class REG for the regulatory filing, and it corroborates manufacturer primary control and connector descriptions.

Authorization, Cover, and Confidentiality Letters, documents 3374739, 3374545, 3374546. Administrative filing letters. The confidentiality letter explains why the schematics, block diagram, and operational description are absent from the public set.

## Claims now backed by a held artifact

Before this acquisition the following claims cited FCC sources the project did not possess. They are now backed by a hash verified local artifact.

| Claim area | Prior citation | Now held |
|---|---|---|
| Multi PCB architecture and removable daughterboard with two board to board connectors | FCC-IP pages 4 and 5 | Yes, document 3374744 |
| Service and connector PCB with Micro-USB receptacle and DC barrel jack | FCC-IP page 7 | Yes, document 3374744 |
| Connector board silkscreen 40-HKTANA-CNB2G | FCC-IP page 7 | Yes, document 3374744 |
| Key PCB silkscreen 40-HKTANA-KYB2G and three switch mechanisms | FCC-IP page 8 | Yes, document 3374744 |
| Top PCB with 13 visible LED packages | FCC-IP page 6 | Yes, document 3374744 |
| Two cabled antenna elements | FCC-IP page 3 | Yes, document 3374744 |
| External power supply internals | FCC-IP page 9 | Yes, document 3374744 |
| Bluetooth 4.1, adapter 19 V and 2.0 A rating, PIFA antenna gains | FCC-BT page 6 | Yes, document 3374512 |
| 5 GHz certification with 20, 40, 80 MHz modes | FCC-INDEX | Corroborated by held Test Reports 3374792 and 3374800 |

## Claims still unheld or unresolved

The formal FCC Grant of Equipment Authorization PDF from apps.fcc.gov is not held because that host returned HTTP 503. Grant frequencies and bandwidth modes are corroborated by the held 5 GHz test reports and by the captured fccid.io index snapshot, but the primary grant PDF remains unheld. The schematics, block diagram, operational description, and parts list are not in the public exhibit set and remain unavailable.

## Direct visual observations from the internal photographs

The following observations were read directly from the internal photographs in document 3374744 at native embedded resolution, roughly 1047 by 699 pixels at 200 pixels per inch. Working render crops used for reading are stored outside git under the archive at `derived/fcc-render/APIHKINVOKE/`. These are new direct visual observations, class OBS-PHOTO. Where a marking is not legible, that is stated rather than guessed.

### Compute daughterboard and its connectors

The daughterboard is a small module carrying two long board to board connectors on opposite edges, one along the top edge and one along the bottom edge, each with pin numbering silkscreened up to about 70. This confirms the removable daughterboard and its two connector interface as a direct visual fact, not an inference. The board also carries several silkscreened test points including TP16, TP17, TP19, TP20, and TP21.

### Applications processor

The daughterboard bottom side carries a large ball grid array device with an unambiguous Marvell logo. The primary part number line is washed out by a diagonal glare streak in the photograph and is not reliably legible. A lot code near 1637, consistent with 2016 week 37, is visible. The Marvell identity of the processor is a direct observation. The exact part number is not legible from this photograph, so no specific Marvell part number is asserted here.

### DRAM markings

The daughterboard top side carries an SK Hynix package whose top marking reads `SKhynix` with the part line reading `H5TC4G63CFR` and a date or lot field near `635`. The H5TC4G63 family is a 4 gigabit DDR3 SDRAM, which is 512 megabytes in one package. One package is clearly readable. A second DRAM package may exist on the opposite side beneath thermal material and is not confirmed, so total memory capacity is not established. One character of the speed grade suffix is at the edge of legibility, so the family and 4 gigabit density are reported with higher confidence than the exact speed grade suffix.

### Wireless combo IC

The daughterboard bottom side carries a quad flat package with a Marvell logo whose part line begins with `88W8` and ends with the suffix `-NAA2`. The middle digits are not reliably legible. The `88W8` prefix identifies the Marvell Avastar wireless LAN and Bluetooth combo family. The presence of a Marvell 88W8 series combo is a direct observation. The exact model number is not asserted because the middle digits cannot be read with confidence.

### Micro-USB service port

The service and connector PCB, silkscreen `40-HKTANA-CNB2G`, carries a shielded Micro-USB receptacle and a separate DC barrel jack. The interconnect to the main board is a labeled inline connector whose silkscreen names the pins `1 USB_DM`, `2 USB_DP`, `3 GND_CN`, `4 USB_5V`, `5 GND_CN`, `6 DC_IN_19V`, `7 DC_IN_19V`, `8 MCU_3V3`, `9 LED_ORANGE`, and `10 LED_WHITE`. The presence of USB_DM and USB_DP shows the Micro-USB port is wired for USB data, not power only. This is a new direct observation that strengthens the external interface inventory. The only external connectors visible remain the Micro-USB and the DC power jack.

### Candidate UART or debug test pads

The main audio and control PCB carries a populated two by six through hole pin header at one board edge. Its silkscreen designator sits at the frame edge and is not clearly legible, and no pin function names such as TX, RX, or GND are printed next to it in the photograph. This is a candidate debug, UART, or JTAG header, but the photograph does not label its function, so it is recorded as a candidate only and not as a confirmed UART. It is an internal header that requires disassembly to reach, which is consistent with the earlier statement that no external UART is exposed on the finished product.

### Other main board devices

The main audio and control PCB carries one large quad flat package of roughly one hundred pins and one smaller quad flat package of roughly twenty eight pins. Both have laser etched markings that are not legible at the available resolution, so no part numbers are asserted for them. A finned heatsink sits over a further device that is presumed to be the audio power amplifier. That device is under the heatsink and is not visible, so the amplifier part number cannot be read from these photographs.

## Bibliography

### FCC-EXHIBITS
fccid.io mirror of FCC ID `APIHKINVOKE`, Harman International Industries.
Index URL: https://fccid.io/APIHKINVOKE
Role: exhibit list and per exhibit stable public URLs. Twenty exhibit PDFs downloaded and hash verified. Provenance sidecars in `metadata/FCC-APIHKINVOKE-*.json`.

### FCC-IP
SGS and Harman, Internal Photos, FCC document 3374744, nine pages.
URL: https://fccid.io/APIHKINVOKE/Internal-Photos/Internal-Photos-3374744.pdf
SHA-256: bd9ad8dbda90b5ae76454a06545369b75b79b19265896388414e80b64d56ef61

### FCC-BT
SGS-CSTC, test report SZEM170300200801, FCC document 3374512.
URL: https://fccid.io/APIHKINVOKE/Test-Report/Test-report-3374512.pdf
SHA-256: 83e7b0b1f354c24e2b6b3673619b918eabe44e906f7d02380c311ecda6f3baef
