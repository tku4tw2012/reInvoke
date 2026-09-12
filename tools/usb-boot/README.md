---
title: USB boot and RAM-runtime tooling
description: Recovery sessions, gated RAM composition and loading, and native flash inspection
ms.date: 2026-09-12
ms.topic: how-to
---

These host tools support [U-Boot recovery](../../docs/uboot-access.md),
RAM development and evidence capture. "Native RAM" means ARM Linux loaded by
a host, not autonomous NAND startup. Commands use the repository root;
`REINVOKE_ARCHIVE` identifies retained private inputs and outputs.
The public clone includes neither donor payloads nor all pinned build inputs.

Recovery replaces the current session and loses volatile credentials, bonds
and diagnostics. Native candidates 02/03 did not enumerate USB; network ADB
5555 refused connections. Host ADB 5037/5038 and helper console 8141 are not
device administration ports. Candidate evidence belongs in the
[native guide](../../docs/native-nand-platform.md).

## Recovery session inputs

Host tools include ADB, libusb/build tools, usbutils and, for capture, usbmon
with `dumpcap`. The repository's `70-usbmon-wireshark.rules` grants the
capture group access; `99-marvell-invoke.rules` grants boot-endpoint access
and invokes a staged descriptor recorder. Per-attempt capture records
descriptors independently.

The private firmware directory contains `usb_boot`, `bcm_erom.bin.usb`,
`bootloader.img`, `sysinit.img`, `drm_erom.img`, numbered protocol files and
comment-only `79_IMAGE`. Set `INVOKE_FIRMWARE_DIR`; default is sibling
`../invoke-boot`. `start-session.sh` refuses `83_IMAGE`, `99_IMAGE` or
non-comment commands in `79_IMAGE`.

The alternative host helper is pinned at
`jryruegas92/hk-invoke-arm-flasher` commit
`63444e82cc5274abe31ec49ad55ee552b50b64b3`
([P2-003](../../metadata/P2-003.json)):

```bash
tools/usb-boot/build-arm-flasher.sh
```

It builds for the current host; upstream name `usb_boot_arm` does not imply
the x86-64 workstation output is an ARM binary or Raspberry Pi-tested.

## Observe or start a session

Capture only the connector's `usbmonN` bus:

```bash
INVOKE_USBMON_INTERFACE=usbmonN \
  tools/usb-boot/capture-attempt.sh normal-boot passive
```

| Mode                                | Firmware-serving behavior                         |
| ----------------------------------- | ------------------------------------------------- |
| `passive`                           | No helper; descriptor/ADB queries still occur     |
| `original-stock`, `original-absent` | Vendor helper with/without `08_IMAGE`             |
| `arm-stock`, `arm-absent`           | Pinned open-source helper with/without `08_IMAGE` |

`usbmon0` is rejected. Capture defaults to 600 seconds;
`INVOKE_CAPTURE_LIMIT_SECONDS` accepts 60-3600. Evidence includes topology,
pcap, kernel/ADB transitions, descriptors, hashes and any helper/console logs
under the private archive's `hardware/usb-attempts/`. Capture cleanup stops
only its isolated ADB server, default 5038 (`INVOKE_ADB_SERVER_PORT` override).
USB traffic may include other devices on that bus; retain it privately.

Without packet capture:

```bash
INVOKE_FIRMWARE_DIR="<private-firmware-dir>" tools/usb-boot/start-session.sh stock
```

`stock` serves `08_IMAGE`; `absent` withholds it. The first run preserves
`08_IMAGE.stock`, restored by a later stock run. READY means helper polling,
not U-Boot acquisition. The helper can time out after 120 seconds without an
endpoint; start it when the endpoint is present. An armed loader does not
extend helper lifetime.

The original helper waits for a TCP-8141 client before polling USB; the
open-source helper opens its console after reaching request service.
`attach-console.sh` handles both orders. `uboot-console.py` consumes telnet
negotiation without replying, avoiding the vendor helper's negotiation loop.
Use the printed active FIFO for queries, not a regular file. Stale FIFO/log
files do not prove a live relay.

Ordinary session tools intentionally avoid NAND writes, but complete absence
of autonomous device-side writes is not established. Do not issue `nand`,
`nandinit`, `nanderase`, `tftp2nand`, `l2nand` or `saveenv` during routine
recovery. Successful past recovery is not a guarantee after arbitrary
boot-chain corruption.

## Build the autonomous runtime bundle

