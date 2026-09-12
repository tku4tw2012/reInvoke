---
title: FCC exhibits for APIHKINVOKE
description: Acquired exhibit inventory and qualified visual observations of the FCC sample
ms.date: 2026-09-12
---

## Acquisition and integrity

Twenty public PDFs were acquired from the [APIHKINVOKE filing index][index]
on 2026-08-28. All returned HTTP 200, `application/pdf`, and a `%PDF` header.
Thirteen are byte-unique: 48,505,793 total acquired bytes and 31,183,043 unique
bytes. Seven additional exhibit IDs duplicate other PDFs.

The mirror redirected named URLs to content-addressed `/m/<sha256>.pdf`
locations whose filenames matched the downloaded hashes. This checks mirror
consistency, not independent authentication against the primary FCC service,
which returned HTTP 503. Public metadata below records full hashes and URLs;
private PDFs reside under `originals/fcc/APIHKINVOKE/`.

The nine-page Internal Photos PDF `3374744` has SHA-256
`bd9ad8dbda90b5ae76454a06545369b75b79b19265896388414e80b64d56ef61`.
Visual observations concern that certification sample, not an inspection of
the project's unit.

## Exhibit inventory

Each linked ID opens its public acquisition record. Duplicate IDs in one row
have identical bytes; use the sidecars for exact sizes, dates, and checksums.

| Exhibit                | Primary ID | Byte-identical listings | Useful evidence                      |
| ---------------------- | ---------- | ----------------------- | ------------------------------------ |
| Internal photos        | [3374744]  | [3374549]               | Boards, modules, connectors, supply  |
| External photos        | [3374547]  | [3374742]               | Enclosure and controls               |
| Label/location         | [3374817]  | None                    | FCC identity and label position      |
| Bluetooth report       | [3374512]  | None                    | 200801: Classic radio, adapter, PIFA |
| WLAN 2.4 GHz report    | [3374820]  | None                    | 200803: 802.11b/g/n                  |
| WLAN 5 GHz report      | [3374792]  | None                    | 200804: a/n/ac, 20/40/80 MHz         |
| DFS report             | [3374800]  | None                    | 200804: 5 GHz radar detection        |
| RF exposure            | [3374821]  | None                    | 200805: MPE evaluation               |
| Test-setup photos      | [3374511]  | [3374787], [3374818]    | Certification test arrangements      |
| User manual            | [3374805]  | [3374825], [3374517]    | Controls and service connector       |
| Authorization          | [3374739]  | None                    | Administrative letter                |
| Cover letter           | [3374545]  | None                    | Administrative letter                |
| Confidentiality        | [3374546]  | [3374741]               | Withholding of technical exhibits    |

Schematics, block diagram, operational description, and BOM were unavailable
in the acquired public set. Their absence does not prove they were never
filed. The formal grant PDF was not acquired; the held reports and index
snapshot support the cited certification data.

## Board and package observations

The retained review used embedded images around 1047 x 699 pixels at 200 ppi;
private crops are under `derived/fcc-render/APIHKINVOKE/`. Package readings
below have not been independently confirmed from a second image source.

| Location             | Visible detail and interpretation limit                             |
| -------------------- | ------------------------------------------------------------------- |
| Daughterboard, p.5   | Two long connectors, numbering near 70; exact pin count unproved    |
| Daughterboard pads   | `TP16`, `TP17`, `TP19`, `TP20`, `TP21`; functions unknown           |
| Processor, p.5       | Marvell logo; glare obscures part line; `1637` is a possible code   |
| Radio, p.5           | Marvell, prefix `88W8`, suffix `-NAA2`; middle digits unclear       |
| Main board, pp.3-4   | Large roughly 100-pin QFP and smaller roughly 28-pin QFP unreadable |
| Heatsink, main board | Covered device; amplifier role is a hypothesis                      |
| Internal header      | Populated 2x6 through-hole header; no legible TX/RX/GND assignments |
| Top board, p.6       | 12 peripheral LED packages plus one central; rotary component       |
| Key board, p.8       | Three switches and `40-HKTANA-KYB2G` marking                        |
| Antennas, p.3        | Two cabled elements, not proof of spatial-stream topology           |
| Supply, p.9          | External switch-mode supply internals                               |

