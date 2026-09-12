---
title: Project plan and handoff
description: Current evidence, project status, and next steps for reInvoke
ms.date: 2026-09-12
ms.topic: overview
---

Current state, established facts, and next steps. The normative behavior and
service boundaries are defined by the
[current product and architecture contract](docs/current-product-contract.md).
Older plans and milestone records below are preserved as evidence and are
explicitly labeled when they no longer describe the target.

**Last updated:** 2026-09-12

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
| Hardware validation, closed device available | **Native milestone reached**: candidate 02 starts from NAND and has demonstrated Bluetooth pairing, audible playback, rotary volume, physical provisioning, and local-network MCU/DSP calls |
| Owned service replacement | **Accepted architecture**: owned PID 1 supervises the MCU, DSP, network, BlueZ/BlueALSA, Bonefish compatibility, logging, and bounded helper services |
| Persistent installation | **Demonstrated**: a complete vendor-format bundle starts the owned runtime from NAND; settings remain volatile and native USB/ADB remains unresolved |

### The finding that reframes the project

Harman's final firmware, `Barracuda_libre-12.2134.0` in the OTA2 bundle, removes
Cortana, the Cortana harness, Spotify, and the Skype call library. It adds
`oobe-ui` and a `wifi-blocker` service. The examined physical sample carries the earlier
`Barracuda_libre-12.2050.3` rootfs.

Artifact-backed finding: Harman shipped a firmware line whose service set is
consistent with a local Bluetooth-speaker role after the cloud assistant was
removed. That image is now a donor and comparison point rather than the target
runtime. The verified direction is a reInvoke-owned PID 1, rootfs, and
application stack that reuse only the required kernel support, firmware,
calibration, MCU protocol, and DSP/audio boundary.

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
- A GCC 4.9 replacement kernel registers the first GPIO bank at base 0 while
  retaining USB, Wi-Fi, and SPI.
- The physical MCU reports application version `000116` and recovery flag `0`.
- The physical DSP reports `0.0.64.58`, packed as WAMP value `25688`.
- ALSA card 1 accepts 48 kHz stereo `S32_LE`, and the speaker audibly reproduced
  a guarded -48 dBFS tone.
- ALSA capture is resolved at stereo 48 kHz `S32_LE`, 256-frame periods, and
  16 periods. Attended speech/tapping correlation passed while unmuted; muted
  capture produced all-zero samples.
- The physical rotary ring produced 120 matched MCU and WAMP direction events.
- Native `bt8xxx.ko` enables `hci0`; Bluedroid enables A2DP Sink and both AVRCP
  roles and enters discoverable pairing mode.
- Both iPhone and Ubuntu peers pair and send RTP/SBC media. Ubuntu's BlueZ and
  PulseAudio source path was independently verified. Donor Bluedroid receives
  the frames but does not wake its decoder/PCM client.
- The checksum-gated service launcher rebuilds the complete RAM diagnostic
  graph from a clean service state and keeps amplifier, DAC, and NAND safe.
- The owned MCU service is the process-lifetime microphone privacy controller.
  It registers the public compatibility API, stores state atomically in RAM,
  retries failures independently of WAMP, protects the red indication, and uses
  the DSP service's root-only mode-`0600` Unix socket.
- The owned DSP service exports seven public WAMP procedures. The donor
  historically exported eight; raw microphone mutation is now private.
- The patched BlueALSA path enforces active-PCM ownership and the donor ALSA
  contract, buffers decoded PCM, conceals SBC RTP gaps, and drains short or
  closed-FIFO streams.
- Static ARM provisioning and station-apply daemons now provide ephemeral TLS,
  a 256-bit token, root-peer verification, derived WPA2 configuration, and
  bounded association. Live fail-closed and AP success-path tests pass.
- The native SD8887 STA/uAP profile returns USB in five seconds, exposes
  `mlan0` and `p2p0`, and serves an isolated WPA2/DHCP provisioning network with
  no gateway, DNS, or forwarding. Real `mlan0` association, DHCP renewal,
  gateway reachability, and DNS resolution also pass without plaintext
  credential persistence.

Artifact-backed findings:

- The final firmware service set is consistent with Harman converting the
  product to a local Bluetooth-speaker role.
- The local WAMP bus is a real control surface inside the firmware, but stock
  network reachability is blocked by firewall rules unless the debug path is
  active.
- The MCU startup path issues the recorded raw I2C transactions under emulation.

Historical inference:

