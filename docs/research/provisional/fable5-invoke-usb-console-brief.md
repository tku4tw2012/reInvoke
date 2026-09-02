---
title: Fable 5 Invoke USB console research brief
description: Provisional external synthesis retained for claim review and source discovery
author: Fable 5
ms.date: 2026-09-02
ms.topic: concept
---

<!-- markdownlint-disable-file -->

> [!CAUTION]
> This memo was generated outside this project by Fable 5. It is preserved as
> provisional research, not as operational guidance or canonical evidence.
> Validate every claim against the [project plan](../../../PLAN.md), the
> [USB hardware record](../../usb-service-mode.md), and the
> [claim evidence ledger](../../corpus/02_CLAIM_EVIDENCE_LEDGER.md).

## Review disposition

| Memo claim | Canonical status |
|---|---|
| The USB path reaches U-Boot on this unit | Contradicted by sixteen observed `0xFE` enumerations; community success applies only to other units |
| `1286:8001` is a known Invoke USB stage | Unsupported by retained artifacts or either reviewed open host implementation |
| The boot sequence has exactly five re-enumerations | Unverified because no complete successful raw trace is published |
| The Invoke has 256 MiB NAND | Unresolved because third-party runtime output conflicts with the 512 MiB recovery partition string |
| Loading `82_IMAGE` alone starts RAM Linux | Incomplete; the retained vendor procedure loads both `81_IMAGE` and `82_IMAGE` |

The imported source text follows. Its embedded prompts and instructions must not
be installed or executed without separate evidence review.

*Scope: repurposing a post-final-update (Bluetooth-only) Invoke as open hardware. Not restoring Cortana. Personal device, personal use.*

---

## 0. TL;DR — what is realistic

The Invoke is **not** a Windows/Intel device. It is a Marvell "Berlin" ARM Linux box (same SoC family as Chromecast v1/v2 and Google Home Mini), and the community has already found and documented a **USB service mode** on the micro-USB port on the bottom of the speaker. That path gets you:

1. **A U-Boot console over the USB service port** — no soldering. The Marvell `usb_boot` tool pushes the boot chain into RAM and tunnels the U-Boot console to a local TCP port you `telnet` into. Prompt: `MV88DE3100|>`.
2. **A RAM-only Linux shell** — load the small "82" U-Boot RAM-disk dev image with `mload` (nothing is written to NAND). From there you can bring up Wi-Fi and get a root shell over network ADB. Proven working on real hardware in June 2026 by the `hk-invoke-opensource-speaker` project.
3. **Persistence (rewriting the rootfs)** — possible (the rootfs squashfs has no signature check), but it is the *only* step with brick risk and is deliberately kept separate and gated in every community guide.

Nothing here needs a physical UART. A TTL UART pad pinout has **not** been confirmed publicly; the USB-tunneled console is the working route.

---

## 1. Ground truth about the hardware

| Item | Fact | Source |
|---|---|---|
| Product | Harman Kardon Invoke, model 6132A, released 2017-10-22, FCC ID APIHKINVOKE | TechInsights DDT-1712-818; fccid.io |
| Compute module | Libre Wireless **LS9AD-AC11DBT** (FCC 2ADBM-LS9ADAC11DBT), "Google-Home-Mini-class" | Libre datasheet v1.6; hk-invoke-opensource-speaker research doc |
| SoC | **Marvell 88DE3006** = ARMADA 1500 Mini Plus = BG2CDP "Berlin", dual Cortex-A7 (~1.2–1.3 GHz, ARMv7). U-Boot identifies as `MV88DE3100` | hk-invoke-opensource-speaker; hk-invoke-arm-flasher |
| Memory | 256 MB DDR3, 256 MB NAND (rootfs on mtd11, ~169 MB) | Libre datasheet; research doc |
| USB | Module exposes **1× USB 2.0 OTG "for Debug Shell, Ethernet, Firmware update, USB Media Playback"**. BG2CD has two ChipIdea USB controllers: usb0 host-only, usb1 dual-role | Libre datasheet §2; LKML BG2CD DTS patch |
| UART | Module has a UART "for HOST-MCU communication / Debugging" (pins 43 Tx / 45 Rx; rev 2.0 also 49/51). On the Invoke this talks to the button/LED MCU. No confirmed console pad on the Invoke PCB | Libre datasheet pin table; research doc §9 |
| Wi-Fi/BT | Marvell Avastar 88W8887 (SD8887) over SDIO, USB ID 02df:9135; firmware alias `sd8801` | research doc §1 |
| Audio | Wolfson/Cirrus WM8904 codec → 6-driver amp; 7-mic array | research doc §1 |
| OS | Linux **3.8.13**, Yocto/Poky 2.1 "Krogoth", glibc 2.23, `arm-poky-linux-gnueabi` | research doc §1 |
| Security | Bootloader/kernel likely secure-boot (as on Home Mini). **Rootfs squashfs has no signature check** — coggy9 rebuilt and reflashed it and it booted | research doc §4; Courk's Home Mini write-up |
| Physical | Service port (micro-USB) and 19 V power jack on the bottom; TR10 Torx security screws if you open it | iFixit device page; hk-invoke-arm-flasher |

