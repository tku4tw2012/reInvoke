---
title: Project plan and handoff
description: Current evidence, project status, and next steps for reInvoke
ms.date: 2026-09-02
ms.topic: overview
---

Current state, established facts, and next steps. This file exists so work can resume
in a fresh session — or from GitHub on the web — without the original conversation.

**Last updated:** 2026-09-02

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
| Hardware validation — donor device(s) available | **In progress** — Bluetooth works; **interactive U-Boot console reached over USB** via yellow service mode, see [uboot-access.md](docs/uboot-access.md); NAND healthy and mapped read-only; firmware version still unread |

### The finding that reframes the project

Harman's final firmware, `Barracuda_libre-12.2134.0` in the OTA2 bundle, removes
Cortana, the Cortana harness, Spotify, and the Skype call library. It adds
`oobe-ui` and a `wifi-blocker` service.

Artifact-backed finding: Harman shipped a firmware line whose service set is
consistent with a local Bluetooth-speaker role after the cloud assistant was
removed. Inference: the project's stated end goal, repurposing completeness, may
therefore be partly reachable on stock software rather than by replacing it.
Confirming which firmware a physical unit carries remains the highest-value
observation, which is why it leads the hardware procedure.

### The control-plane emulation result

The device's service bus is WAMP over MsgPack, routed by `bonefish`, an
open-source router that ships in the rootfs. Both the router and its client
services have been run on an x86 host under `qemu-user` in a rootless sandbox.
A third-party client joined the bus and successfully called
`com.harman.musicMuteToggle`, which changed real service state.

This verifies the software control path under emulation. It does not verify
speaker output, Bluetooth transport behaviour, daughterboard wiring, or physical
I2C device identities.

### Evidence classification used below

- **Verified facts** are directly observed from held artifacts, hash checks, or
  reproduced execution logs.
- **Artifact-backed findings** are conclusions supported by those artifacts but
  not yet measured on a physical unit.
- **Inference** is explicitly marked when it transfers meaning from names,
  ordering, sibling sources, or plausible system design rather than direct
  Invoke measurement.

### Current evidence split

Verified facts:

- OTA2 contains `Barracuda_libre-12.2134.0`; its rootfs removes the Cortana,
  Spotify, and Skype components listed above and adds `oobe-ui` plus
  `wifi-blocker`.
- The firmware artifacts, FCC exhibits, and acquisition sidecars named in this
  repository are held or mirrored as documented and hash checked where hashes are
  recorded.
- The WAMP router and selected services run under `qemu-user`, and audio
  volume/mute calls changed emulated service state.

Artifact-backed findings:

- The final firmware service set is consistent with Harman converting the
  product to a local Bluetooth-speaker role.
- The local WAMP bus is a real control surface inside the firmware, but stock
  network reachability is blocked by firewall rules unless the debug path is
  active.
- The MCU startup path issues the recorded raw I2C transactions under emulation.

Inference:

- Stock final firmware may satisfy part of repurposing completeness before any
  replacement firmware work.
- Device-class readings for the I2C addresses and the likely utility of a
  virtual HCI adapter remain engineering hypotheses pending physical or
  transport-level tests.

### Where things live