- Before owned RAM boot was available, stock final firmware appeared capable of
  satisfying part of repurposing completeness. This is retained as a historical
  inference, not a current product direction.
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
| 2 | Firmware bundles (569 MB) | [GitHub Releases](https://github.com/tku4tw2012/reInvoke/releases/tag/invoke-firmware-mirror) |
| 3 | Full working set including Git mirrors (4.9 GB) | Private operator-managed cold archive |

No archive credentials, signed URLs, account names, or container names belong
in Git. The local archive, private cold copy, and published release were verified
byte-identical by SHA-256 at the recorded preservation checkpoint.

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

- The recovery command's partitions sum to exactly 512 MiB and do not describe
  this unit's physical NAND. U-Boot and RAM-native Linux both identify a 256 MiB
  Toshiba NAND, and a complete 268,435,456-byte logical data image has been
  captured. The active SquashFS begins at `0x02920000`.
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

### Phase 4 — hardware validation and RAM-product acceptance

A donor device is now physically in hand and will not be opened. The operator
procedure is [no-disassembly-observation-procedure.md](docs/no-disassembly-observation-procedure.md),
which supersedes the ordering in `hardware-validation-plan.md` for units that
stay closed. The end goal remains **repurposing completeness (L2)**, not full
schematic completeness.

The FCC exhibit set for `APIHKINVOKE` is now held locally under
`originals/fcc/` in the archive, with provenance sidecars in `metadata/`. Until
this session those claims cited evidence the project did not possess.

**The two observations that matter most:**

1. Does the unit pair over Bluetooth and play audio? **Answered 2026-09-03:**
   the RAM-only BlueZ/BlueALSA replacement forms a durable allowlisted bond,
   decodes A2DP SBC, advances the target ALSA DMA path, and produced audible
   output after the MCU amplifier and DAC were explicitly unmuted.
2. Does the Micro-USB service port expose a Marvell boot endpoint?
   **Answered 2026-09-02: yes.** Yellow service mode transitions through
   Marvell USB stages and reaches interactive RAM-loaded U-Boot. U-Boot then
   loads a custom initramfs whose PID 1 exposes root ADB without mounting NAND.
   The verified path, image request sequence, and safety boundary are in
   [uboot-access.md](docs/uboot-access.md) and
   [native-ram-platform.md](docs/native-ram-platform.md).

Historical preparation record: ADB 1.0.41, libusb 1.0.25,
bus-specific usbmon capture, timestamped attempt bundles, and a native x86-64
build of the pinned open-source flasher at commit `63444e82`.

**Current remaining work:**

1. Repair native USB ADB and add the approved key-authenticated network
   administration fallback without blocking the working product.
2. Give the next image consistent NAND-specific identity and a repeatable
   private-configuration build interface.
3. Repeat the microphone data-path/privacy measurements on a native NAND boot.
4. Design persistence for Wi-Fi profiles and Bluetooth bonds together with
   power-loss and update-preservation behavior.
5. Publish the native milestone, recovery limits, reproducible source, and
   preserved engineering history.

The occasional Mic-Mute press for which the companion MCU produces no event
remains a hardware/firmware observation. Both the donor and owned service show
this behavior; the owned privacy controller remains correct whenever an event is
delivered.

**Historical Phase 4 work plan (status recorded on 2026-09-03):**

The list below preserves the bring-up sequence. Status statements in it describe
that checkpoint, not the current target; the current gates above and the
[canonical contract](docs/current-product-contract.md) take precedence.

1. **Done:** establish reproducible rebuilt-kernel boot profiles using NDK r10e
   GCC 4.9, the verified `0x02008000` layout, and checksum-gated device trees.
2. **Done:** add DesignWare SPI, low GPIO numbering, and verify MCU/DSP
   bidirectional startup.
3. **Done:** enable Berlin ASoC and ALSA loopback, enumerate both cards, pass a
   muted zero-data test, and audibly verify a guarded low-level tone.
4. **Done:** native SD8887 HCI, peer pairing, A2DP/AVRCP, absolute
   volume, and SBC ingress are complete. A static BlueZ/BlueALSA replacement
   now provides a RAM-only, address-allowlisted pairing path and a verified
   mute-first A2DP-to-PCM pipeline. The attended audible Bluetooth acceptance
   test passed on 2026-09-03. See
   [bluetooth-stack.md](docs/emulation/bluetooth-stack.md) and `P1-045`.
5. **In progress:** replace donor MCU, DSP, and media adapters with
   reInvoke-owned services. The media transport and host-side WAMP compatibility
   service are owned. The owned MCU service now passes mute-first
   initialization, default-deny unmute, five-second heartbeat, status, and
   bidirectional rotary tests on hardware. The owned DSP service passes
   exact-image download, physical boot-event, and `getVer` validation without
   changing mute policy. At this checkpoint the physical ALSA capture endpoint
   was unresolved. It was later resolved at stereo 48 kHz `S32_LE`,
   256-frame periods, and 16 periods, with attended signal correlation and
   all-zero muted capture. Opening the enclosure or probing board-level buses
   is not part of this project.
6. **In progress:** the physical-gated provisioning API and authenticated TLS
   parser, SD8887 `p2p0` radio adapter, isolated provisioning AP, real station
   application, and restart-safe owned DHCP/resolver lifecycle are implemented
   and live-validated in RAM. The packaged lifecycle image is reproducible and
   contains the reviewed components. Credential replacement while the owned
   supervisor remained active also passed. The hardened packaged image then
   cold-booted in yellow mode with automatic networkd startup and no NAND
   mount; its live binary matched the newly built artifact, and the
   credential-free acceptance image correctly waited for a station supplicant.
   SSH is optional.
7. **Done:** cold-boot the current checksum-gated RAM image, validate recovery
   through the external USB and button sequence, and retain yellow mode as the
   rollback path. The indefinite prompt waiter now remains armed until the
   operator reaches yellow mode.
8. **In progress:** recover the donor MCU, DSP, audio, source-manager, LED,
   button, and update contracts through static analysis, emulation, interposed
   system calls, WAMP capture, and live RAM-only diagnostics. MCU and DSP
   control contracts have live RAM evidence. Microphone capture was the
   remaining audio-path gap at that checkpoint and has since been resolved.
9. **Owned replacement:** completed for the accepted service graph. Each required
   contract is behind an owned reInvoke boundary while RAM rollback remains.
10. **Persistence decision:** **historical gate completed.** Candidate 02 now
    starts the owned service graph from NAND. Persistent user configuration is
    a separate unresolved design.

The supported physical control surface is the closed unit's Micro-USB port and
buttons. Opening the enclosure, electrical probing, board modification, and
firmware flashing remain outside autonomous work. Flashing requires a separate
human-approved recovery and rollback plan.

### Smaller open items

- **Private cold-archive restore runbook** — **documented:**
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
- **DSP link contract.** **Done as static analysis confirmed on the wire.**
  The donor `dsp-client` transport, frame layout, checksum, GPIO handshake
  order, expander-backed reset, boot-image staging, and full command and event
  vocabulary were recovered from the held binary without opening the unit.
  Both directions share one five-byte header, and every frame moves one byte
  per `SPI_IOC_MESSAGE`, which is what makes ioctl counts far exceed frame
  counts. A byte-exact hardware capture then confirmed both directions. See
  [dsp-boundary.md](docs/emulation/dsp-boundary.md), the passive decoder
  `tools/control/dsp-frame-decode.mjs`, and the offline capture comparator
  `tools/emulation/spi-capture-label.mjs`. Current limit: the multi-block
  transmit path is understood from code only, only `getVer` has been seen on
  the wire, and no DSP part identity is claimed.
- **DSP wire capture.** **Done.** A byte-exact log-and-forward capture on the
  physical unit, archived as
  `hardware/software-captures/20260903T191657Z-dsp-ioctl-record/`, holds 40,121
  four-byte image transfers plus 23 one-byte message transfers. The image
  payloads concatenate to a byte-identical copy of the per-byte bit-reversed
  `dsp-img.ldr`, and the predicted device frame `00 01 00 01 06 04 00 00` for
  `EVENT_DSP_BOOTUP` appeared exactly as written. **The DSP program is
  host-loaded and volatile**: the host resends all 160,484 bytes at every
  service start, so `dsp-img.ldr` is part of a replacement's payload, not a
  device property. Verified offline with
  `tools/emulation/spi-capture-label.mjs`.
- **USB download-mode feasibility.** **Resolved after this static-analysis
  checkpoint:** yellow service mode reaches interactive RAM-loaded U-Boot and
  boots the owned initramfs. The absence of BootROM, OTP, and secure-boot source
  in the corpus remains a historical evidence limit, not an access blocker.

---

## 4. Working rules

1. **Originals are never modified.** No repacking, no recompression. Recorded SHA-256
   values are the integrity anchor.
2. **Persistent device changes require explicit owner approval.** The current
   native image was installed through an approved whole-good-block vendor
   operation. Builds, tests, documentation updates, and acknowledgement strings
   do not authorize another write, retry, reset, or reboot.
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
