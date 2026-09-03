---
title: Bluetooth stack
description: Evidence and emulation boundaries for the Invoke Bluedroid stack
ms.date: 2026-09-02
ms.topic: concept
---

The examined Bluetooth service in the final firmware does not use the BlueZ
user-space path (`bluetoothd`/D-Bus). Recording this because earlier documents
in this repository treated BlueZ as the missing component, which shaped a wrong
conclusion about what emulation would require.

## What the firmware actually uses

`usr/bin/bluetooth` links `libhardware.so`, `libcutils.so`, `libutils.so`, and
`libasound.so.2`. It links neither `libbluetooth` nor `libdbus`. Its error
vocabulary is `HCI_ERR_*`, and it references `[BT][BluedroidCall]` and
`/data/misc/bluedroid/.a2dp_data`.

The stack is Bluedroid, Android's Bluetooth implementation, loaded through the
Android hardware abstraction layer:

| Component | Path |
|---|---|
| HAL module | `system/lib/hw/bluetooth.default.so` |
| Vendor library | `system/lib/libbt-vendor.so` |
| Kernel driver | `lib/modules/3.8.13-yocto-standard/.../bt_sd8887/bt8xxx.ko` |
| Controller firmware | `lib/firmware/mrvl/sd8887_bt_a2_new.bin` |
| Stack configuration | `etc/bluetooth_orig/bt_stack.conf` |

`usr/bin/bluetooth.sh` copies `etc/bluetooth_orig` to `/data/bluetooth`, inserts
`bt8xxx.ko` with `fw_name=mrvl/sd8887_bt_a2_new.bin`, then starts the service.

D-Bus is present in the rootfs and used elsewhere in the product, but the
Bluetooth service does not depend on it.

## Evidence classification

Verified facts:

* `usr/bin/bluetooth` and its linked libraries are present in the held final
  firmware rootfs.
* The service references Bluedroid strings, `/data/misc/bluedroid/.a2dp_data`,
  `/dev/rfkill`, and HCI error vocabulary.
* The rootfs carries the Marvell SDIO Bluetooth driver and controller firmware
  listed above.

Artifact-backed findings:

* The emulation boundary is below the WAMP procedure layer and above or at the
  kernel Bluetooth subsystem: the service needs an HCI interface and rfkill
  behaviour, not a BlueZ daemon.

Inference:

* A host-created virtual HCI adapter is the plausible next emulation substitute.
  That has not yet been tested with this Bluedroid stack.

## The real emulation boundary

`libbt-vendor.so` opens `/dev/rfkill` and waits for an `hci%d` interface. That
places the boundary at the kernel Bluetooth subsystem: an HCI device plus
`/dev/rfkill`, reached through `AF_BLUETOOTH` sockets rather than a session bus.

Consequences for the sandbox:

* `bluetoothd` and a D-Bus session would not help, because nothing calls them.
* The Marvell SDIO driver cannot load, because `qemu-user` runs no guest kernel.
* A virtual HCI adapter from the host's `hci_vhci` module is the plausible
  substitute to test, since Bluedroid speaks HCI directly.

## Experimental HCI management shim

`tools/emulation/invoke-ioctl-shim.c` can now return one synthetic, active
`hci0` device for `HCIGETDEVLIST`, `HCIGETDEVINFO`, and `HCIDEVUP`. The library
compiles for ARM against `GLIBC_2.4`.

This is management-plane scaffolding only. It does not emulate HCI commands,
events, ACL traffic, pairing, media transport, or `/dev/rfkill`. No Bluetooth
procedure has completed through this shim.

## Physical RAM-native validation

The recovery kernel identifies the SD8887 combo device as Marvell SDIO
functions `02df:9135`, `02df:9136`, and `02df:9137`. Its Wi-Fi module binds
function `9135`; the installed `bt8xxx.ko` module aliases function `9136`.

The installed Bluetooth module has no symbol-version section but carries
`vermagic` for `3.8.13-yocto-standard`. A temporary copy changed only that
metadata to `3.8.13-mrvl`. Loaded into the ephemeral recovery kernel, it:

* Downloaded 272,656 bytes from `sd8887_bt_a2_new.bin`
* Reported `BT FW is active(2)`
* Created `hci0`
* Created an rfkill entry
* Applied the temporary address `02:52:49:4e:56:02`

The first observed HCI command `0x080f` timed out. The installed Bluedroid
service nevertheless joined the RAM-owned Bonefish router and registered its
pairing, media transport, and device-name procedures using an empty RAM-only
`/data` tree. A read-only `deviceNameGet` call reached the service but did not
return before timeout.

This establishes native transport registration and userspace startup. It does
not yet establish pairing, A2DP audio, or reliable HCI command completion.

A later GCC 4.9 audio-kernel run removed the temporary-module limitation. The
Invoke GPL module was built with exact `3.8.13-reinvoke-audio` vermagic and
loaded without metadata editing. Its SHA-256 is
`b77adca16d3c2778a047243f824b8fea339603343c88da32ab4c42e952bbd522`.

The matching module and RAM-only Bluedroid stack then:

* Reported `BT FW is active(2)`
* Created `hci0` and an unblocked Bluetooth rfkill device
* Enabled the adapter with its controller-provided local address
* Initialized A2DP Sink, AVRCP Controller, and AVRCP Target with result `0`
* Entered connectable mode
* Entered discoverable pairing mode through `com.harman.bluetoothPairing`