`build-native-runtime.sh` gates owned MCU/DSP/capture binaries, DSP image,
BlueZ/BlueALSA, helpers, Bonefish/private D-Bus, donor `arecord` and
`libasound`. It does not copy the full donor rootfs. Donor EGLIBC 2.23 stays
under `/opt/reinvoke/lib` with its own loader, separate from recovery EGLIBC
2.12.

```bash
tools/usb-boot/build-native-runtime.sh \
  --donor-rootfs "<extracted-donor-rootfs>" \
  --mcu-interface "<owned-mcu-binary>" --dsp-interface "<owned-dsp-binary>" \
  --mic-capture "<owned-microphone-capture-binary>" --dsp-image "<dsp-img.ldr>" \
  --bluetoothd "<bluez-5.55-bluetoothd>" --bluealsa "<bluealsa-4.0.0>" \
  --bluealsa-aplay "<bluealsa-aplay-4.0.0>" --bluealsa-cli "<bluealsa-cli-4.0.0>" \
  --hci-init "<owned-hci-init>" --media-control "<owned-media-control>" \
  --pairing-agent "<owned-pairing-agent>" --peer-address "<allowlisted-peer>" \
  --output-dir "<new-runtime-bundle>"
```

Ignored `local.conf`, based on `local.conf.sample`, can supply
`REINVOKE_PEER_ADDRESS` and `REINVOKE_PAIR_SECONDS` instead of
`--peer-address`/`--pair-seconds`; flags override it. The peer is compiled into
the image configuration, so changing it requires rebuild/reboot.
For provisioning supply private `--provision-ap-ssid-file` and
`--provision-ap-psk-file`. Repeated `--wamp-allow-cidr` permits explicit IPv4
peers; absent an allowlist, WAMP is not a general network service.
Keep filled configuration and generated bundles private.

## Build the native RAM initramfs

The builder replaces recovery PID 1, removes its vendor flash launcher, injects
donor firmware/calibration and verifies daemon/module/runtime pins:

```bash
tools/usb-boot/build-native-initramfs.sh \
  --source-initramfs "<retained-ota2-82_IMAGE>" \
  --donor-rootfs "<extracted-donor-rootfs>" --kernel-modules "<accepted-modules>" \
  --provisiond "<reinvoke-provisiond>" --wifi-applyd "<reinvoke-wifi-applyd>" \
  --networkd "<reinvoke-networkd>" --windowd "<reinvoke-provision-windowd>" \
  --runtime-bundle "<new-runtime-bundle>" \
  --runtime-manifest-sha256 "<sha256-of-runtime-SHA256SUMS>" \
  --output "<new-image-dir>/82_IMAGE"
```

Archive metadata is normalized; module `build`/`source` host symlinks are
removed. With a runtime bundle, unused recovery graphics/media are removed
and output is capped at 60 MiB below the U-Boot overlap limit.
Kernel profiles are in the [kernel builder](../kernel/README.md).

PID 1 supervises the owned services and bounded syslog (256 KiB plus one
backup, kernel-log fallback). Optional networkd starts at boot and retries
after five seconds; provisioning/apply children start through windowd, not
directly at boot. Mic-Mute long press opens a window only in STA/uAP mode.
A matching packaged `bt8xxx.ko` loads firmware and creates HCI, not a pairing
window by itself.

Isolation boot arguments are `reinvoke.runtime=off`, `reinvoke.router=off`,
`reinvoke.mcu=off`, `reinvoke.dsp=off`, `reinvoke.bluetooth=off` and
`reinvoke.networkd=off`. Speaker/privacy policy is specified in the
[speaker](../../docs/emulation/owned-speaker-control.md) and
[microphone](../../docs/microphone-capture.md) references.

## Load a reviewed RAM image

With a live recovery relay and fresh `MV88DE3100|>` prompt:

```bash
tools/usb-boot/boot-native-ram.sh \
  --kernel "<reviewed-81_IMAGE>" --kernel-sha256 "<reviewed-kernel-sha256>" \
  --initramfs "<reviewed-82_IMAGE>" --initramfs-sha256 "<reviewed-initramfs-sha256>" \
  --wait-for-prompt --adb-server-port 5038 --status-file "<private-status-file>"
```