---

## 2. What "console via USB" means on this device

There are three tiers. Do them in order; each one is a checkpoint.

### Tier 1 — U-Boot console (RAM-only, zero writes)
- Put the speaker in **service/loader mode** (LED ring turns **yellow**). Two published entry sequences — try (a) first:
  - (a) Unplug power → hold the reset pinhole → plug in the 19 V adapter → release when the yellow ring appears (~4 s). *(hk-invoke-arm-flasher)*
  - (b) Hold reset while applying power → tap mic-mute 4× within 5 s → yellow ring. *(hk-invoke-opensource-speaker runbook)*
- Connect micro-USB to a **direct USB-A 2.0 port** on a Linux host. Expect Marvell USB IDs **`1286:8174`** (service port) and **`1286:8001`** (BootROM device-mode download).
- Run the boot tool (`usb_boot` from Harman's flashing zip, or the open `usb_boot_arm` rewrite): `sudo ./usb_boot_arm 1286 8174 ./ 8141`, then in a second terminal `telnet 127.0.0.1 8141`. The tool feeds `bcm_erom.bin.usb` → `bootloader.img` → `sysinit.img` → `drm_erom.img`; the SoC **disconnects and re-enumerates 5 times** during this, which is why sync USB libraries, WSL2/usbipd, VM passthrough and USB-C hubs all fail.
- Success = the `MV88DE3100|>` prompt. Safe first commands: `help`, `version`, `printenv`, `bdinfo`, `mtdparts` (if present). **Do not type `l2nand` anything.**

### Tier 2 — RAM-booted Linux shell (still zero writes)
- From the tool/console, `mload` the **82_IMAGE** (U-Boot RAM-disk dev image — small enough for RAM). 83_IMAGE is the full system and is NAND-only; it will not `mload`.
- The RAM boot runs `/etc/init.d/rcS` but **not** `rc.sysinit`, so only `lo` comes up. Wi-Fi needs the device's own blobs and init sequence, replicated by hand:
  1. On the host: `binwalk -e` the **StockRoot 83_IMAGE** → mount the squashfs → copy `lib/firmware/mrvl/*.bin` and read `/etc/init.d/rc.sysinit` + `rcS`.
  2. On the device: stage blobs into `/lib/firmware/mrvl/`, `insmod mlan.ko` then `insmod sd8801.ko` (a.k.a. `sd8887.ko`/`sd8xxx.ko`, `drv_mode=` STA, `fw_name=mrvl/sd8887_uapsta.bin`), `ip link set mlan0 up`, `wpa_supplicant`, `udhcpc`.
  3. Then `adb connect <device-ip>:5555` for a root shell (StockRoot build has network ADB on).
- Fallback worth testing first: USB gadget Ethernet (`g_ether` → `usb0`) if the kernel config in `invoke-kernel.tar` has `CONFIG_USB_GADGET`. Host-only fallback: USB-Ethernet dongle (asix / cdc_ether).
- Proven result (2026-06-22): RAM boot → Wi-Fi associated → DHCP on `mlan0` → ping 1.1.1.1 and example.com. Device shows on the router as "MARVELL SEMICONDUCTOR" (OUI 00:50:43). Reboot wipes everything.

### Tier 3 — Persistence (gated; the only brick-risk step)
- Rebuild the rootfs squashfs (CaramelKat/PodiumFlashing has an extract/modify/rebuild tool for 83_IMAGE) and write it with `l2nand 83` from the U-Boot console.
- The mask boot ROM survives NAND writes, so an interrupted `l2nand 83` is recoverable by re-entering service mode. A *wrong* image is not. Only ever flash image token 83, only a byte-verified image, on stable power, direct USB.

---

## 3. Firmware / artifact inventory (download and mirror locally — Harman's links rot)

| Artifact | Size | What it is | Use it? |
|---|---|---|---|
| **StockRoot 83_IMAGE** (build 11.1842.0) | 107,934,810 bytes (~108 MB) | Last Cortana-era rootfs; pre-rooted, network ADB on, OTA blocked, Wi-Fi works | **Yes — the base.** coggy9/HKHacking releases → `StockRoot/83_IMAGE`. Verify the byte count |
| OTA2 / final 83_IMAGE (12.2134.0, "Barracuda_libre") | ~69 MB | March-2021 final OTA. Ships a **wifi-blocker** binary; Cortana and Wi-Fi removed | **No** as a base. Only mine it for the BT blob. Size is the fast tell: 69 MB = blocker, 108 MB = working |
| Harman "INVOKE Driver & OTA2" zip (12.2314.0, 2021-09-08) | ~220 MB | Harman's DIY USB flash package: Windows driver + OTA2.pdf + images. Adds factory-reset-by-pinhole (hold 5 s) and BT fixes | Reference only (this is likely what your unit has) |
| `Harman.Kardon.INVOKE.Flashing.zip` (HarmanFlash release) | 263 MB | The toolkit: `usb_boot`/`hk_usb_boot`, `l2nand`, `Mrvl_WinUSB` driver, `run.sh`, images 70/81/82/83/99 | Yes — run `run.sh` as root on Linux |
| `invoke-kernel.tar` (archive.org/details/invoke-kernel) | 520 MB | Kernel 3.8.13 BSP source: out-of-tree mlan/sd8887 + bt8xxx, snd-soc-berlin, Berlin DTS, `request_firmware()` names | Yes — build modules against this, not generic headers |
| `Invoke Source Disclosure.tar` (archive.org/details/HK-Invoke-source-disclosure) | 110 MB | Userland only: stock `rc.sysinit`/`rcS`, wpa_supplicant, U-Boot env | Yes for init sequence; no drivers/firmware inside |
| **99_IMAGE** | — | **Confirmed brick** (stuck loop, no U-Boot, hardware recovery) | **NEVER. Don't even keep it in the flash directory** |

Image tokens: `81` = kernel uImage · `82` = U-Boot RAM-disk dev image (the RAM-only vehicle) · `83` = full system (NAND-only) · `99` = bricks the Invoke.

---

## 4. Hard NEVERs (put these in `copilot-instructions.md` so the agent enforces them)

1. Never flash, load, or download `99_IMAGE`.
2. Never run `l2nand` during bring-up. RAM-only via `mload`/device-mode until Tier 3 is a deliberate, separate decision.
3. Never base a build on the 12.2134.0 / 12.2314.0 OTA2 rootfs if you want Wi-Fi.
4. Never use a hub, USB-C adapter, WSL2 + usbipd, or VM USB passthrough for the boot sequence. Direct USB-A 2.0 (black) port, real data cable.
5. Not `kwboot`, `WtpDownload`, `A3700-utils`, or Qualcomm EDL tooling — wrong Marvell family / wrong vendor.
6. No corporate/BitLocker-managed machine for the driver or flashing step (Harman's own note). Use a personal Linux box or a **Raspberry Pi 4** (proven path).
7. Never commit firmware images, NAND dumps, factory certs, or Wi-Fi PSKs to the repo.

---

## 5. Phased plan with acceptance criteria

| Phase | Goal | Done when |
|---|---|---|
| **0. Workspace** | Create a private repo; add the upstream repos as reference (clone or submodule); write `copilot-instructions.md` with §4 | Copilot can answer "what is image 82?" from the repo alone |
| **1. Host toolkit** | Linux host (Pi 4 or x86 Linux): `gcc libusb-1.0-0-dev telnet binwalk squashfs-tools adb python3`; udev rule for `1286:8174` / `1286:8001`; build `usb_boot_arm` | `lsusb` shows the device in service mode; tool starts and waits |
| **2. Enumerate** | Enter service mode; capture `dmesg -w` and `lsusb -v` for every re-enumeration | You have a log of all 5 re-enumerations with VID:PID and interface/endpoint descriptors |
| **3. U-Boot console** | Run the boot tool + `telnet 127.0.0.1 8141`; capture `help`, `version`, `printenv`, `bdinfo` | `MV88DE3100|>` prompt captured to a file; env saved to `logs/uboot-env.txt` |
| **4. Images on the host** | `binwalk -e` StockRoot 83_IMAGE; mount squashfs; extract `lib/firmware/mrvl/*`, `rc.sysinit`, `rcS`; parse 83 layout with PodiumFlashing `parseFS.py` / Aristoddle `parse_ota83.py` | Blob names match the `request_firmware()` strings grepped from `invoke-kernel.tar` |
| **5. RAM-boot Linux** | `mload` 82_IMAGE; get a shell; record `/proc/meminfo`, `/proc/mtd`, `dmesg`, `lsmod`, whether a USB gadget controller is present | Shell prompt over the tunneled console; MTD map saved |
| **6. Network + ADB** | Stage blobs → insmod → wpa_supplicant → udhcpc → `adb connect :5555` (or `g_ether`) | Root shell over ADB; device visible on router as Marvell OUI |
| **7. (Gated) Persistence** | Only after 0–6 are repeatable and you accept the risk: rebuild rootfs, `l2nand 83` | Boots from NAND with your change; you have a tested re-flash path back |

---

## 6. GitHub Copilot setup

Copilot cannot touch the USB port — you run the hardware steps and paste logs back. Copilot's job is everything around that: ingesting the upstream research, building/porting the host tools, writing the scripts and udev rules, parsing the images, diffing kernel configs, and triaging your logs. Use **Copilot agent mode in VS Code** (it can read public GitHub repos directly, which matters because the canonical `coggy9/HKHacking` docs live in README + Discussions).

### 6a. Suggested repo layout
```
invoke-console/
  .github/copilot-instructions.md   # the guardrails (§4) + device facts (§1)
  .github/prompts/                  # the prompt files below (*.prompt.md)
  refs/                             # git submodules or shallow clones of upstream repos
  tools/                            # usb_boot_arm build, udev rules, helper scripts
  logs/                             # dmesg/lsusb/uboot captures (gitignored if large)
  firmware/                         # .gitignore'd — never committed
```

### 6b. `copilot-instructions.md` (paste)
```
This repo is for bringing up a U-Boot console and a RAM-only Linux shell on a Harman Kardon Invoke
(Marvell 88DE3006 "Berlin" BG2CDP, dual Cortex-A7, Linux 3.8.13 Yocto Krogoth, 256MB NAND) over its
micro-USB service port. Personal device, personal use. We are NOT restoring Cortana.

Safety rules you must enforce in every suggestion:
- Never reference, download, or flash 99_IMAGE. It bricks the device.
- Never suggest `l2nand` or any NAND write unless the user explicitly says "Phase 7 / persistence".
  Default is RAM-only: usb_boot device-mode + `mload` of 82_IMAGE.
- Wi-Fi work must be based on the StockRoot 83_IMAGE (11.1842.0, 107,934,810 bytes), never the
  69 MB OTA2 (12.2134.0 / 12.2314.0) which contains a wifi-blocker.
- USB: direct USB-A 2.0 port on a Linux host or Raspberry Pi 4. Reject WSL2/usbipd, VMs, hubs, USB-C adapters.
- This is Marvell Berlin, not Armada/Kirkwood and not Qualcomm: do not propose kwboot, WtpDownload,
  A3700-utils, or EDL/QDL tooling.
- Firmware images, NAND dumps, certificates and Wi-Fi credentials are never committed. Check .gitignore.
Known-good facts: service-port USB IDs 1286:8174 and 1286:8001; U-Boot prompt `MV88DE3100|>`;
boot tool feeds bcm_erom.bin.usb → bootloader.img → sysinit.img → drm_erom.img and the SoC
re-enumerates 5 times; console is tunneled to TCP (e.g. `telnet 127.0.0.1 8141`).
Upstream references: coggy9/HKHacking (canonical RE; Devices/Invoke/Readme.md, Discussions #3 #5 #7 #8 #11,
Issue #1), jryruegas92/hk-invoke-arm-flasher (docs/REVERSE_ENGINEERING.md, src/usb_boot_arm.c),
Aristoddle/hk-invoke-opensource-speaker (docs/wire-a-new-invoke-runbook.md, scripts/hk-invoke/*),
CaramelKat/PodiumFlashing (imager.py, parseFS.py), tchebb/chromecast-tools (Berlin boot-image tools).
```

### 6c. Prompt files (one per phase — paste into `.github/prompts/*.prompt.md` or straight into agent chat)

**`01-ingest.prompt.md` — build the knowledge base**
```
Read these public repos and write refs/NOTES.md as a single consolidated reference:
- github.com/coggy9/HKHacking — Devices/Invoke/Readme.md, Discussions #3 (image layout / MTD / hk_usb_boot),
  #5 (wifi-blocker), #7 (root / ADB / mcu-interface), #8 (Yocto toolchain), #11 (final OTA), Issue #1
  (service-port USB ID), and the release asset list (StockRoot, HarmanFlash).
- github.com/jryruegas92/hk-invoke-arm-flasher — docs/REVERSE_ENGINEERING.md, docs/PROJECT_STORY.md,
  src/usb_boot_arm.c.
- github.com/Aristoddle/hk-invoke-opensource-speaker — docs/firmware-bringup-research-2026-06-22.md,
  docs/wire-a-new-invoke-runbook.md, scripts/hk-invoke/hk_usb_boot.c, ram_boot_console.sh, parse_ota83.py.
- github.com/CaramelKat/PodiumFlashing — imager.py, parseFS.py.
For each: exact service-mode entry steps, USB VID:PIDs per boot stage, the USB protocol (endpoints,
image-request format), U-Boot commands seen (mload, l2nand, others), image token meanings, MTD layout,
and every warning. Flag any contradictions between sources (e.g. the two service-mode sequences) as
"VERIFY ON HARDWARE". Do not paraphrase safety warnings — quote them.
```

**`02-host-toolkit.prompt.md` — build the host side**
```
Target host: Raspberry Pi 4 running Raspberry Pi OS (64-bit) [or: Ubuntu 24.04 x86-64 laptop]. Create:
1. tools/setup.sh — installs gcc, libusb-1.0-0-dev, telnet, binwalk, squashfs-tools, android-tools-adb,
   python3-usb; builds usb_boot_arm from refs/hk-invoke-arm-flasher/src/usb_boot_arm.c with
   `gcc -o usb_boot_arm src/usb_boot_arm.c -lusb-1.0 -lpthread`.
2. tools/99-invoke.rules — udev rules granting my user access to 1286:8174 and 1286:8001, plus a rule that
   prevents ModemManager or usb-storage from grabbing them.
3. tools/capture.sh — starts `dmesg -w` and a `lsusb -v` poller into logs/<timestamp>/ so all 5
   re-enumerations are captured with descriptors.
4. tools/console.sh — starts the boot tool with the flash dir as an argument, waits for the TCP port, then
   attaches `telnet 127.0.0.1 8141` with `script` logging to logs/. It must refuse to start if a file named
   99_IMAGE exists anywhere under firmware/.
Everything RAM-only. Do not add any l2nand automation.
```

**`03-console-session.prompt.md` — first live session**
```
I'm about to do the first U-Boot console session. Give me a printed checklist: service-mode entry
(both published sequences, labelled a/b), what LED colours mean, which USB port to use, the exact
commands to run in two terminals, what a healthy log looks like at each of the 5 re-enumerations, and
the safe commands to run at the MV88DE3100|> prompt (help, version, printenv, bdinfo, and anything
read-only that reveals MTD/partition layout). Include a "stop here" list of commands never to run.
After I paste the logs, parse them: extract VID:PID per stage, endpoints, U-Boot version string,
full env, and any partition map, and write them to logs/summary.md.
```

**`04-images.prompt.md` — host-side firmware analysis**
```
firmware/StockRoot_83_IMAGE is present (verify it is exactly 107,934,810 bytes; stop if not).
1. Use refs parse_ota83.py / parseFS.py to describe the 83 container layout; write tools/parse83.py if
   neither handles it cleanly.
2. `binwalk -e` it, mount the squashfs read-only, and extract lib/firmware/mrvl/*, etc/init.d/rcS,
   etc/init.d/rc.sysinit, and any wpa_supplicant/udhcpc config into firmware/extracted/ (gitignored).
3. From refs/invoke-kernel (if present) grep the out-of-tree Marvell driver Makefiles and
   request_firmware() calls; produce a table mapping module name → firmware filename → insmod parameters,
   and check that each firmware file exists in the extracted rootfs.
4. Report whether the kernel .config has CONFIG_USB_GADGET / g_ether (USB-net shortcut for Tier 2).
```

**`05-ram-boot.prompt.md` — Tier 2**
```
Write tools/ramboot-wifi.sh (runs on the device over the tunneled console) and tools/stage-blobs.md
(what to push and how) that replicate exactly what rc.sysinit does for Wi-Fi: stage blobs into
/lib/firmware/mrvl/, insmod mlan then sd8801/sd8887 with the parameters from 04-images, bring mlan0 up,
run wpa_supplicant from a config that is generated at run time from an env var (never stored in the repo),
then udhcpc, then print the IP and `adb connect` line. Use only busybox-compatible shell. Add a
pre-flight that aborts if any command contains "l2nand" or "99".
```

**`06-triage.prompt.md` — when something fails**
```
Here is a failed session log. Classify the failure as one of: (1) never enumerated / cable or port,
(2) stuck after stage N of 5 re-enumerations, (3) tool started but no TCP console, (4) console up but
mload failed, (5) Linux up but no network. For each, give the most likely cause from the upstream
PROJECT_STORY.md / runbook, the one-line check, and the fix. Never suggest a NAND write as a fix.
```

---

## 7. Gaps I could not close from here (Copilot can, since it reads GitHub directly)

- The `coggy9/HKHacking` README and Discussions would not load through my fetch tools. Everything above about them is second-hand via the two downstream repos that cite them. Prompt 01 exists to pull the primary text.
- `docs/REVERSE_ENGINEERING.md` in hk-invoke-arm-flasher (the endpoint/image-request protocol detail) and Aristoddle's `hk_usb_boot.c` / `ram_boot_console.sh` did not load either — same fix.
- Two service-mode entry sequences are published (§2). Both come from people who succeeded; verify which one your unit responds to.
- Whether your 12.2314.0 unit's boot chain differs at all from the 12.2134.0 units the community flashed — unknown; the boot ROM/service mode is below the OTA layer, so it should not matter, but log it.
- Physical UART pad location/voltage and JTAG: unconfirmed anywhere public. Not needed for this plan.

---

## 8. Sources

- Harman: "INVOKE: Final Software Update & Release Notes" (12.2314.0, 2021-09-08) — support.harmanaudio.com … /000018514.html
- Harman International: "Harmon Kardon Invoke Statement" (2020-07-31) — harman.com/India/Pages/Harmon-Kardon-Invoke-Statement.aspx
- Aristoddle/hk-invoke-opensource-speaker — README, docs/firmware-bringup-research-2026-06-22.md, docs/wire-a-new-invoke-runbook.md, docs/setup.md, scripts/hk-invoke/ listing
- jryruegas92/hk-invoke-arm-flasher — README.md (usb_boot_arm, service mode, 5× re-enumeration, telnet 8141, `MV88DE3100|>`, 99_IMAGE warning)
- CaramelKat/PodiumFlashing — README (83_IMAGE extract/modify/rebuild; marvell_flash_tool from Harman's zip)
- tchebb/chromecast-tools — README (Berlin boot-image header mangling; key ID 0x02 NAND / 0x82 USB)
- Courk, "Running Custom Code on a Google Home Mini (Part 1)" — courk.cc (same SoC family; secure-boot and NAND-layout background)
- Libre Wireless LS9AD-AC11DBT datasheet v1.6 (FCC 2ADBM-LS9ADAC11DBT) — usermanual.wiki / fccid.io
- TechInsights DDT-1712-818 (Invoke 6132A teardown listing); fccid.io APIHKINVOKE internal photos; iFixit "Harman Kardon Invoke" device page
- archive.org/details/invoke-kernel (kernel source, 2021-03-19); archive.org/details/HK-Invoke-source-disclosure
- LKML: "ARM: dts: berlin: add BG2CD nodes for USB support" (two ChipIdea controllers; usb1 dual-role)