### DRAM markings

The tentative transcription is `SKhynix H5TC4G63CFR`, with a field near `635`
and incomplete speed-grade suffix. If correctly identified, the `H5TC4G63`
family is 4 Gibit DDR3, or 512 MiB per package. A possible opposite-side
package under thermal material was not confirmed. This is not a full memory
census or a verified part/type on the project unit; its independent
[512 MiB U-Boot measurement](01_CANONICAL_HARDWARE_BASELINE.md#compute-and-memory)
does not make the photograph more legible.

### Micro-USB service port

Photo p.7 shows the `40-HKTANA-CNB2G` board with Micro-USB and a DC barrel jack.
Its interconnect labels were transcribed as:

| Pin  | Label        | Pin  | Label        |
| ---- | ------------ | ---- | ------------ |
| 1    | `USB_DM`     | 6    | `DC_IN_19V`  |
| 2    | `USB_DP`     | 7    | `DC_IN_19V`  |
| 3    | `GND_CN`     | 8    | `MCU_3V3`    |
| 4    | `USB_5V`     | 9    | `LED_ORANGE` |
| 5    | `GND_CN`     | 10   | `LED_WHITE`  |

These are silkscreen labels, not measured continuity or verified rail voltages.
They support a data-capable service connector. No UART/debug assignment follows
from them or from the internal 2x6 header. For measured behavior, use
[U-Boot access](../uboot-access.md); product/radio ratings are in the
[hardware baseline](01_CANONICAL_HARDWARE_BASELINE.md).

[index]: https://fccid.io/APIHKINVOKE
[3374744]: ../../metadata/FCC-APIHKINVOKE-internal-photos-3374744.json
[3374549]: ../../metadata/FCC-APIHKINVOKE-internal-photos-3374549.json
[3374547]: ../../metadata/FCC-APIHKINVOKE-external-photos-3374547.json
[3374742]: ../../metadata/FCC-APIHKINVOKE-external-photos-3374742.json
[3374817]: ../../metadata/FCC-APIHKINVOKE-label-3374817.json
[3374512]: ../../metadata/FCC-APIHKINVOKE-test-report-bt-2g4-3374512.json
[3374820]: ../../metadata/FCC-APIHKINVOKE-test-report-wlan-2g4-3374820.json
[3374792]: ../../metadata/FCC-APIHKINVOKE-test-report-wlan-5g-3374792.json
[3374800]: ../../metadata/FCC-APIHKINVOKE-test-report-dfs-5g-3374800.json
[3374821]: ../../metadata/FCC-APIHKINVOKE-rf-exposure-3374821.json
[3374511]: ../../metadata/FCC-APIHKINVOKE-test-setup-photos-3374511.json
[3374787]: ../../metadata/FCC-APIHKINVOKE-test-setup-photos-3374787.json
[3374818]: ../../metadata/FCC-APIHKINVOKE-test-setup-photos-3374818.json
[3374805]: ../../metadata/FCC-APIHKINVOKE-user-manual-3374805.json
[3374825]: ../../metadata/FCC-APIHKINVOKE-user-manual-3374825.json
[3374517]: ../../metadata/FCC-APIHKINVOKE-user-manual-3374517.json
[3374739]: ../../metadata/FCC-APIHKINVOKE-authorization-letter-3374739.json
[3374545]: ../../metadata/FCC-APIHKINVOKE-cover-letter-3374545.json
[3374546]: ../../metadata/FCC-APIHKINVOKE-confidentiality-letter-3374546.json
[3374741]: ../../metadata/FCC-APIHKINVOKE-confidentiality-letter-3374741.json
