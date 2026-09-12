---
title: Invoke and Berlin source catalogue
description: Acquisition status, authoritative records and unresolved firmware and source leads
ms.date: 2026-09-12
---

## Record authority

The source catalogue distinguishes acquired objects from discovery leads for
Invoke and the Marvell Berlin family. Status describes the recorded capture,
not present upstream availability or compatibility with the speaker.

* [Acquisition definitions](../../tools/acquisitions.json) identify sources,
  destinations and capture scope.
* [Metadata sidecars](../../metadata/) bind downloaded bytes or Git revisions
  to their provenance. Use their complete hashes, sizes and retrieval dates
  rather than filenames as identities.
* [Firmware reference](../firmware-reference.md) records extracted formats and
  generation differences.
* [Storage policy](storage-policy.md) defines retention and publication.

Public upstream packages are distinct from private build outputs and device
captures. Full firmware, extracted trees and the private archive are not
provided by this repository. The custom image is not published; the recorded
2026-09-12 release check found no reInvoke releases.

## Recorded acquisition results

The three Invoke originals were acquired on 2026-08-26 from
[coggy9/HKHacking releases](https://github.com/coggy9/HKHacking/releases).
The sidecars carry exact asset URLs and complete-object digests.

| Record                                 | Retained object                        | Status and use                               |
| -------------------------------------- | -------------------------------------- | -------------------------------------------- |
| [P0-004a](../../metadata/P0-004a.json) | `Harman.Kardon.INVOKE.Flashing.zip`    | Downloaded; stock 11.1842 and recovery kit   |
| [P0-004b](../../metadata/P0-004b.json) | `Harman.Kardon.INVOKE.Driver.OTA2.zip` | Downloaded; final 12.2134 full-image USB kit |
| [P0-004c](../../metadata/P0-004c.json) | Standalone StockRoot `83_IMAGE`        | Downloaded; rooted 11.1842 variant           |

The [release API snapshot](../../metadata/HKHacking-releases-api-20260826T115746Z.json)
preserves release metadata, not the assets themselves.
[Discussion 3](https://github.com/coggy9/HKHacking/discussions/3) is the original
community acquisition context; a rendered HTML copy was retained privately.
A mirror of the main repository does not capture release assets or every
Discussion/wiki page.

### Other P0 records

| Record                               | Source                                                                                                                              | Recorded result                                                  |
| ------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------- |
| [P0-001](../../metadata/P0-001.json) | Harman `Citation.zip`, source URL in sidecar                                                                                        | HTTP 200 and 6,556 downloaded bytes; not proof of a complete BSP |
| [P0-002](../../metadata/P0-002.json) | Official Chromecast/Nest source-folder pointer                                                                                      | `DISCOVERY_ONLY`; no folder payload acquired                     |
| [P0-003](../../metadata/P0-003.json) | [Google/Nest bootloader](https://nest-open-source.googlesource.com/manifest_repos/bootloader/)                                      | Git mirror; revision and refs in sidecar                         |
| P0-005                               | [Archived Cortana SDK notices](https://web.archive.org/web/20231203010301/https://www.harmankardon.com/cortana-sdk-opensource.html) | HTML acquired; third-party notices, not SDK source               |

P0-001 also has a historical HTTP 403 observation. The successful small
download supersedes neither that observation nor the need to establish package
contents. Its SHA-256 and response metadata are in the sidecar.

P0-002 exposed the folder title `Chromecast Opensource Code` but no child-file
listing to the unauthenticated request. The official
[Google provenance page](https://support.google.com/product-documentation/answer/10525328?hl=en)
is useful even while the resourcekey-gated payload remains unresolved.

P0-005 has no dedicated public sidecar. The captured page SHA-256 is
`6b2e25ae48c4e3456c1952a2ff13d8013cf978b68f94d7295a741e30aac7696b`.
The private locator is
`web-pages/harman-cortana-sdk-opensource-20231203010301.html`.
It records Microsoft-authored notices for components such as Expat, curl,
OpenSSL and Opus, not an acquired Cortana implementation.

## Mirrored donor sources

The following sidecars record Git captures from the acquisition stage.
Related SoCs and code names identify comparison material, not drop-in Invoke
drivers or proof of matching peripherals.

| Record                               | Source                                                                                     | Retained scope and engineering use                                 |
| ------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------ |
| [P0-003](../../metadata/P0-003.json) | [Nest bootloader](https://nest-open-source.googlesource.com/manifest_repos/bootloader/)    | Mirror; Berlin bootloader layout and tools                         |
| [P1-020](../../metadata/P1-020.json) | [Valve steamlink-sdk](https://github.com/ValveSoftware/steamlink-sdk)                      | Mirror; BG2CD/88DE3005 predecessor BSP, toolchain and build layout |
| [P1-030](../../metadata/P1-030.json) | [KinomaJS](https://github.com/Kinoma/kinomajs)                                             | Mirror; platform and historical update references                  |
| [P1-031](../../metadata/P1-031.json) | [Acorn kernel](https://github.com/kinoma/acorn_kernel)                                     | Mirror; sibling kernel comparison                                  |
| [P1-032](../../metadata/P1-032.json) | [Acorn U-Boot](https://github.com/kinoma/acorn_uboot)                                      | Mirror; sibling bootloader comparison                              |
| [P2-001](../../metadata/P2-001.json) | [HKHacking](https://github.com/coggy9/HKHacking)                                           | Main-repository mirror; community recovery evidence                |
| [P2-002](../../metadata/P2-002.json) | [google/adb-sync](https://github.com/google/adb-sync)                                      | Mirror; historical filesystem-pull tooling                         |
| [P2-003](../../metadata/P2-003.json) | [hk-invoke-arm-flasher](https://github.com/jryruegas92/hk-invoke-arm-flasher)              | Pinned mirror; host recovery implementation                        |
| [P2-004](../../metadata/P2-004.json) | [hk-invoke-opensource-speaker](https://github.com/Aristoddle/hk-invoke-opensource-speaker) | Provisional mirror; claims require independent corroboration       |

P0-003's required historical commit is
`836ad32e08388e0e4ce8d03fe4f14d2c3ea8ba13`, distinct from its captured HEAD.
The pinned
[Berlin bootloader tree](https://nest-open-source.googlesource.com/manifest_repos/bootloader/+/836ad32e08388e0e4ce8d03fe4f14d2c3ea8ba13/berlin_tools/bootloader/)
and its `bootloader.lds` are the intended layout references.

For evidence qualification rather than acquisition status, use the
[sibling-source cross-index](../corpus/05_SIBLING_SOURCE_CROSSINDEX.md),
including its [community-project review](../corpus/05_SIBLING_SOURCE_CROSSINDEX.md#community-projects).

### Linux Berlin capture discrepancy

[P1-040](../../metadata/P1-040.json) still says `DISCOVERY_ONLY`.
The later acquisition definition instead records a shallow, blobless sparse
checkout of roughly 44 MB from the
[Linux Berlin maintainer tree](https://kernel.googlesource.com/pub/scm/linux/kernel/git/jszhang/linux-berlin/).
Neither record establishes the full mirror proposed by the original seed.

The later scope note reports Berlin2CD/BG2CD content but no BG2CDP board file,
`sound/soc/berlin`, or `drivers/soc/berlin` in that upstream tree. The absent
subtrees matter when comparing mainline support with the vendor kernel; a
sparse-path request is not evidence that all requested paths exist.

## Build and comparison inputs

Later records cover sources actually used in kernel, audio and provisioning
work. Exact archive/member checksums and compiler identities remain in JSON.

| Record                               | Source or input                                                                                                                           | Captured scope                                                      |
| ------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------- |
| [P1-041](../../metadata/P1-041.json) | [Invoke-kernel.tar](https://archive.org/details/invoke-kernel)                                                                            | Acquired and extracted GPLv2 Linux 3.8.13 source                    |
| [P1-042](../../metadata/P1-042.json) | Official Google Android NDK r10e                                                                                                          | Archive acquired; ARM GCC 4.9 prebuilt subtree extracted            |
| [P1-043](../../metadata/P1-043.json) | [AOSP system/bt](https://android.googlesource.com/platform/system/bt) at `android-6.0.1_r81`                                              | Selected source files and notices, not a full Android tree          |
| [P1-044](../../metadata/P1-044.json) | Ubuntu Go 1.18.1 packages                                                                                                                 | Retained compiler/source packages and provisioning build identities |
| [P1-045](../../metadata/P1-045.json) | BlueZ 5.55, bluez-alsa 4.0.0, SBC 2.0, D-Bus 1.12.20                                                                                      | Pinned source archives, licences, signatures and build flags        |
| [P2-005](../../metadata/P2-005.json) | [courk/gmini-linux PCM source](https://github.com/courk/gmini-linux/blob/764b617b647c91fe969332ceb690282ecdad4e0c/sound/soc/berlin/pcm.c) | One pinned file snapshot; comparison with Invoke ASoC integration   |

P1-041 records verification against Internet Archive MD5/SHA-1 plus a computed
SHA-256. P1-042 records an independent SHA-1 from Ubuntu's NDK installer
package. These are integrity/provenance checks, not endorsements of executing
unreviewed archive content.

P1-045 reports verified upstream signatures for BlueZ, SBC and D-Bus; bluez-alsa
has a recorded SHA-256 but no upstream detached signature. It also corrects
the D-Bus 1.12.20 URL from `.tar.xz` to `.tar.gz`: the digest did not change.
This is recorded endpoint drift, not a fresh download check.

### Runtime evidence records

P1-045 through P1-051 also bind historical builds and tests. Their status
strings are checkpoint-specific, not the current product status:

* [P1-046](../../metadata/P1-046.json) records RAM network lifecycle.
* [P1-047](../../metadata/P1-047.json) records software-bus capture.
* [P1-048](../../metadata/P1-048.json) records the owned MCU service.
* [P1-049](../../metadata/P1-049.json) records the v9 RAM candidate checkpoint.
* [P1-050](../../metadata/P1-050.json) records microphone DMA/capture evidence.
* [P1-051](../../metadata/P1-051.json) records the v10 composition checkpoint.

Later results belong in the [product contract](../current-product-contract.md)
and [journal](../journal.md). RAM validation does not establish native
acceptance, and a reproducible composition from pinned held inputs is not a
complete clean-clone build.

## Unresolved source leads

These identifiers remain useful for correlating old references. They are not
an acquisition queue, and no completed capture is inferred from their presence.

| Identifier    | Target                                                             | Evidence limit                                                                                                                   |
| ------------- | ------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------- |
| P1-001..011   | Google/Nest kernel, SDK, toolchain, drivers and media repositories | Proposed repositories under the [Nest index](https://nest-open-source.googlesource.com/manifest_repos/); no blanket mirror claim |
| P1-033        | Entire public Kinoma organization                                  | Organization-wide capture was proposed, not established                                                                          |
| DISCOVERY-001 | Kinoma HD firmware, recovery image or GPL/SDK package              | No package established                                                                                                           |
| DISCOVERY-002 | Kinoma Studio/Code installers                                      | Historical installer lead only                                                                                                   |
| DISCOVERY-003 | `chromecast_sdk_oss.tgz`, `chromecast_oss.tgz`                     | Historical 1.56 `Kernel Bootloader SDK` path lead; not a verified Drive hierarchy                                                |
| DISCOVERY-004 | Historical Harman Demandware OSS assets                            | Captured HTML does not preserve linked packages                                                                                  |
| DISCOVERY-005 | Standalone BG2CDP/88DE3006 BSP                                     | No standalone package established                                                                                                |

The useful search vocabulary is `BG2CDP`, `88DE3006`, `berlin2cdp`, `Galois`,
`ARMADA 1500 Mini Plus`, and related board aliases. A matching filename or
string is a discovery lead, not a compatibility result. Hardware conclusions
belong in the [canonical baseline](../corpus/01_CANONICAL_HARDWARE_BASELINE.md).

## Record maintenance

Keep originals byte-for-byte and extract into separate trees. Downloads need
the stable source URL, retrieval time, byte count and complete digest; Git
captures need a revision and explicit full, shallow or sparse scope.
Redact expiring signed queries and keep credentials out of provenance records.

Distinguish `DOWNLOADED`, `MIRRORED` and file snapshots from `DISCOVERY_ONLY`.
Seed-era `LIVE_CONFIRMED` means a URL responded at that checkpoint, not that
its payload was preserved. When records disagree, document the discrepancy
instead of silently promoting a discovery status to an acquired input.

Archive-relative names in JSON refer to private retained storage, not public
download paths. Generated images and captures require their own manifests;
the acquisition sidecars are not a complete build or backup inventory.
See [backup verification](storage-policy.md#backup-and-restore) before relying
on a restored input set.
