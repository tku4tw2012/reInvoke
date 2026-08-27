# Invoke / Marvell Berlin Artifact Acquisition Manifest

**Generated:** 2026-08-25  
**Purpose:** seed manifest for automated preservation of Harman Kardon Invoke / Marvell 88DE3006 (BG2CDP) firmware, BSPs, GPL drops, SDKs, donor trees, and reverse-engineering material.

## Automation policy

For every acquired object: preserve the original byte-for-byte; record source URL, final URL, UTC retrieval time, SHA-256, SHA-1, size, MIME/type, and HTTP metadata; extract only into a separate tree; never overwrite an older copy; mirror full Git history when practical.

**Do not automatically execute or flash any downloaded artifact.** Acquisition automation is authorized only to download, mirror, archive, hash, extract, index, and document.

Suggested tree:

```text
reinvoke/                 # Git repository (small, durable catalogue)
├── docs/
├── metadata/             # provenance sidecars, SHA-256 per artifact
└── tools/

reinvoke-archive/         # bulk payloads, never in Git; mirrored to cold storage
├── originals/{harman,google,marvell,kinoma,valve,community}/
├── git-mirrors/
├── extracted/
└── web-pages/
```

`destination:` values below are relative to the archive root, not the repository.
The operational manifest is `tools/acquisitions.json`; this document is the seed
specification and rationale.

# P0 — acquire immediately

## P0-001 — Harman Citation GPL/Open-Source package

```yaml
id: P0-001
priority: P0
status: LIVE_CONFIRMED
kind: binary_archive
expected_filename: Citation.zip
download_url: "https://www.harmankardon.com/on/demandware.static/-/Sites-masterCatalog_Harman/default/dwb3ecd0ef/downloads/Citation.zip"
provenance_page: "https://www.harmankardon.com/opensource.html"
destination: originals/harman/citation/Citation.zip
why: "Same Harman ecosystem; Citation-family material is a high-value donor for Invoke research."
```

After extraction search for:

```text
88DE3006 BG2CDP bg2cdp berlin2 berlin2cdp berlin2cdp-dongle
Galois galois Marvell mrvl u-boot uboot linux kernel defconfig
.dts .dtsi NAND mtd 88W8887 toolchain buildroot busybox
```

## P0-002 — Official Google Chromecast / Nest open-source archive

```yaml
id: P0-002
priority: P0
status: LIVE_CONFIRMED
kind: cloud_folder
folder_url: "https://drive.google.com/drive/folders/1jdISUGQQr10kX_MeJ_tWoCLjv1CznoWH?resourcekey=0-DDxaPDf4jphp5EtgzWFQ5g"
provenance_page: "https://support.google.com/product-documentation/answer/10525328?hl=en"
destination: originals/google/chromecast-nest-oss/
action: ENUMERATE_AND_DOWNLOAD_ALL
```

Locate with highest priority:

```text
chromecast_sdk_oss.tgz
chromecast_oss.tgz
*1.56*
*Kernel*Bootloader*SDK*
*Chromecast*Audio*
*Chromecast*2*
*Google*Home*
*Google*Home*Mini*
```

Historical high-value lead:

```text
1.56/
└── Kernel Bootloader SDK/
    └── .../
        └── chromecast_sdk_oss.tgz
```

Do not assume the current Drive hierarchy still matches the historical hierarchy. Search recursively by filename.

## P0-003 — Google/Nest Marvell Berlin bootloader source

```yaml
id: P0-003
priority: P0
status: LIVE_CONFIRMED
kind: git_repository
clone_url: "https://nest-open-source.googlesource.com/manifest_repos/bootloader"
browse_url: "https://nest-open-source.googlesource.com/manifest_repos/bootloader/"
destination: git-mirrors/google-nest/bootloader.git
action: GIT_MIRROR
required_commit: "836ad32e08388e0e4ce8d03fe4f14d2c3ea8ba13"
```

Preserve these exact historical paths too:

```text
https://nest-open-source.googlesource.com/manifest_repos/bootloader/+/836ad32e08388e0e4ce8d03fe4f14d2c3ea8ba13/berlin_tools/bootloader/
https://nest-open-source.googlesource.com/manifest_repos/bootloader/+/836ad32e08388e0e4ce8d03fe4f14d2c3ea8ba13/berlin_tools/bootloader/bootloader.lds
```

## P0-004 — Harman Kardon Invoke flashing bundle