Bluedroid normally calls `com.harman.identifiersGet`, a procedure owned by the
broad stock supervisor. Static analysis recovered its two result fields,
`mac-hex` and `unique-hex`. A reInvoke-owned fixed-response WAMP service supplies
only those RAM-safe fields. Bluedroid derives the current compatibility name
`HK Invoke_4E5601` from that response.

An iPhone completed a RAM-only bond and A2DP negotiation at 44.1 kHz stereo.
The physical ring changed the ALSA music control and Bluedroid forwarded the
same changes through AVRCP absolute volume.

Ubuntu 22.04 provided an independently controlled source. BlueZ connected the
classic Audio Sink UUID, and PulseAudio exposed an active SBC `a2dp_sink` with
a live sink input. Both the phone and Linux source delivered sustained RTP/SBC
frames on dynamic L2CAP channel `0x44`.

The remaining failure is after compressed-media ingress. The donor stack did
not emit its A2DP audio-start callback, its `media_worker` remained asleep, and
ALSA stayed closed. A client connected to `.a2dp_data` but received no decoded
PCM. The standard control-channel `CHECK_READY` command returned failure
acknowledgement `1`, so START was not sent.

The donor test configuration differs from normal only by enabling HCI snoop and
raising HCI, L2CAP, and BTIF trace levels. It does not disable A2DP decoding.
This boundary is preserved for later work, but it should not delay replacing
the obsolete Bluedroid userspace with a maintained stack.

## RAM-only BlueZ and BlueALSA replacement

The physical RAM-native platform now has a working classic-Bluetooth replacement
path. BlueZ 5.55 is built statically for the target's old EGLIBC userland and
started with `ControllerMode=bredr`; this avoids the unsupported GATT setup
required by the target's MGMT 1.2 kernel. BlueALSA 4.0.0 registers an A2DP
sink, and `bluealsa-aplay` is ready against ALSA card 1 (`plughw:1,0`) at
initial volume zero.

The owned [bluez-pairing-agent.c](../../tools/control/bluez-pairing-agent.c)
registers as the default `org.bluez.Agent1` through a private D-Bus socket. It
accepts only the operator-supplied peer address and the A2DP/AVRCP service UUIDs;
all other peers and services are rejected. Pairing, D-Bus state, and BlueZ
configuration remain in volatile RAM under `/tmp` and
`/usr/var/lib/bluetooth`. They disappear on reboot. The source and artifact
hashes are recorded in [P1-045](../../metadata/P1-045.json).

Launch a clean stack with an explicit peer and bounded pairing window:

```bash
tools/usb-boot/start-bluez-audio.sh \
  --rootfs path/to/installed-rootfs-region.bin \
  --bluetoothd path/to/bluetoothd \
  --bluealsa path/to/bluealsa \
  --bluealsa-aplay path/to/bluealsa-aplay \
  --hci-init path/to/hci-init \
  --pairing-agent path/to/bluez-pairing-agent \
  --peer-address AA:BB:CC:DD:EE:FF \
  --pair-seconds 60
```

The initiating host must also be pairable during this window. On the tested
BlueZ host, initiating `bluetoothctl pair` while the host adapter was not
pairable completed a transient `No Bonding` exchange. Enabling host pairability
for the bounded exchange created a bond that persisted across disconnect. Host
pairability was disabled immediately afterward.

The Mac mini completed a fresh bond with the RAM-only stack. The target then
returned to `Pairable=false` and `Discoverable=false`. After disconnecting and
waiting for the pairing window to close, the host retained `Paired=true`, the
target retained its volatile bond record, and A2DP reconnected without opening
another pairing window.

With the MCU amplifier and DAC mute asserted and the host sink limited to one
percent, a streamed test tone put ALSA card 1 into `RUNNING` state. The PCM was
stereo `S16_LE` at 44.1 kHz, and its hardware pointer advanced from `192000` to
`238080`. This proves A2DP transport, SBC decode, BlueALSA handoff, ALSA
playback, and DMA operation. Physical mute remained asserted, so audible
Bluetooth playback still requires a short attended acceptance test.

The UIPC command values, sink queue, decoder-reset path, and expected automatic
decode trigger were cross-checked against official AOSP `system/bt` tag
`android-6.0.1_r81`, commit
`3ba689bd4e88946eeb40b8d8b91fb7f42db46529`. The pinned source snapshot and
Apache license evidence are recorded in `P1-043`.

## What is unresolved

Whether `qemu-user` forwards `AF_BLUETOOTH` sockets faithfully enough for the
Bluedroid stack to complete initialization. This is untested.

Whether the host's `/dev/vhci` can be used safely. The workstation has real
Bluetooth hardware at `hci0`, so any virtual adapter work must target a
separate created adapter and must never drive the host's own controller.

Whether a maintained userspace can decode the verified incoming SBC frames and
feed the proven ALSA `music` path. The donor stack pairs and receives media but
does not release decoded PCM.

No Bluetooth procedure has yet been shown to complete under emulation. Current
evidence is limited to service registration on the WAMP bus and static/runtime
evidence for the stack boundary.

## Practical assessment

The physical unit is the cheaper source of truth here. It has a working
Bluedroid stack, a real controller, and a real peer whenever a phone is paired.
The emulator advantage that applied to I2C and ALSA, where a narrow ioctl
boundary could be answered synthetically, is weaker for Bluetooth because the
missing piece is a stateful protocol stack rather than a handful of calls.
