---
title: USB boot session tooling
description: Host tools for observing the Invoke Marvell boot endpoint without known NAND writes
ms.date: 2026-09-03
ms.topic: how-to
---

Host-side tooling for observing the Marvell boot/download endpoint on the Harman
Kardon Invoke over Micro-USB. Verified against hardware on 2026-09-01.
Background and the
service-mode entry sequence are in [../../docs/usb-service-mode.md](../../docs/usb-service-mode.md).

No proprietary files are included here. The Marvell `usb_boot` binary and the
boot-chain images come from the community flashing bundle and must be staged
separately.

## Contents

| File | Purpose |
|---|---|
| `99-marvell-invoke.rules` | udev rule granting unprivileged access to the Marvell boot endpoint, and triggering descriptor capture on attach |
| `attach-console.sh` | Waits for either boot tool to open its TCP console before attaching the console client |
| `boot-native-ram.sh` | Stages checksum-gated kernel and initramfs payloads, sends only volatile U-Boot commands, and reports bounded boot progress |
| `build-arm-flasher.sh` | Builds the pinned `jryruegas92` implementation natively from its preserved Git mirror |
| `capture-attempt.sh` | Creates a timestamped evidence bundle containing usbmon, kernel, ADB, descriptor, protocol, and console logs |
| `capture-descriptor.sh` | Dumps the full USB descriptor when the device appears. The boot window is only a few seconds, too short to run `lsusb -v` by hand |
| `build-native-initramfs.sh` | Builds a sanitized RAM-only initramfs from reviewed held artifacts |
| `monitor-descriptors.sh` | Polls sysfs during an attempt and captures every distinct Marvell enumeration |
| `native-ram-init` | Replacement PID 1 for root ADB, read-only NAND access, and SD8887 Wi-Fi bring-up |
| `uboot-console.py` | Console client for the `usb_boot` TCP relay. Strips telnet negotiation, logs the transcript, and forwards commands from a FIFO |
| `start-session.sh` | Brings up either reviewed boot tool and refuses to run if flashable images or automatic commands are staged |

## Setup

Install the host packages and load usbmon:

```bash
sudo apt-get update
sudo apt-get install -y adb android-sdk-platform-tools-common \
  build-essential pkg-config libusb-1.0-0-dev tcpdump usbutils \
  wireshark-common
sudo modprobe usbmon
```

The Wireshark setup grants the `wireshark` group permission to use `dumpcap`,
which has the required packet-capture capabilities. The capture launcher uses
`dumpcap` directly; it does not require passwordless sudo access to arbitrary
packet-capture commands.

Install the repository's usbmon device rule so the kernel capture nodes are
also readable by that group:

```bash
sudo cp tools/usb-boot/70-usbmon-wireshark.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules
sudo udevadm trigger --subsystem-match=usbmon
```

Install the udev rule once:

```bash
sudo cp 99-marvell-invoke.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules
```

The rule also runs the staged copy of `capture-descriptor.sh` on attach, which
appends to `/tmp/invoke-descriptor.log`. Per-attempt capture does not rely on
that transient file; `monitor-descriptors.sh` writes directly into the evidence
bundle.

Stage a sibling directory at `../invoke-boot` containing `usb_boot`,
`bcm_erom.bin.usb`, `bootloader.img`, `sysinit.img`, `drm_erom.img`, the
numbered protocol files, and a comment-only `79_IMAGE`. Override it with
`INVOKE_FIRMWARE_DIR` when needed.

## Build the native RAM initramfs

The native RAM platform uses proprietary recovery and board-firmware inputs
from the external archive. The builder verifies the reviewed OTA2 recovery
initramfs, replaces PID 1, removes the vendor flash launcher, and injects the
SD8887 Wi-Fi and Bluetooth firmware plus board calibration from an extracted
donor rootfs:

