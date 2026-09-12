---
title: USB boot and RAM-runtime tooling
description: Host tools for verified yellow-mode U-Boot access and the owned reInvoke RAM lifecycle
ms.date: 2026-09-12
ms.topic: how-to
---

Host-side recovery and RAM-development tooling for reaching U-Boot and loading
the owned runtime over Micro-USB without issuing intentional NAND writes.
Candidate 02's ordinary operation starts from NAND without this helper. The
recovery entry sequence is in
[U-Boot console access](../../docs/uboot-access.md). The older
[service-mode investigation](../../docs/usb-service-mode.md) is historical
failed-attempt evidence. The
[current product contract](../../docs/current-product-contract.md) defines the
runtime assembled here.

Here, “native RAM” means ARM Linux running on the Invoke after a host loads it,
not host-independent NAND startup. The builders require private donor/RC12
artifacts and pinned toolchains; this is not a complete public fresh-clone
firmware kit. Recovering with this helper replaces a native session and loses
its volatile diagnostic state. Do not arm it over a working session without
explicit approval.

Native candidate 02 does not enumerate USB, and default network ADB on TCP
5555 explicitly refused connections while WAMP worked. Host ADB ports
5037/5038 and helper-console port 8141 are different services. USB ADB itself
has no IP port.

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
| `flash-native-once.mjs` | Offline-tested bundle inspector and explicitly gated one-command vendor flash wrapper; first live use installed candidate 03 |
| `flash-native-once-test.mjs` | Focused offline wrapper tests with mocked transport |
| `monitor-descriptors.sh` | Polls sysfs during an attempt and captures every distinct Marvell enumeration |
| `native-ram-init` | Owned PID 1 for volatile filesystems, NAND isolation, USB, radio setup, networking, bounded logs, and supervised product services |
| `uboot-console.py` | Console client for the `usb_boot` TCP relay. Strips telnet negotiation, logs the transcript, and forwards commands from a FIFO |
| `start-session.sh` | Brings up either reviewed boot tool and refuses to run if flashable images or automatic commands are staged |

## Offline-tested native flash wrapper

`flash-native-once.mjs` is a thin host wrapper, not the tool used to establish
candidate 02's native acceptance. Its default action is regular-file-only
inspection:

```bash
node tools/usb-boot/flash-native-once.mjs "<bundle-dir>"
node tools/usb-boot/flash-native-once.mjs "<bundle-dir>" inspect
```

The bundle directory contains the image and `MANIFEST.json` produced by the
native bundle builder. Inspection checks the known vendor source and fixed
boot payloads, nine-record layout and CRCs, selected main rootfs/BSL, image
length, and manifest agreement. Candidate 03 uses the same manifest schema;
schema compatibility is not native boot acceptance.

