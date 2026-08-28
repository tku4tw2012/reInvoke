# Project Plan and Handoff

Current state, established facts, and next steps. This file exists so work can resume
in a fresh session — or from GitHub on the web — without the original conversation.

**Last updated:** 2026-08-26

---

## 1. Current state

### Phases complete

| Phase | Status |
|---|---|
| Acquisition — locate and retrieve artifacts | **Done** |
| Storage architecture — repo / archive / cold storage split | **Done** |
| Publication — firmware mirrored with attribution | **Done** |
| Analysis — unpack and understand the firmware | **Done** |
| Control-plane emulation — device userland runs off-device | **Done** — see [control-plane-emulation.md](docs/emulation/control-plane-emulation.md) |
| Evidence closure — FCC exhibits, OTA2, sibling cross-index | **Done** |
| Hardware validation — donor device(s) available | **Ready to start** — procedure in [no-disassembly-observation-procedure.md](docs/no-disassembly-observation-procedure.md) |

### The finding that reframes the project

Harman's final firmware, `Barracuda_libre-12.2134.0` in the OTA2 bundle, is an
end-of-life conversion. Cortana, the Cortana harness, Spotify, and the Skype
call library are removed. `oobe-ui` and a `wifi-blocker` service are added.

Harman shipped a version of this device that is a local Bluetooth speaker with
the cloud assistant taken out. The project's stated end goal, repurposing
completeness, may therefore be partly reachable on stock software rather than
by replacing it. Confirming which firmware a physical unit carries is now the
highest-value observation, which is why it leads the hardware procedure.

### The control plane is solved

The device's service bus is WAMP over MsgPack, routed by `bonefish`, an
open-source router that ships in the rootfs. Both the router and its client
services have been run on an x86 host under `qemu-user` in a rootless sandbox.
A third-party client joined the bus and successfully called
`com.harman.musicMuteToggle`, which changed real service state.

This means the audio control surface can be exercised, and its message shapes
recovered, without a physical unit and without any risk to one.

### Where things live

```text
~/<workspace>/
├── reinvoke/           1.5 MB   this Git repository
└── reinvoke-archive/   4.9 GB   bulk payloads, NOT under Git control
    ├── originals/      569 MB   firmware, byte-for-byte as retrieved
    ├── git-mirrors/    4.3 GB   bare mirrors of donor source trees
    ├── extracted/               unpacked material, incl. binaries kept out of the repo
    └── web-pages/               captured HTML (GitHub Discussions etc.)
```

The two directories must remain **siblings**: `tools/acquire.py` derives the archive
location from its own path. Override with `--archive-root` or `$REINVOKE_ARCHIVE`.

### Three-tier storage

| Tier | Contents | Location |
|---|---|---|
| 1 | Docs, metadata, hashes, extracted text layer | This repository (~1.5 MB) |
| 2 | Firmware bundles (569 MB) | [GitHub Releases](../../releases/tag/invoke-firmware-mirror) |
| 3 | Full working set incl. Git mirrors (4.9 GB) | Azure Blob, `<resource-group>` / `<storage-account>` / `archive`, westus2, Cool, LRS |

Azure access is via Microsoft Entra ID; no keys are stored anywhere. Cost is
approximately $0.58/year. Local, Azure, and the published release have been verified
byte-identical by SHA-256.

---

## 2. Established hardware facts

Derived from `docs/bundle-contents/`, all traceable to preserved artifacts.

### NAND layout — from `marvell_flash_tool/gen-cmd.sh` (596 bytes)

```text
mtdparts=mv_nand:
  128K(block0)          1M(pre-bootloader)    1408K(env)          512K(aligned)
  2M(post-bootloader)   2M(post-bootloader)   16M(factory_setting)
  16M(tz_en)            16M(tz_en-B)          16M(bootimgs)       16M(bootimgs-B)
  192M(rootfs)          152M(app)             16M(localstorage)   64M(BDlocalstorage)
  1M(bbt)
```

- Partitions sum to **exactly 512 MB**, establishing NAND size by derivation.
- Several regions are **A/B paired** (`post-bootloader` ×2, `tz_en`/`tz_en-B`,
  `bootimgs`/`bootimgs-B`), indicating failsafe update capability. Worth confirming
  against the OTA bundle.