The loader checks kernel load/entry `0x02008000`, rejects staged `83_IMAGE`
and `99_IMAGE`, and sends only `usbload`, `set bootargs` and `bootm`.
`--prepare-only` validates/stages without console access.
Station-only is default; `--wifi-mode sta-uap` selects provisioning support.
`--wait-for-prompt` has no operator timeout, but still needs a live helper.
Use the capture session's isolated ADB port.

One firmware-directory `.reinvoke-native-loader.lock` covers shared staging,
waiting and injection across users. A second loader fails before replacing
images. Child processes close its descriptor; a stale inherited lock is
reported separately. The loader verifies that a relay actually holds the
console FIFO open.

Status progresses `staged`, `waiting-for-uboot`, `uboot-acquired`,
`kernel-loading`, `booting`, `adb-ready`, or `failed`. Waiting refreshes every
15 seconds; stale timestamps indicate a dead loader. `adb-ready` proves only
the RAM ADB criterion, not native startup or full runtime acceptance.

## RAM acceptance collectors

These require the RAM ADB path, not an unshelled native session:

```bash
tools/usb-boot/collect-native-acceptance.sh --output-dir "<private-acceptance-output>"
tools/usb-boot/collect-physical-controls.sh --duration 180 \
  --output-dir "<private-controls-output>"
tools/usb-boot/collect-provisioning-window.sh --output-dir "<private-window-output>"
```

The first collects structural smoke, calls MCU status/DSP version `25688`,
changes Mic-Mute and attempts to restore its initial state; failure exits
nonzero after collection. Use an attended target. The packaged
`reinvoke-acceptance` alone is structural smoke, not release acceptance.

The controls collector calls no setters and counts physical event publications,
not subscription confirmations. The provisioning collector first checks
STA/uAP, `p2p0`, windowd/socket, then waits for a physical Mic-Mute long press
and bounded five-minute teardown. It reads no AP credentials and submits none.
Station-only fails before a press. Keep diagnostics private.

For donor comparison only, `start-native-services.sh --rootfs <carve>` gates a
retained block-aligned rootfs and avoids the stock supervisor/updater.
Optional `--start-dsp` downloads firmware and can transiently unmute before
remuting; `--pair` opens pairing. These are attended legacy probes, not the
owned runtime startup path.

## Offline-tested native flash wrapper

Default `flash-native-once.mjs` operation inspects regular files only:

```bash
node tools/usb-boot/flash-native-once.mjs "<bundle-dir>" inspect
```

It checks `MANIFEST.json`, vendor source/fixed boot payloads, nine-record
layout/CRCs, main/BSL identity, image length and manifest agreement.
Offline tests include mocks and archived candidate-02 replay.
Its first live use installed candidate 03; programming coverage is not native
service acceptance.

### Destructive flash interface

> [!CAUTION]
> This established interface issues `l2nand 83`, erasing all good NAND blocks
> before reprogramming listed records, including retained boot-chain payloads.
> Omitted factory/`fw_stat`/tail content is not preserved. Use only a separately
> reviewed image in an attended recovery/write procedure. `99_IMAGE` is excluded.

```bash
node tools/usb-boot/flash-native-once.mjs "<bundle-dir>" flash \
  --expected-sha256 "<reviewed-bundle-sha256>" --confirm ERASE-AND-FLASH-NATIVE \
  --approval-ref "<explicit-owner-approval-reference>" \
  --firmware-dir "<private-firmware-dir>" --session-dir "<private-recovery-session-dir>" \
  --usb-path "<physical-usb-path>" --evidence "<new-private-evidence-dir>"
```

The helper must already be recovery-ready. The wrapper does not start it,
reset the unit, boot RAM or initiate native reboot. It requires exactly one
matching device and binds the helper's actual USB file descriptor to the
selected physical port. Fresh `version`/`nandinit` responses establish geometry.
It stages `83_IMAGE`/correct `07_IMAGE` length and durably records intent before
issuing exactly one command.

Verification requires transfer length, all expected per-record program/read
address sets, error checks and a fresh returned prompt. Growing logs are read
as bounded snapshots; image/manifest immutability checks remain separate.
Only success stops identified helper/client PIDs, removes active `83_IMAGE`
and restores `07_IMAGE`. Failure preserves state/evidence, never retries or
clears an uncertain operation. Intent/evidence must be on durable storage,
not tmpfs/ramfs; do not delete the no-reissue marker to retry.

For passive normal-boot observation, USB may stay connected with the helper
stopped. A disconnected start separately tests host independence; cable-only
influence on boot remains unknown. The wrapper leaves physical power control
untouched.