Offline validation now passes 30 focused tests, including the new controls
below and the real archived candidate 02 bundle/transcript checks.
On 2026-09-12, its first approved live invocation installed candidate 03 and
verified all nine program/read address sets, exact transfer length, known
geometry/bad blocks, and a fresh returned prompt. Validation through helper
shutdown and staging cleanup took 36.819 seconds; command submission through
verified program/read coverage took 32.440 seconds. Neither figure is pure
NAND programming time. The subsequent power-only boot returned the new
Bluetooth identity; USB still did not enumerate and native SSH is unverified.
See [native startup evidence](../../docs/native-nand-platform.md#candidate-03-startup).
One successful run does not authorize another flash or qualify every failure
path, device, or layout.

The initial 21-test checkpoint missed two issues found and fixed during review
before any hardware invocation:

* The wrapper now binds the helper's actual open USB file descriptor to the
  selected physical port and rejects multiple matching recovery devices.
  Matching a descriptor or a supplied path alone was insufficient.
* Growing console logs are read as bounded snapshots. Ordinary appends are not
  mistaken for corruption of an immutable input; strict image/manifest input
  checks remain separate.

### Future explicitly approved flash interface

> [!CAUTION]
> This invokes the vendor whole-good-block erase/program path, not a sparse
> rootfs update. Image 99 is excluded. The acknowledgement below is an error
> guard, not owner approval. Do not run it from this documentation alone.

```bash
node tools/usb-boot/flash-native-once.mjs "<bundle-dir>" flash \
  --expected-sha256 "<reviewed-bundle-sha256>" \
  --confirm ERASE-AND-FLASH-NATIVE \
  --approval-ref "<explicit-owner-approval-reference>" \
  --firmware-dir "<private-firmware-dir>" \
  --session-dir "<private-recovery-session-dir>" \
  --usb-path "<physical-usb-path>" \
  --evidence "<new-private-evidence-dir>"
```

The existing helper must already be recovery-ready. This wrapper does not
start the helper, reset the unit, boot RAM Linux, or perform a native reboot.
It does not alter the existing session-start or helper-readiness implementation.
The operator-first ordering below is for future attended operations, not a request
to start or keep a helper waiting while the owner is absent.
Use the reviewed physical USB path from private operator configuration; never
publish that binding.

For an approved run, the wrapper:

1. Validates the bundle and existing recovery context, requires one matching
   recovery device, and checks the helper's open USB descriptor against the
   selected physical port.
2. Requires fresh `version` and `nandinit` geometry responses.
3. Stages `83_IMAGE` and its correct `07_IMAGE` transfer length.
4. Durably records an intent/no-reissue marker before issuing exactly one
   vendor flash command.
5. Captures timestamps, transfer completion, expected per-record write/read
   loops, errors, and a fresh returned post-flash prompt.
6. Only after completed verification, stops its identified helper/client PIDs,
   removes active `83_IMAGE`, restores `07_IMAGE`, and leaves the owner's
   physical power gate untouched.

For development, keep USB connected to a passive observer during the next
owner-controlled normal boot, with the firmware-loading helper stopped.
Disconnecting USB is a separately chosen host-independence test, not an
established boot requirement or a step to repeat after every flash. A cable
does not launch the helper, but whether USB power/presence affects this unit's
boot selection or initialization remains unknown. Late attachment alone cannot
settle that question. Candidate 03's first test used disconnected USB; its
changed Bluetooth identity proves runtime progress, not early USB behavior.

Intent and evidence require durable private storage, not tmpfs/ramfs. Failure
preserves state and evidence rather than retrying, reflashing, or clearing the
helper. Do not delete the intent marker to turn an uncertain result into
another attempt. Independent review must resolve it first.

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
sudo cp tools/usb-boot/99-marvell-invoke.rules /etc/udev/rules.d/
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
  --source-initramfs "${REINVOKE_ARCHIVE}/extracted/ota2/OTA2/82_IMAGE" \
  --donor-rootfs "${REINVOKE_ARCHIVE}/hardware/dumps/<snapshot>/rootfs-extracted/primary" \
  --kernel-modules "${REINVOKE_ARCHIVE}/build/artifacts/<kernel>/modules" \
  --provisiond "${REINVOKE_ARCHIVE}/build/artifacts/<provisiond>/reinvoke-provisiond" \
  --wifi-applyd "${REINVOKE_ARCHIVE}/build/artifacts/<applyd>/reinvoke-wifi-applyd" \
  --networkd "${REINVOKE_ARCHIVE}/build/artifacts/<networkd>/reinvoke-networkd" \
  --output "${REINVOKE_ARCHIVE}/build/artifacts/<image>/82_IMAGE"
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
RAM speaker path. It checksum-gates owned MCU/DSP and microphone-capture
binaries, the volatile DSP image, BlueZ, BlueALSA, the pairing/HCI helpers, and
an isolated Bonefish/D-Bus runtime. The fixed ALSA capture helper and
`libasound` are also individually pinned. It never copies the full donor
SquashFS. The donor EGLIBC 2.23 libraries remain under `/opt/reinvoke/lib` and
are invoked through their own loader, so they cannot replace the recovery
image's EGLIBC 2.12 libraries.

The builder requires a peer address because the current pairing agent accepts
only one reviewed peer during its bounded window. The generated configuration
and all binaries remain outside Git:

```bash
tools/usb-boot/build-native-runtime.sh \
  --donor-rootfs "${REINVOKE_ARCHIVE}/hardware/dumps/<snapshot>/rootfs-extracted/primary" \
  --mcu-interface <owned-mcu-binary> \
  --dsp-interface <owned-dsp-binary> \
  --mic-capture <owned-microphone-capture-binary> \
  --dsp-image <dsp-img.ldr> \
  --bluetoothd <bluez-5.55-bluetoothd> \
  --bluealsa <bluealsa-4.0.0> \
  --bluealsa-aplay <bluealsa-aplay-4.0.0> \
  --bluealsa-cli <bluealsa-cli-4.0.0> \
  --hci-init <owned-hci-init> \
  --pairing-agent <owned-pairing-agent> \
  --peer-address <allowlisted-peer> \
  --output-dir "${REINVOKE_ARCHIVE}/build/artifacts/reinvoke-native-runtime"
```

### Host-specific values

The allowlisted peer is a Bluetooth address that identifies a particular
machine, so it should not be typed into commands that end up in shell history,
issues or commits. Copy the sample configuration and edit it instead:

```bash
cp tools/usb-boot/local.conf.sample tools/usb-boot/local.conf
```

`local.conf` is ignored by Git. `build-native-runtime.sh` reads
`REINVOKE_PEER_ADDRESS` and `REINVOKE_PAIR_SECONDS` from it, so `--peer-address`
and `--pair-seconds` can then be omitted. An explicit flag still overrides the
file when you need a one-off value.

Note that the peer address is baked into the image at build time and becomes the
only entry in the pairing allowlist. Changing the peer requires a rebuild and a
reboot, which is deliberate: the device cannot be re-targeted at a different
source while it is running.

Pass the resulting directory and the SHA-256 of its `SHA256SUMS` file to the
initramfs builder:

```bash
tools/usb-boot/build-native-initramfs.sh \
  <existing-reviewed-inputs> \
  --runtime-bundle "${REINVOKE_ARCHIVE}/build/artifacts/reinvoke-native-runtime" \
  --runtime-manifest-sha256 <reviewed-manifest-sha256> \
  --output "${REINVOKE_ARCHIVE}/build/artifacts/reinvoke-native/82_IMAGE"
```

When the bundle is present, the builder removes unused recovery graphics/media
payloads and enforces a 60 MiB output budget below the U-Boot overlap limit.
PID 1 starts and supervises the router, MCU, DSP, D-Bus, BlueZ, BlueALSA,
playback, and pairing services. Add `reinvoke.runtime=off`,
`reinvoke.router=off`, `reinvoke.mcu=off`, `reinvoke.dsp=off`, or
`reinvoke.bluetooth=off` to the volatile kernel command line for isolation.

The DSP service exposes seven public WAMP procedures. Raw microphone opcode
`0x09` is available only through `/run/reinvoke/dsp-mic-control.sock`, created
with mode `0600`. The MCU service owns `com.harman.dsp.micMute`, physical
Mic-Mute events, atomic RAM privacy state, retry, and the protected red
indication. DSP restart restores required mute before readiness.

The packaged BlueALSA build includes the six reviewed active-PCM lease, Invoke
ALSA contract, decoded-jitter-buffer, SBC-gap-concealment, short-clip-drain, and
closed-FIFO-drain patches. The playback service emits a RAM-only active-PCM
lease. The MCU policy opens
the physical DAC and amplifier only when the lease thread ID matches ALSA's
`owner_pid`, ALSA reports `RUNNING`, and `/proc/<tid>/exe` identifies the
packaged player. Silence, disconnect, process exit, and shutdown remove
authorization and reassert mute; a 1.5-second holdoff covers brief transport
gaps without flapping the hardware gates.

Service output passes through BusyBox syslog with a 256 KiB active file and one
rotated backup. If syslog is unavailable, services use the bounded kernel log.

## Collect RAM-boot acceptance evidence

These collectors depend on the host-loaded RAM ADB path. They are historical
RAM acceptance tools, not an available way to inspect candidate 02's
unshelled native session. Their device calls are not implied by an offline
build or documentation task.

The packaged `/usr/sbin/reinvoke-acceptance` command is structural smoke only:
runtime hashes, NAND isolation, raw MTD-node removal, radio/audio devices,
service PID files, zombies, and fatal kernel messages. It is not release
acceptance by itself. Collect the complete host-side evidence bundle after each
boot:

```bash
tools/usb-boot/collect-native-acceptance.sh \
  --output-dir "${REINVOKE_ARCHIVE}/hardware/usb-attempts/<timestamp>/acceptance"
```

The collector also calls MCU status, requires DSP `getVer` and version event
`25688`, verifies Mic-Mute and restores the initial privacy state, then retains
post-probe service logs. It exits nonzero after evidence collection if any check
fails.

Physical policy consumes decoded MCU events rather than accepting a WAMP
publication as a physical press. Control and indicator gates need a person at
the speaker. In a separately approved session, run the capture harness:

```bash
tools/usb-boot/collect-physical-controls.sh \
  --duration 180 \
  --output-dir "${REINVOKE_ARCHIVE}/hardware/usb-attempts/<timestamp>/controls"
```

It records rotary `com.harman.test.inputEvent` publications, key
`com.harman.vui.keypress` publications, every Bluetooth state the file reports
during the window, indicator and pairing log lines, and the runtime log for
that window only. It presses nothing and calls no state-changing procedure. Its
summary counts published events rather than subscription confirmations, so a
window with no presses reports `button_publications=0` instead of appearing to
observe one.

For the final STA/uAP gate, start the provisioning collector after ADB returns:

```bash
tools/usb-boot/collect-provisioning-window.sh \
  --output-dir \
    "${REINVOKE_ARCHIVE}/hardware/usb-attempts/<timestamp>/provisioning"
```

It first proves the STA/uAP boot argument, `p2p0`, the window daemon, and its
control socket are ready. The operator then performs one Mic-Mute long press.
The collector waits for the HTTPS descriptor, captures the isolated AP address,
listeners, forwarding state, child processes, storage mounts, and logs, then
waits for the bounded five-minute window to remove its processes and runtime
directory. It never reads AP credentials or submits station credentials. On a
station-only boot it fails before asking for a physical press.

After a capture session reports the live `MV88DE3100|>` prompt, stage and boot
a reviewed native pair with elapsed progress and a bounded USB criterion:

```bash
tools/usb-boot/boot-native-ram.sh \
  --kernel "${REINVOKE_ARCHIVE}/build/artifacts/<kernel>/81_IMAGE" \
  --kernel-sha256 <reviewed-kernel-sha256> \
  --initramfs "${REINVOKE_ARCHIVE}/build/artifacts/<image>/82_IMAGE" \
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

The waiting loader does not keep an absent-device helper alive. The pinned
helper can time out after 120 seconds without an endpoint. Have the operator
and passive observer ready first; start or attach the helper only when the
endpoint is present. A stale relay or waiting status file is not proof that
the helper will catch a future power cycle.

One host-wide loader lock covers staging, waiting, and injection. A second
loader fails before it can replace shared `81_IMAGE`/`82_IMAGE` or send commands.
Operationally, prepare the passive observer and owner-agreed power step first.
Once the endpoint is present, start/attach the helper and verify the live relay
and exactly one loader. Within that explicitly approved RAM-recovery scope,
the armed loader injects as soon as it sees the new U-Boot banner and prompt.

The loader writes its current state to `--status-file`, which defaults to
`${XDG_RUNTIME_DIR:-/tmp}/reinvoke-loader-status`. The file always holds one
timestamped line, so a single `cat` answers whether the yellow-mode window was
caught without watching a long-running log:

```bash
cat "${XDG_RUNTIME_DIR:-/tmp}/reinvoke-loader-status"
```

States progress `staged`, `waiting-for-uboot`, `uboot-acquired`,
`kernel-loading`, `booting`, `adb-ready`, or `failed` with a reason. While the
loader is armed it refreshes `waiting-for-uboot` every fifteen seconds, so a
stale timestamp means the loader died rather than that the window was missed.
An operator therefore never has to guess after a reset: `uboot-acquired` proves
the prompt was caught, and `adb-ready` proves the loader's RAM ADB criterion
passed. Neither proves host-independent NAND startup or full product acceptance.

Children are spawned with the lock descriptor closed. The ADB fork-server
daemonizes and would otherwise inherit that descriptor and hold the lock for the
life of the host session, which made every later loader run fail even though no
loader was running. When the lock is held but no loader process exists, the
loader now reports that stale-descriptor case explicitly instead of claiming a
concurrent run.

The lock file lives beside the staged images, as `.reinvoke-native-loader.lock`
inside the firmware directory. The resources it protects are host-global: the
shared console FIFO, the shared `81_IMAGE` and `82_IMAGE`, and the single USB
device. Keying the lock on the invoking user or on `XDG_RUNTIME_DIR` would let a
`sudo` run and an unprivileged run lock different inodes and interleave
`usbload` commands, which is the exact corruption the singleton exists to stop.

Before it arms, the loader also confirms a console relay still holds the command
FIFO open. The FIFO and the console log both survive as files after a capture
session exits, so their presence alone cannot prove the loader would catch
anything. Without that check an operator could reset into yellow mode against a
dead session while the loader waited forever.

For historical donor-comparison work only, ADB can start the old minimum
diagnostic graph:

```bash
tools/usb-boot/start-native-services.sh \
  --rootfs "${REINVOKE_ARCHIVE}/hardware/dumps/<snapshot>/installed-rootfs-region.bin"
```

That historical launcher checksum-gates the block-aligned rootfs carve, mounts its host copy
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

The resulting binary is x86-64 on the test workstation despite its upstream
`usb_boot_arm` name. It has not been run on Raspberry Pi hardware.

## Capture one attempt

Identify the connector's host USB bus with `lsusb -t`. Capture only that bus to
avoid recording unrelated traffic from other USB buses:

```bash
INVOKE_USBMON_INTERFACE=usbmonN \
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
`${REINVOKE_ARCHIVE}/hardware/usb-attempts/`. It contains:

* Attempt metadata and hashes
* USB topology
* Bus-specific usbmon pcap
* Kernel messages
* ADB device transitions
* Every observed Marvell descriptor
* Boot-tool and console logs when applicable

Use the bus-specific `usbmonN` that matches `lsusb -t`.
The script refuses `usbmon0` because it would capture every USB bus.

## Direct session startup

`start-session.sh` remains available when packet capture is not needed. It
reports `READY` only after the selected tool enters its device polling loop:

```bash
INVOKE_FIRMWARE_DIR=../invoke-boot tools/usb-boot/start-session.sh stock
```

Prepare the operator and passive observer before this step, and start/attach
only when the endpoint is present. The helper's 120-second absent-device
timeout is independent of any loader wait. Do not compensate with an
unapproved reset or automatic helper restart.

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

## Historical autoboot-countdown experiment

`interrupt-autoboot.py` feeds harmless newlines into the console FIFO so
keystrokes are already waiting when the brief USB window opens. Start it, then
power cycle:

```bash
python3 tools/usb-boot/interrupt-autoboot.py 180
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

The served boot chain loads into RAM; complete absence of autonomous
device-side writes has not been proved by this procedure. Avoid `nand`,
`nandinit`, `nanderase`, `tftp2nand`, `l2nand`, and `saveenv` at the prompt on a
working unit.