- `tz_en` implies a TrustZone/secure-world image.

### Boot parameters — same file

```text
console=ttyS0,115200   init=/bin/sh   root=/dev/ram   initrd=0x08000000
```

A serial console at 115200 baud and a RAM-disk recovery path with a shell as init.

### USB identity — from `Mrvl_WinUSB_Driver_040114/Mrvl_WinUSB.inf`

`VID_1286` (Marvell) with `PID_8100` / `PID_8101`, plus `VID_8086` with
`PID_e001` / `PID_c001` / `PID_d001`. These identify the SoC in USB boot / recovery mode.

### The three `83_IMAGE` variants

Two are exactly **107,934,810 bytes** but differ in **83,800,608 bytes (77.64%)**.
A third, newer rootfs was recovered from the OTA2 bundle.

| Variant | Build tag | SHA-256 |
|---|---|---|
| `StockRoot` release asset | `Barracuda_rooted_libre-11.1842.0` | `f59d0a56f5d3d4cc90b146e2433ec32da36239e6c4373813d57fe92e19326cc7` |
| Inside `Flashing.zip` | `Barracuda_libre-11.1842.0` | `90a4f54d7c92f55ea20f6d63f89caae5f7738b62dec4913bded0fd7816ec9a1c` |
| Inside `OTA2.zip` | `Barracuda_libre-12.2134.0` | `b2e12178...` (see OTA2 analysis) |

The first two differ by a SquashFS rebuild plus 11 deliberate file changes; the
`StockRoot` copy is a rooted variant. That question is closed.

The third is a genuinely newer 2021 build on the stock lineage, and it is the
most consequential artifact in the archive. See
[ota2-analysis.md](docs/bundle-contents/invoke-ota2/ota2-analysis.md).

---

## 3. Next steps

### Phase 3 — analysis (the main work)

1. **Identify `83_IMAGE`'s format.** **Done:** Marvell/Berlin container with two
   gzip-compressed SquashFS v4 members.
2. **Extract the root filesystem** into `reinvoke-archive/extracted/`, never into the repo.
   **Done.**
3. **Diff the two variants.** **Done:** identical tree shape; 11 regular-file content
   changes documented in `FINDINGS.md`.
4. **Examine `81`/`82`/`99_IMAGE`.** **Done:** 81 is the ARM uImage kernel, 82 is the
   gzip/cpio initrd, and 99 is an older LS9 SquashFS/component image.
5. **Write `FINDINGS.md`** — **Done.**
6. **Update `docs/corpus/02_CLAIM_EVIDENCE_LEDGER.md`** — **Done.**
7. **Done:** boot/update correlation linked initrd startup, kernel/device-tree strings,
   LS9 radio firmware/configuration, and RedBend OTA paths.
8. **Done:** static slot-selection search found OTA targets and U-Boot environment
   references but no direct active/inactive selector; the unresolved status is
   recorded. LS9 radio lineage was compared, and a concise text/configuration
   layer was added under `docs/bundle-contents/invoke-flashing/phase3-analysis.md`.
9. **Done:** the two discovery-only sources were checked without acquiring
   artifacts. **Done:** the Phase 3 documentation set (`FINDINGS.md`, corpus
   ledger/hash updates, `phase3-analysis.md`, `runtime-interface-inventory.md`,
   `revival-roadmap.md`, `azure-restore-runbook.md`) was committed as the
   clean preservation milestone.

**Phase 3 is fully closed.** All static, firmware-only analysis achievable
without a physical unit has been completed and committed.

### Phase 4 — hardware validation (current phase)

A donor device is now physically in hand and will not be opened. The operator
procedure is [no-disassembly-observation-procedure.md](docs/no-disassembly-observation-procedure.md),
which supersedes the ordering in `hardware-validation-plan.md` for units that
stay closed. The end goal remains **repurposing completeness (L2)**, not full
schematic completeness.

The FCC exhibit set for `APIHKINVOKE` is now held locally under
`originals/fcc/` in the archive, with provenance sidecars in `metadata/`. Until
this session those claims cited evidence the project did not possess.

**The two observations that matter most:**

1. Does the unit pair over Bluetooth and play audio? Harman's final firmware
   is already a Bluetooth-speaker build, so a working unit may substantially
   satisfy the end goal with no intervention.
