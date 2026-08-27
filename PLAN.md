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

### The two `83_IMAGE` variants

Both are exactly **107,934,810 bytes** but differ in **83,800,608 bytes (77.64%)**.

| Variant | SHA-256 |
|---|---|
| `StockRoot` release asset | `f59d0a56f5d3d4cc90b146e2433ec32da36239e6c4373813d57fe92e19326cc7` |
| Inside `Flashing.zip` | `90a4f54d7c92f55ea20f6d63f89caae5f7738b62dec4913bded0fd7816ec9a1c` |

Identical length with ~78% differing content suggests a same-size rootfs rebuild rather
than a patch. **What differs is the single most interesting open question.**

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
   artifacts. The remaining repository action is an optional clean preservation
   milestone commit after reviewing the uncommitted documentation.

### Beyond Phase 3 — revival path

The end goal is **repurposing completeness (L2)**, not an unsupported claim of
full schematic completeness. See [revival-roadmap.md](docs/revival-roadmap.md).
The next engineering deliverable is a runtime-interface inventory, now captured
in [runtime-interface-inventory.md](docs/bundle-contents/invoke-flashing/runtime-interface-inventory.md),
followed by controlled, read-only observation on a physical sample if one is
available. No flashing or execution of preserved firmware is part of the
autonomous work.
The physical-work gate is specified in
[hardware-validation-plan.md](docs/hardware-validation-plan.md), which now
also documents what FCC regulatory teardown photos already establish without
any physical unit (Micro-USB service port, board topology, no external UART)
and a no-disassembly fallback path for when the enclosure cannot be opened.

**Immediate next steps, in order:**

1. Obtain a donor device (owner-driven; not autonomous).
2. Regardless of disassembly access, attempt the no-disassembly observations
   first: adapter/network passive observation, and read-only USB descriptor
   enumeration on the Micro-USB service port (no vendor commands that could
   trigger a flash).
3. If disassembly becomes possible: board photographs, connector pinout via
   continuity testing (unpowered), then serial console capture and logic
   analyzer work per `hardware-validation-plan.md`.
4. Correlate whatever is captured against
   `runtime-interface-inventory.md` and update it plus the decision-gate
   section of `hardware-validation-plan.md` with results.
5. Only after that correlation does the roadmap's stage 5 reuse decision
   (keep BG2CDP / replace compute / bypass electronics) get made — it should
   not be guessed ahead of evidence.

### Smaller open items

- **Azure restore runbook** — **documented:**
  [azure-restore-runbook.md](docs/acquisition/azure-restore-runbook.md).
- **`P0-002`** — Google/Nest Chromecast OSS Drive folder, still `DISCOVERY_ONLY`.
  The folder title is externally observable, but unauthenticated contents were
  not exposed and no artifact was acquired.
- **`P0-005`** — historical Harman `cortana-sdk-opensource.html`, still
  `DISCOVERY_ONLY`; current site access returns 403/redirect and no artifact
  was acquired.
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
