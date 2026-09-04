# Source Retention Ranking — Custody Gap Axis

This ranking is based on **custody gap**: how likely an artifact survives **without** this archive.

Inputs used here are the verified facts provided on 2026-08-26:
- Software Heritage snapshots exist for: nest bootloader (2024-07-25), HKHacking git repo (2023-12-09), acorn_kernel (2024-06-18), acorn_uboot (2024-06-16), adb-sync (2026-08-05).
- GitHub fork counts: adb-sync 189, steamlink-sdk 108, kinomajs 68, acorn_kernel 2, acorn_uboot 1, HKHacking 3.
- Local mirror sizes: adb-sync 236K, bootloader 20M, HKHacking 26M, kinomajs 67M, acorn_uboot 79M, acorn_kernel 964M, steamlink-sdk 3.2G.
- Citation.zip has no Wayback capture and its Harman CDN currently returns 403 to automated requests.
- Chromecast/Nest Drive folder has no Wayback capture and is resourcekey-gated.
- Release assets, Discussions, and wikis are not preserved by Software Heritage snapshots of git history.

## A) Survival likelihood without us (sorted most-likely-to-keep -> least)

| Artifact/source | Survival likelihood without us | Custody-gap priority to retain locally | Evidence basis |
|---|---:|---:|---|
| google/adb-sync git mirror | 5/5 | 1/5 | SWH snapshot exists (2026-08-05), 189 forks, small (236K). |
| Valve steamlink-sdk git mirror | 5/5 | 1/5 | Very high fork count (108), broad community footprint despite large size (3.2G). |
| KinomaJS git mirror | 4/5 | 2/5 | 68 forks; community redundancy present. |
| Google/Nest bootloader git mirror | 4/5 | 2/5 | SWH snapshot exists (2024-07-25); specialized but preserved. |
| HKHacking git mirror (repo history only) | 4/5 | 2/5 | SWH snapshot exists (2023-12-09); 3 forks. |
| Acorn kernel git mirror | 3/5 | 3/5 | SWH snapshot exists (2024-06-18) but only 2 forks; medium fragility. |
| Acorn U-Boot git mirror | 3/5 | 3/5 | SWH snapshot exists (2024-06-16) but only 1 fork; medium fragility. |
| Google Drive discovery-only pointer | 2/5 | 4/5 | Resourcekey-gated and no Wayback capture; current record is metadata-only. |
| Citation.zip (Harman CDN) | 1/5 | 5/5 | No Wayback capture; CDN blocks automated retrieval (403 now). |
| HKHacking release assets (`Harman.Kardon.INVOKE.Flashing.zip`, `Harman.Kardon.INVOKE.Driver.OTA2.zip`, `83_IMAGE`) | 1/5 | 5/5 | Release binaries are outside git snapshots and not archived by SWH. |
| HKHacking Discussions / wiki layer | 1/5 | 5/5 | Non-git GitHub content is not preserved by git mirrors/SWH; must be captured separately. |

## B) Highest custody-gap items (retain first)

1. HKHacking release assets (P0-004a/b/c)  
2. HKHacking Discussions/wiki pages  
3. Citation.zip from Harman CDN  
4. Google Drive gated discovery layer  
5. Low-fork legacy git mirrors (acorn_kernel, acorn_uboot)

## Local capture status for top custody-gap items

- Release assets captured under `originals/harman/invoke/` in the archive root via acquisition sidecars `metadata/P0-004a.json`, `P0-004b.json`, `P0-004c.json`.
- Discussion page captured as rendered HTML under `web-pages/` in the archive root.
- Releases API JSON captured under `metadata/` to preserve non-git release metadata.

## Build-time upstream sources

These are toolchain inputs rather than research evidence, so they sit outside
the fragility ranking above. All are widely mirrored and at low custody risk;
they are archived to make the runtime build reproducible rather than to
preserve endangered material. Canonical URLs, checksums, and build flags are
recorded in [metadata/P1-045.json](../../metadata/P1-045.json).

| Source | Version | Licence | Provenance |
|---|---|---|---|
| bluez-alsa | 4.0.0 | MIT | sha256 only; upstream publishes no signature |
| BlueZ | 5.55 | GPL-2.0-or-later | Good GPG signature, maintainer key |
| SBC | 2.0 | GPL-2.0-or-later | Good GPG signature, maintainer key |
| D-Bus | 1.12.20 | AFL-2.1 OR GPL-2.0-or-later | Good GPG signature, maintainer key |

Stored at Tier 2 under `sources/upstream/` in the archive root. Every digest
matches the value already recorded in P1-045, and none of these tarballs is
committed to Git.

Signing keys observed:

- BlueZ and SBC — Marcel Holtmann,
  `E932 D120 BC2A EC44 4E55 8F01 06CA 9F5D 1DCF 2659`
- D-Bus — Simon McVittie,
  `DA98 F25C 0871 C49A 59EA FF2C 4DE8 FF2A 63C7 CC90`

### Recorded URL drift

The D-Bus 1.12.20 `.tar.xz` URL in P1-045 now returns 404. Upstream publishes
that release only as `.tar.gz`, whose digest matches the recorded sha256
exactly, so the hash always described the gzip artifact and no content
changed. P1-045 has been corrected. This is a useful reminder that recorded
URLs decay faster than recorded digests, which is why both are kept.