2. Does the Micro-USB service port ever expose `1286:8100` or `1286:8101`?
   That is a hard gate on every RAM-boot path. If it never appears without
   opening the case, that avenue is closed and the result should be recorded.

**Remaining steps, once those two are answered:**

1. **Before powering on:** assign the unit a sample ID, photograph enclosure
   labels and every board (including both sides of the daughterboard and its
   connector area), record visible IC markings/board revisions, and confirm
   the power adapter rating. Keep the known-good firmware image + SHA-256
   manifest offline — do not flash it.
2. **No-disassembly observations first** (useful regardless of whether the
   unit will also be opened): measure adapter output unloaded and at the
   device input; passively observe DHCP/mDNS/UPnP/network behavior on boot;
   attempt read-only USB descriptor enumeration on the Micro-USB service port
   (no vendor commands that could trigger a flash); correlate button/LED
   behavior against `runtime-interface-inventory.md`.
3. **First powered observations:** capture serial output at 115200 baud if
   service pads are identified safely; record boot messages, `/proc/mtd`,
   memory size, network interfaces, and process/service state, all
   read-only. Do not interrupt boot, write U-Boot environment, mount
   partitions read-write, or invoke update/recovery commands.
4. **Interface measurements** (unpowered continuity testing first): map both
   daughterboard connectors pin-by-pin; identify power, ground, reset,
   clocks, UART, I2C, SPI, USB, and digital-audio candidates; then use a
   logic analyzer on suspected buses during ordinary boot and local playback.
5. **Correlate and record:** update `runtime-interface-inventory.md` and the
   decision-gate section of `hardware-validation-plan.md` with actual
   captures, timestamps, probe settings, and sample ID.
6. **Reuse decision (roadmap stage 5):** only after step 5 gives real
   evidence — keep BG2CDP / replace compute module / bypass electronics.
   This should not be guessed ahead of evidence.

Physical measurements, device modification, and firmware flashing require
explicit human control and are outside autonomous work; documentation,
correlation of captured evidence against the firmware findings, and decision
write-ups can be done autonomously once data is provided.

### Smaller open items

- **Azure restore runbook** — **documented:**
  [azure-restore-runbook.md](docs/acquisition/azure-restore-runbook.md).
- **`P0-002`** — Google/Nest Chromecast OSS Drive folder, still `DISCOVERY_ONLY`.
  The folder title is externally observable, but unauthenticated contents were
  not exposed and no artifact was acquired.
- **`P0-005`** — historical Harman `cortana-sdk-opensource.html` — **resolved,
  ACQUIRED.** The live URL still redirects, but a Wayback Machine capture
  (2023-12-03, HTTP 200) was found and archived. It is a Microsoft-authored
  third-party notices file for the Cortana SDK (Expat, RapidJSON, Parson,
  zlib, curl, Breakpad, OpenSSL, Opus, etc.), not source code. See
  `docs/corpus/02_CLAIM_EVIDENCE_LEDGER.md` and the updated entry in
  `docs/acquisition/invoke_berlin_artifact_acquisition_manifest.md`.
- **Regenerate `docs/corpus/99_CORPUS_HASHES.md`** whenever a corpus document changes.

---

## 4. Working rules

1. **Originals are never modified.** No repacking, no recompression. Recorded SHA-256
   values are the integrity anchor.
2. **Nothing is executed or flashed.** Tooling downloads, mirrors, hashes, extracts,
   indexes, and documents only.
3. **Git holds what was written, not what was downloaded.** Analysis, notes, metadata,
   and small text artifacts belong here; bytes belong in Tier 2 or 3.
   Litmus test: *would I ever read this in a diff?*
4. **No credentials or signed URLs in Git.** Presigned query strings are redacted in
   sidecars; the stable public `source_url` is retained.
5. **GitHub limits:** files over 100 MB are rejected outright. Bulk material goes to
   Releases, never into history — a large blob committed once persists in every clone
   forever.

See [docs/acquisition/storage-policy.md](docs/acquisition/storage-policy.md) for the
measurements behind these rules and
[docs/acquisition/source-retention-ranking.md](docs/acquisition/source-retention-ranking.md)
for which artifacts have external custodians and which depend on this archive alone.