```yaml
id: P0-004
priority: P0
status: DISCOVERY_REQUIRED
kind: firmware_bundle
expected_filename: Harman.Kardon.INVOKE.Flashing.zip
known_discussion: "https://github.com/coggy9/HKHacking/discussions/3"
destination: originals/harman/invoke/
action: DISCOVER_ARCHIVE_OR_MIRROR
```

**Do not invent a direct download URL.** Search GitHub attachments/user-content URLs, old Harman pages, archive.org, Wayback, cached pages, mirrors, forums, and preserved user uploads.

Expected internal objects:

```text
70_IMAGE
79_IMAGE
79_IMAGE.examples
81_IMAGE
82_IMAGE
83_IMAGE
99_IMAGE
Mrvl_WinUSB*
usb_boot*
l2nand*
mload*
```

Also archive the complete discussion page above.

## P0-005 — historical Harman Invoke OSS page

```yaml
id: P0-005
priority: P0
status: ACQUIRED
kind: historical_web_target
historical_url: "https://www.harmankardon.com/cortana-sdk-opensource.html"
current_parent: "https://www.harmankardon.com/opensource.html"
destination: reinvoke-archive/web-pages/harman-cortana-sdk-opensource-20231203010301.html
action: WAYBACK_CDX_ENUMERATE_ALL_CAPTURES_AND_LINKS
```

Resolved: the live URL still redirects (`REDIRECTS_TODAY` was accurate for
direct access), but the Wayback Machine CDX index
(`http://web.archive.org/cdx/search/cdx?url=harmankardon.com/cortana-sdk-opensource.html&output=json`)
has two `200`-status captures (2023-03-29, 2023-12-03). The 2023-12-03
capture was retrieved and archived; SHA-256
`6b2e25ae48c4e3456c1952a2ff13d8013cf978b68f94d7295a741e30aac7696b`. It is a
Microsoft-authored third-party notices file for the Cortana SDK (Expat,
RapidJSON, Parson, zlib, curl, Breakpad, OpenSSL, Opus, and related
components), not source code. See
`docs/corpus/02_CLAIM_EVIDENCE_LEDGER.md` §"Cortana SDK third-party notices
(P0-005)" for the full claim breakdown.

Parse every historical capture for:

```text
invoke Invoke INVOKE cortana Cortana opensource open-source
source GPL SDK firmware .zip .tgz .tar.gz demandware.static downloads
```

For every discovered asset URL, archive the asset itself plus all available Wayback captures.

# P1 — mirror donor source trees

Google/Nest repository index:

```text
https://nest-open-source.googlesource.com/manifest_repos/
```

Mirror all of these:

```yaml
repositories:
  - id: P1-001
    url: "https://nest-open-source.googlesource.com/manifest_repos/kernel"
    dest: git-mirrors/google-nest/kernel.git
  - id: P1-002
    url: "https://nest-open-source.googlesource.com/manifest_repos/sdk"
    dest: git-mirrors/google-nest/sdk.git
  - id: P1-003
    url: "https://nest-open-source.googlesource.com/manifest_repos/gnu_toolchain"
    dest: git-mirrors/google-nest/gnu_toolchain.git
  - id: P1-004
    url: "https://nest-open-source.googlesource.com/manifest_repos/toolchain"
    dest: git-mirrors/google-nest/toolchain.git
  - id: P1-005
    url: "https://nest-open-source.googlesource.com/manifest_repos/u-boot"
    dest: git-mirrors/google-nest/u-boot.git
  - id: P1-006
    url: "https://nest-open-source.googlesource.com/manifest_repos/drivers"
    dest: git-mirrors/google-nest/drivers.git
  - id: P1-007
    url: "https://nest-open-source.googlesource.com/manifest_repos/media_modules"
    dest: git-mirrors/google-nest/media_modules.git
  - id: P1-008
    url: "https://nest-open-source.googlesource.com/manifest_repos/mtd-utils"
    dest: git-mirrors/google-nest/mtd-utils.git
  - id: P1-009
    url: "https://nest-open-source.googlesource.com/manifest_repos/alsa-lib"
    dest: git-mirrors/google-nest/alsa-lib.git
  - id: P1-010
    url: "https://nest-open-source.googlesource.com/manifest_repos/alsa-utils"
    dest: git-mirrors/google-nest/alsa-utils.git
  - id: P1-011
    url: "https://nest-open-source.googlesource.com/manifest_repos/ffmpeg"
    dest: git-mirrors/google-nest/ffmpeg.git
```