```text
~/<workspace>/
├── reinvoke/           about 3.5 MB this Git repository
└── reinvoke-archive/   about 7.3 GB bulk payloads, NOT under Git control
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

- The recovery command's partitions sum to exactly 512 MiB. Third-party
  `/proc/mtd` output reports a 256 MiB aggregate device, so physical capacity and
  applicability of the recovery map remain unresolved.
- Several regions are paired (`post-bootloader` twice, `tz_en`/`tz_en-B`, and
  `bootimgs`/`bootimgs-B`). Their names suggest redundancy, but slot-selection
  and fallback semantics remain unresolved.
- `tz_en` implies a TrustZone/secure-world image.

### Boot parameters — same file

```text
console=ttyS0,115200   init=/bin/sh   root=/dev/ram   initrd=0x08000000
```

A serial console at 115200 baud and a RAM-disk recovery path with a shell as init.

### USB identity — from `Mrvl_WinUSB_Driver_040114/Mrvl_WinUSB.inf`

`VID_1286` (Marvell) with `PID_8100` / `PID_8101`, plus `VID_8086` with
`PID_e001` / `PID_c001` / `PID_d001`. These identify the SoC in USB boot / recovery mode.

The same INF also declares `PID_8174` as
`"Marvell(R) WTP: Tools package USB Driver for BG2CDP Boot Device"`, and
`marvell_flash_tool/run.sh` targets `usb_boot 1286 8174`. This is the identifier
the Invoke actually presents; `8100` / `8101` are for Monahans parts and were
never observed on this unit.

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

The Phase 3 preservation and extraction baseline is complete. Targeted static
analysis remains open for boot-stage identity, opaque USB records, factory-mode
selection, and active-slot behavior.

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
2. Does the Micro-USB service port expose a Marvell boot endpoint?
   **Answered 2026-09-01: yes, but the host's iROM bootstrap path is not
   entered.** The unit
   presents `1286:8174`, the BG2CDP Boot Device, for roughly four seconds on
   every power-on. `usb_boot` reaches it, claims the interface, and reports a
   complete `08_IMAGE` transfer. However `bDeviceSubClass` reads 254 on every
   captured attempt, and `usb_boot` only enters its iROM bootstrap path on
   `0xFF`. The vendor button sequence produces the yellow panel indication and
   a sustained retry loop, but not a captured subclass change, so no U-Boot
   console has been reached. Details are in
   [usb-service-mode.md](docs/usb-service-mode.md).

Host preparation is complete for the next controls: ADB 1.0.41, libusb 1.0.25,
bus-specific usbmon capture, timestamped attempt bundles, and a native x86-64
build of the pinned open-source flasher at commit `63444e82`.

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

### Open software work, no hardware required

- **MCU register capture.** **Done as an emulation trace.** Three I2C slave
  addresses and their bring-up writes were recovered under emulation and aligned
  to service log stages. See [mcu-boundary.md](docs/emulation/mcu-boundary.md).
  Current limit: no physical I2C bus was accessed, no real-device responses were
  captured, and no part identities are claimed. The ARM guest-side ioctl shim
  answers raw `I2C_RDWR` without exposing a host bus; `i2c-stub` cannot do this
  because it implements SMBus rather than raw I2C.
- **Volume setter arguments.** **Done.** A guest-side shim now supplies the ALSA
  control ioctls missing from `qemu-user`. The final `audio-ui` initializes all
  playback controls plus microphone mute, and `volumeSet`, `volumeAdjust`, and
  `musicMuteSet` work end to end. All three take the value first and stream
  name second.
- **Bluetooth transport arguments.** Blocked on an HCI transport, not on BlueZ
  user-space services. The examined Bluetooth service uses Bluedroid over the
  kernel Bluetooth subsystem, so the sandbox needs an HCI device and `/dev/rfkill`
  rather than `bluetoothd` and D-Bus. Current limit: Bluetooth procedures are
  registered on the bus but have not been exercised through an emulated HCI
  adapter or a paired peer. See [bluetooth-stack.md](docs/emulation/bluetooth-stack.md).
- **Update-state semantics.** **Done.** `mtd_exec setbootflags` toggles the first
  marker in the persistent `fw_stat` MTD partition between update-required
  (`qeru`) and no-update (`puon`). See
  [boot-update-state.md](docs/emulation/boot-update-state.md).
- **Boot-slot selection.** Still unresolved. The recovery updater targets
  `bootimgs` and `rootfs`, but no preserved state maps those writes to a
  specific active/inactive slot.
- **USB download-mode feasibility.** No BootROM, OTP, or secure-boot source
  exists in the corpus, so this cannot be settled by analysis. It is a
  measurement, gated by the observation procedure.

---

## 4. Working rules

1. **Originals are never modified.** No repacking, no recompression. Recorded SHA-256
   values are the integrity anchor.
2. **No physical device is modified or flashed.** ARM binaries run only from a
   copied rootfs in a rootless sandbox. Preserved originals remain read-only.
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