```bash
tools/usb-boot/build-native-initramfs.sh \
  --source-initramfs ../reinvoke-archive/extracted/ota2/OTA2/82_IMAGE \
  --donor-rootfs ../reinvoke-archive/hardware/dumps/20260902T215700Z-native-ram/rootfs-extracted/primary \
  --kernel-modules ../reinvoke-archive/build/artifacts/invoke-kernel-gcc49-audio-sd8887-20260903/modules \
  --provisiond ../reinvoke-archive/build/artifacts/reinvoke-provisiond-20260903/reinvoke-provisiond \
  --wifi-applyd ../reinvoke-archive/build/artifacts/reinvoke-wifi-applyd-20260903/reinvoke-wifi-applyd \
  --networkd ../reinvoke-archive/build/artifacts/reinvoke-networkd-20260903/reinvoke-networkd \
  --output ../reinvoke-archive/build/artifacts/invoke-native-ram-audio-sd8887-networkd-20260903/82_IMAGE.reinvoke-audio-sd8887-networkd
```

The builder normalizes archive metadata so identical reviewed inputs produce
byte-identical output. It checksum-gates the reviewed daemon binaries and kernel
module tree, then strips host-only `build` and `source` symlinks from every
packaged module release. The generated file remains outside Git. See
[native-ram-platform.md](../../docs/native-ram-platform.md) for the verified
U-Boot load addresses, runtime evidence, component audit, and safety boundary.
See [Invoke kernel build](../kernel/README.md) for the replacement-kernel
source, compatibility patch, and artifact pipeline.

When the network daemon is packaged, PID 1 starts it by default and restarts it
after failures with a five-second delay. Add `reinvoke.networkd=off` to the
kernel command line for manual network bring-up or failure isolation.

When the supplied module tree includes the repository-built `bt8xxx.ko`, PID 1
loads it with the stock volatile firmware parameters. This creates `hci0`
without changing module metadata or starting a pairing service.

The optional provisioning and Wi-Fi apply daemons are independently
checksum-gated and installed but never auto-started. When included, the
checksum-gated network lifecycle service starts at boot and waits for a
root-controlled station supplicant before acquiring DHCP state. See
[Native Wi-Fi provisioning boundary](../../docs/native-provisioning.md).

## Build the autonomous runtime bundle

`build-native-runtime.sh` assembles only the services required by the owned
RAM speaker path. It checksum-gates owned MCU/DSP binaries, the volatile DSP
image, BlueZ, BlueALSA, the pairing/HCI helpers, and an isolated Bonefish/D-Bus
runtime. It never copies the full donor SquashFS. The donor EGLIBC 2.23
libraries remain under `/opt/reinvoke/lib` and are invoked through their own
loader, so they cannot replace the recovery image's EGLIBC 2.12 libraries.

The builder requires a peer address because the current pairing agent accepts
only one reviewed peer during its bounded window. The generated configuration
and all binaries remain outside Git:

```bash
tools/usb-boot/build-native-runtime.sh \
  --donor-rootfs ../reinvoke-archive/hardware/dumps/<dump>/rootfs-extracted/primary \
  --mcu-interface <owned-mcu-binary> \
  --dsp-interface <owned-dsp-binary> \
  --dsp-image <dsp-img.ldr> \
  --bluetoothd <bluez-5.55-bluetoothd> \
  --bluealsa <bluealsa-4.0.0> \
  --bluealsa-aplay <bluealsa-aplay-4.0.0> \
  --bluealsa-cli <bluealsa-cli-4.0.0> \
  --hci-init <owned-hci-init> \
  --pairing-agent <owned-pairing-agent> \
  --peer-address <allowlisted-peer> \
  --output-dir ../reinvoke-archive/build/artifacts/reinvoke-native-runtime
```

Pass the resulting directory and the SHA-256 of its `SHA256SUMS` file to the
initramfs builder:

```bash
tools/usb-boot/build-native-initramfs.sh \
  <existing-reviewed-inputs> \
  --runtime-bundle ../reinvoke-archive/build/artifacts/reinvoke-native-runtime \
  --runtime-manifest-sha256 <reviewed-manifest-sha256> \
  --output ../reinvoke-archive/build/artifacts/reinvoke-native/82_IMAGE
```