## P1-020 — Valve Steam Link SDK

```yaml
id: P1-020
priority: P1
status: LIVE_CONFIRMED
kind: git_repository
clone_url: "https://github.com/ValveSoftware/steamlink-sdk.git"
browse_url: "https://github.com/ValveSoftware/steamlink-sdk"
archive_url: "https://github.com/ValveSoftware/steamlink-sdk/archive/refs/heads/master.tar.gz"
destination: git-mirrors/valve/steamlink-sdk.git
action: GIT_MIRROR
notes: "88DE3005/BG2CD predecessor; valuable Marvell BSP/toolchain/build-layout donor."
```

Preserve/index:

```text
kernel/ rootfs/ toolchain/ scripts/ external/ examples/
MARVELL_SDK_PATH bg2cd bg2cd_penguin_mlc_defconfig Berlin
Galois Marvell Vivante NAND mtd uImage
```

# P1 — Kinoma preservation

## P1-030 — KinomaJS

```yaml
id: P1-030
priority: P1
status: LIVE_CONFIRMED
kind: git_repository
clone_url: "https://github.com/Kinoma/kinomajs.git"
browse_url: "https://github.com/Kinoma/kinomajs"
archive_url: "https://github.com/Kinoma/kinomajs/archive/refs/heads/master.tar.gz"
destination: git-mirrors/kinoma/kinomajs.git
action: GIT_MIRROR
```

Search **full history**, not only HEAD, for:

```text
Kinoma HD KinomaHD kinomahd kinoma-hd BG2CDP 88DE3006
Berlin Marvell firmware update upgrade downgrade manifest OTA
release beta xsedit KPL KPR
```

Extract and preserve every historical firmware-update URL or manifest URL found.

## P1-031 — Kinoma Acorn kernel

```yaml
id: P1-031
clone_url: "https://github.com/kinoma/acorn_kernel.git"
browse_url: "https://github.com/kinoma/acorn_kernel"
archive_url: "https://github.com/kinoma/acorn_kernel/archive/refs/heads/master.tar.gz"
destination: git-mirrors/kinoma/acorn_kernel.git
action: GIT_MIRROR
```

## P1-032 — Kinoma Acorn U-Boot

```yaml
id: P1-032
clone_url: "https://github.com/kinoma/acorn_uboot.git"
browse_url: "https://github.com/kinoma/acorn_uboot"
archive_url: "https://github.com/kinoma/acorn_uboot/archive/refs/heads/master.tar.gz"
destination: git-mirrors/kinoma/acorn_uboot.git
action: GIT_MIRROR
```

## P1-033 — entire public Kinoma GitHub organization

```yaml
id: P1-033
url: "https://github.com/Kinoma"
action: ENUMERATE_ALL_PUBLIC_REPOSITORIES_AND_GIT_MIRROR
destination: git-mirrors/kinoma/
```

Record the repository inventory, default branch, HEAD commit, and last update time.

# P1 — Linux Berlin maintainer tree

```yaml
id: P1-040
priority: P1
status: LIVE_CONFIRMED
kind: git_repository
clone_url: "https://kernel.googlesource.com/pub/scm/linux/kernel/git/jszhang/linux-berlin"
browse_url: "https://kernel.googlesource.com/pub/scm/linux/kernel/git/jszhang/linux-berlin/"
destination: git-mirrors/linux/linux-berlin.git
action: GIT_MIRROR
```

Search history for:

```text
88DE3006 BG2CDP berlin2cdp berlin2 Marvell
ARMADA 1500 Mini Plus chromecast kinoma
```

# P2 — community preservation

## P2-001 — HKHacking

```yaml
id: P2-001
priority: P2
status: LIVE_CONFIRMED
kind: git_repository_and_web_content
clone_url: "https://github.com/coggy9/HKHacking.git"
browse_url: "https://github.com/coggy9/HKHacking"
destination: git-mirrors/community/HKHacking.git
action:
  - GIT_MIRROR
  - ARCHIVE_ISSUES
  - ARCHIVE_DISCUSSIONS
  - ARCHIVE_RELEASES
  - ARCHIVE_RELEASE_ASSETS
  - ARCHIVE_LINKED_USER_CONTENT
```

Critical discussion:

```text
https://github.com/coggy9/HKHacking/discussions/3
```

Index:

```text
Harman.Kardon.INVOKE.Flashing.zip Mrvl_WinUSB 88DE3006 BG2CDP
berlin2cdp-dongle 79_IMAGE.examples 81_IMAGE 82_IMAGE 83_IMAGE
99_IMAGE mload l2nand GCastSDK anchovy galois tz_en reboot_usb.sh
```

## P2-002 — google/adb-sync

```yaml
id: P2-002
clone_url: "https://github.com/google/adb-sync.git"
browse_url: "https://github.com/google/adb-sync"
destination: git-mirrors/google/adb-sync.git
action: GIT_MIRROR
why: "Referenced by Invoke investigators while pulling mounted flash contents."
```

# Archival discovery jobs

## DISCOVERY-001 — Kinoma HD firmware / recovery / GPL / SDK

```yaml
id: DISCOVERY-001
priority: P0
status: NOT_FOUND_YET
target: Kinoma HD
destination: originals/kinoma/hd/discovered/
search_terms:
  - '"Kinoma HD" firmware'
  - '"Kinoma HD" recovery'
  - '"Kinoma HD" image'
  - '"Kinoma HD" SDK'
  - '"Kinoma HD" GPL'
  - '"Kinoma HD" source'
  - '"Kinoma HD" download'
  - '"kinomahd"'
  - '"kinoma-hd"'
  - '"88DE3006" Kinoma'
  - '"BG2CDP" Kinoma'
  - '"berlin2cdp" Kinoma'
file_patterns:
  - "*.zip"
  - "*.tgz"
  - "*.tar.gz"
  - "*.img"
  - "*.bin"
  - "*.uImage"
  - "*.ubi"
  - "*.squashfs"
```

Search archive.org, Wayback/CDX, GitHub, GitLab, Bitbucket, SourceForge, historical Kinoma/Marvell pages, developer forums, FCC exhibits, FTP indexes, and download mirrors.

Seed domain hypotheses, verify before treating as historical facts:

```text
kinoma.com
developer.kinoma.com
forum.kinoma.com
downloads.kinoma.com
marvell.com
```

## DISCOVERY-002 — Kinoma Studio / Kinoma Code installers

```yaml
id: DISCOVERY-002
priority: P1
historical_lead: "https://kinoma.com/studio"
destination: originals/kinoma/tools/
search_terms:
  - '"Kinoma Studio" download'
  - '"Kinoma Studio 4.4"'
  - '"Kinoma Code" installer'
  - '"Kinoma Studio" dmg'
  - '"Kinoma Studio" exe'
file_patterns: ["*.dmg","*.pkg","*.exe","*.msi","*.zip"]
```

Preserve installers; never execute automatically.

## DISCOVERY-003 — exact Chromecast SDK bundles

```yaml
id: DISCOVERY-003
priority: P0
official_folder: "https://drive.google.com/drive/folders/1jdISUGQQr10kX_MeJ_tWoCLjv1CznoWH?resourcekey=0-DDxaPDf4jphp5EtgzWFQ5g"
targets:
  - chromecast_sdk_oss.tgz
  - chromecast_oss.tgz
fallback_queries:
  - '"chromecast_sdk_oss.tgz"'
  - '"chromecast_oss.tgz" "1.56"'
  - '"combined-sdk-kernel-bootloader"'
destination: originals/google/chromecast-1.56/
```

Preserve all byte-distinct copies and provenance.

## DISCOVERY-004 — Harman Demandware OSS assets

```yaml
id: DISCOVERY-004
priority: P0
seed_pages:
  - "https://www.harmankardon.com/opensource.html"
  - "https://www.harmankardon.com/cortana-sdk-opensource.html"
known_live_asset:
  - "https://www.harmankardon.com/on/demandware.static/-/Sites-masterCatalog_Harman/default/dwb3ecd0ef/downloads/Citation.zip"
destination: originals/harman/discovered/
action: "Enumerate historical captures and extract demandware.static/download links."
```

Search historical HTML for:

```text
demandware.static downloads opensource source GPL cortana invoke citation
.zip .tgz .tar.gz
```

## DISCOVERY-005 — standalone Marvell BG2CDP / 88DE3006 BSP

```yaml
id: DISCOVERY-005
priority: P0
status: NOT_FOUND_AS_STANDALONE_PACKAGE
destination: originals/marvell/bg2cdp/
search_terms:
  - '"BG2CDP" BSP'
  - '"BG2CDP" SDK'
  - '"88DE3006" SDK'
  - '"88DE3006" BSP'
  - '"berlin2cdp" SDK'
  - '"berlin2cdp" BSP'
  - '"ARMADA 1500 Mini Plus" SDK'
  - '"ARMADA 1500 Mini Plus" BSP'
  - '"Marvell Berlin" SDK'
  - '"Marvell Berlin" BSP'
  - '"Galois" "Marvell Berlin"'
  - '"bg2cdp-dongle"'
file_patterns: ["*.tar.gz","*.tgz","*.zip","*.7z"]
```

Search public package indexes, old FTP indexes, GitHub forks, Google/Nest history, OEM GPL drops, Harman packages, Chromecast mirrors, and preservation sites.

# Provenance pages to archive

```yaml
provenance_targets:
  - "https://www.harmankardon.com/opensource.html"
  - "https://www.harmankardon.com/cortana-sdk-opensource.html"
  - "https://support.google.com/product-documentation/answer/10525328?hl=en"
  - "https://nest-open-source.googlesource.com/manifest_repos/"
  - "https://github.com/coggy9/HKHacking/discussions/3"
  - "https://github.com/ValveSoftware/steamlink-sdk"
  - "https://github.com/Kinoma/kinomajs"
  - "https://kernel.googlesource.com/pub/scm/linux/kernel/git/jszhang/linux-berlin/"
```

# Post-download indexing

Recursively index these tokens across extracted archives and all Git history where feasible:

```text
88DE3006 88DE3005 BG2CDP BG2CD bg2cdp bg2cd
berlin2cdp berlin2cdp-dongle berlin ARMADA 1500 Marvell
Galois galois anchovy joplin mushroom chromecast GCastSDK
88W8887 Mrvl_WinUSB l2nand mload sign_image uImage U-Boot
NAND mtd mtdblock squashfs yaffs ubi ubifs factory_setting
tz_en bootimgs rootfs Vivante DRM PDM I2S Cortana Invoke Citation Kinoma
```

Index at least these extensions/types:

```text
.c .h .S .lds .dts .dtsi .config defconfig .mk Makefile
.sh .py .pl .xml .json .ini .conf .txt .md .pdf
.bin .img .elf .axf .uImage .ubi .squashfs .tgz .tar.gz .zip
```

# Immediate execution order

```text
01  P0-001        Download Harman Citation.zip
02  P0-002        Enumerate/download official Google Chromecast/Nest OSS folder
03  DISCOVERY-003 Locate chromecast_sdk_oss.tgz and chromecast_oss.tgz
04  P0-003        Mirror Google/Nest bootloader repository
05  P0-004        Hunt Harman.Kardon.INVOKE.Flashing.zip
06  P0-005        Crawl historical cortana-sdk-opensource.html
07  DISCOVERY-004 Enumerate historical Harman Demandware OSS assets
08  P1-001..011   Mirror selected Google/Nest repositories
09  P1-020        Mirror Valve Steam Link SDK
10  P1-030..033   Mirror Kinoma repositories / org
11  P1-040        Mirror Linux Berlin maintainer tree
12  P2-001        Mirror/archive HKHacking + discussions/assets
13  DISCOVERY-001 Deep Kinoma HD archival hunt
14  DISCOVERY-002 Hunt Kinoma Studio/Code installers
15  DISCOVERY-005 Hunt standalone BG2CDP BSP/SDK
16  INDEX          Build recursive term/file index
17  HASH           Generate hashes/provenance/deduplication report
```

# Suggested metadata sidecar

```json
{
  "source_url": "",
  "final_url": "",
  "retrieved_utc": "",
  "filename": "",
  "size_bytes": 0,
  "sha256": "",
  "sha1": "",
  "mime": "",
  "http_etag": "",
  "http_last_modified": "",
  "notes": ""
}
```

Git metadata:

```json
{
  "clone_url": "",
  "retrieved_utc": "",
  "default_branch": "",
  "head_commit": "",
  "refs_count": 0,
  "mirror_path": "",
  "notes": ""
}
```

# Confidence labels

```text
LIVE_CONFIRMED       URL/repository currently responds and is identifiable.
HISTORICAL_CONFIRMED Artifact/path is documented but current binary endpoint is not confirmed.
DISCOVERY_REQUIRED   Artifact is known or strongly suspected but must be located.
INFERENCE            Plausible lead; do not present as established fact.
NOT_FOUND_YET        Search target only.
```

Do not upgrade `DISCOVERY_REQUIRED` or `NOT_FOUND_YET` without recording the exact retrieved URL and content hash.

# End