When the bundle is present, the builder removes unused recovery graphics/media
payloads and enforces a 60 MiB output budget below the U-Boot overlap limit.
PID 1 starts and supervises the router, MCU, DSP, D-Bus, BlueZ, BlueALSA,
playback, and pairing services. Add `reinvoke.runtime=off`,
`reinvoke.router=off`, `reinvoke.mcu=off`, `reinvoke.dsp=off`, or
`reinvoke.bluetooth=off` to the volatile kernel command line for isolation.

The playback service emits a RAM-only active-PCM lease. The MCU policy opens
the physical DAC and amplifier only when the lease thread ID matches ALSA's
`owner_pid`, ALSA reports `RUNNING`, and `/proc/<tid>/exe` identifies the
packaged player. Silence, disconnect, process exit, and shutdown remove
authorization and reassert mute.

Service output passes through BusyBox syslog with a 256 KiB active file and one
rotated backup. If syslog is unavailable, services use the bounded kernel log.

## Collect autonomous acceptance evidence

The packaged `/usr/sbin/reinvoke-acceptance` command checks runtime hashes,
NAND isolation, raw MTD-node removal, radio/audio devices, service PID files,
zombies, and fatal kernel messages. Collect a complete host-side evidence
bundle after each boot:

```bash
tools/usb-boot/collect-native-acceptance.sh \
  --output-dir ../reinvoke-archive/hardware/usb-attempts/<timestamp>/acceptance
```

The collector also calls the owned MCU and DSP WAMP surfaces and retains all
service logs. It exits nonzero after evidence collection if any check fails.

After a capture session reports the live `MV88DE3100|>` prompt, stage and boot
a reviewed native pair with elapsed progress and a bounded USB criterion:

```bash
tools/usb-boot/boot-native-ram.sh \
  --kernel ../reinvoke-archive/build/artifacts/invoke-native-audio/81_IMAGE.reinvoke-audio \
  --kernel-sha256 <reviewed-kernel-sha256> \
  --initramfs ../reinvoke-archive/build/artifacts/invoke-native-ram-audio/82_IMAGE.reinvoke-audio \
  --initramfs-sha256 <reviewed-initramfs-sha256> \
  --wait-for-prompt \
  --adb-server-port 5038
```

The loader verifies the kernel's `0x02008000` load and entry address, rejects
staged `83_IMAGE` and `99_IMAGE`, and sends only `usbload`, `set bootargs`, and
`bootm`. Use `--wait-for-prompt` to keep the checksum-gated loader armed until
yellow-mode U-Boot appears, without imposing an operator timeout. Use
`--prepare-only` to validate and stage without touching the live console. While
`capture-attempt.sh` owns the USB interface, pass its isolated ADB server port
to the loader. The default capture port is 5038.

After ADB returns, start the minimum diagnostic service graph:

```bash
tools/usb-boot/start-native-services.sh \
  --rootfs ../reinvoke-archive/hardware/dumps/20260902T215700Z-native-ram/installed-rootfs-region.bin
```

The launcher checksum-gates the block-aligned rootfs carve, mounts its host copy
read-only from RAM, starts only Bonefish and the MCU, DSP, audio, source, and
optional Bluetooth adapters, and initializes music volume at 20 percent. DSP
startup is disabled by default because its normal boot event transiently
unmutes the amplifier and DAC before the launcher can reassert mute. Pass
`--start-dsp` only for attended audio work and `--pair` only for a bounded
pairing window. The launcher never starts the stock supervisor or updater.

## Pinned open-source tool

The `jryruegas92/hk-invoke-arm-flasher` mirror is pinned in
`metadata/P2-003.json` at commit
`63444e82cc5274abe31ec49ad55ee552b50b64b3`. Build that exact source for the
current Linux host:

```bash
tools/usb-boot/build-arm-flasher.sh
```

The resulting binary is x86-64 on this Mac mini despite its upstream
`usb_boot_arm` name. It has not been run on Raspberry Pi hardware.

## Capture one attempt

The previously used connector appears on USB bus 3. Capture only that bus to
avoid recording unrelated traffic from other USB buses:

```bash
INVOKE_USBMON_INTERFACE=usbmon3 \
  tools/usb-boot/capture-attempt.sh normal-boot passive
```

The command uses `dumpcap` to open the usbmon capture; the boot tools, descriptor
monitor, console client, and ADB client remain unprivileged. Press Ctrl-C after
the physical attempt. A hard timeout stops packet capture after ten minutes by
default; set
`INVOKE_CAPTURE_LIMIT_SECONDS` to a value from 60 through 3600 when a
different bound is required.

The ADB observer uses an isolated host server on port 5038 and shuts down only
that server during cleanup. Set `INVOKE_ADB_SERVER_PORT` if 5038 is already in
use.

Available modes:

| Mode | Behavior |
|---|---|
| `passive` | Observe a normal boot or button mode without sending USB protocol data |
| `original-stock` | Run Harman's original tool and serve the stock `08_IMAGE` |
| `original-absent` | Run Harman's original tool without `08_IMAGE` |
| `arm-stock` | Run the pinned open-source implementation and serve stock `08_IMAGE` |
| `arm-absent` | Run the pinned open-source implementation without `08_IMAGE` |

Each run creates a private directory under
`../reinvoke-archive/hardware/usb-attempts/`. It contains:

* Attempt metadata and hashes
* USB topology
* Bus-specific usbmon pcap
* Kernel messages
* ADB device transitions
* Every observed Marvell descriptor
* Boot-tool and console logs when applicable

Use `usbmon2` instead when testing a connector that `lsusb -t` places on bus 2.
The script refuses `usbmon0` because it would capture every USB bus.

## Direct session startup

`start-session.sh` remains available when packet capture is not needed. It
reports `READY` only after the selected tool enters its device polling loop:

```bash
INVOKE_FIRMWARE_DIR=../invoke-boot tools/usb-boot/start-session.sh stock
```

The argument controls what is served for image type `0x08`:

| Variant | Effect |
|---|---|
| `stock` (default) | Serve the bundle's `08_IMAGE` |
| `absent` | Remove `08_IMAGE` so the request cannot be satisfied |

The original is preserved as `08_IMAGE.stock` on first run and restored by a
`stock` run. The launcher does not stage known NAND images or automatic
commands. Complete absence of device-side persistent writes has not been
verified by before-and-after storage capture.

## Reading the session

Use the paths printed by `capture-attempt.sh`. During a direct session, logs are
written in the firmware staging directory and `/tmp/uboot.log` points to the
console transcript.

Send a command to the prompt:

```bash
echo 'printenv' > /tmp/uboot_cmd
```

## Interrupting an autoboot countdown

`interrupt-autoboot.py` feeds harmless newlines into the console FIFO so
keystrokes are already waiting when the brief USB window opens. Start it, then
power cycle:

```bash
python3 interrupt-autoboot.py 180
```

This tests whether the device's request for image type `0x08` comes from a
running U-Boot executing `usbload 8`. A completed interrupt transfer produced
no visible response on this unit; that result does not prove firmware consumed
the byte or that it reached the relevant timing window.

## Why the console client is required

The original `usb_boot` blocks at `wait for connection on port: 8141` and does
not watch USB until a client attaches. Started without one, it silently misses
the boot window. The stock `run.sh` masks this by launching PuTTY as a fifth
argument.

`usb_boot` also answers every telnet `DONT` and `WONT` with a further
negotiation command, so a client that replies will loop indefinitely.
`uboot-console.py` consumes those sequences without responding.

The pinned open-source implementation does the reverse: it starts polling
before its console port exists, then opens the port after a device reaches its
request-serving phase. `attach-console.sh` handles both orderings.

## Safety

`start-session.sh` aborts if `83_IMAGE` or `99_IMAGE` is present or if
`79_IMAGE` contains a non-comment command. The first is used by the vendor NAND
workflow; the second is reported to brick the unit unrecoverably.

The boot chain loads into RAM and writes nothing on its own. Avoid `nand`,
`nandinit`, `nanderase`, `tftp2nand`, `l2nand`, and `saveenv` at the prompt on a
working unit.
