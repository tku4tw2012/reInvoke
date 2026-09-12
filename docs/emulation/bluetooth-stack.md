---
title: Bluetooth stack
description: Current BlueZ and patched BlueALSA architecture plus historical Invoke Bluedroid evidence
ms.date: 2026-09-05
ms.topic: concept
---

This document separates current reInvoke behavior from the 2021 final Harman
firmware. The vendor firmware used Bluedroid; the owned target uses BlueZ
5.55, patched BlueALSA 4.0.0, private D-Bus, and owned HCI/pairing helpers. See
the [current product and architecture contract](../current-product-contract.md)
for the normative boundary.
The detailed bring-up and transport captures below are historical RAM tests.
Candidate 02 separately demonstrated native NAND pairing, audible playback,
and rotary volume; see the [native NAND guide](../native-nand-platform.md).

## Current reInvoke stack

The accepted path is:

```text
allowlisted peer
  -> SD8887 HCI / BlueZ 5.55
  -> BlueALSA 4.0.0 A2DP Sink decoder
  -> patched bluealsa-aplay
  -> Invoke ALSA PCM
  -> MCU-owned speaker safety gates
```

The pairing agent authorizes only `<allowlisted-peer>` and the A2DP/AVRCP
service UUIDs during a bounded window. The HCI initializer resets volatile
controller state. Bonds, D-Bus state, configuration, and the active-PCM lease
remain in RAM.

The accepted BlueALSA build applies six reviewed patches:

1. emit an active-PCM lease containing the ALSA-owning worker thread;
2. match the Invoke ALSA hardware contract and recover partial writes/underruns;
3. buffer decoded PCM against scheduling jitter;
4. insert bounded silence for timestamp-confirmed SBC RTP gaps;
5. drain clips that end before the normal prefill threshold; and
6. drain buffered audio after the PCM FIFO closes.

The MCU service authorizes physical unmute only when the lease thread matches
ALSA's owner, resolves to the packaged player, and ALSA is `RUNNING`. Silence,
disconnect, player exit, or shutdown reasserts mute, with a 1.5-second holdoff
for brief transport gaps. Earlier audible playback and control runs passed.
The latest accepted image still needs one attended playback-continuity run.

## Historical Harman Bluedroid stack

The final Harman `usr/bin/bluetooth` links `libhardware.so`, `libcutils.so`,
`libutils.so`, and
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

### Evidence classification

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

### Historical emulation boundary

`libbt-vendor.so` opens `/dev/rfkill` and waits for an `hci%d` interface. That
places the boundary at the kernel Bluetooth subsystem: an HCI device plus
`/dev/rfkill`, reached through `AF_BLUETOOTH` sockets rather than a session bus.

Consequences for the sandbox:

* `bluetoothd` and a D-Bus session would not help, because nothing calls them.
* The Marvell SDIO driver cannot load, because `qemu-user` runs no guest kernel.
* A virtual HCI adapter from the host's `hci_vhci` module is the plausible
  substitute to test, since Bluedroid speaks HCI directly.

### Historical experimental HCI management shim

`tools/emulation/invoke-ioctl-shim.c` can now return one synthetic, active
`hci0` device for `HCIGETDEVLIST`, `HCIGETDEVINFO`, and `HCIDEVUP`. The library
compiles for ARM against `GLIBC_2.4`.

This is management-plane scaffolding only. It does not emulate HCI commands,
events, ACL traffic, pairing, media transport, or `/dev/rfkill`. No Bluetooth
procedure has completed through this shim.

### Historical donor-assisted RAM validation

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
* Applied a temporary locally administered address matching
  `02:XX:XX:XX:XX:XX`

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
only those RAM-safe fields. Bluedroid derived a compatibility name matching
`HK Invoke_<address-suffix>` from that response.

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

## RAM-only BlueZ and BlueALSA validation

The physical RAM-native platform now has a working classic-Bluetooth replacement
path. BlueZ 5.55 is built statically for the target's old EGLIBC userland and
started with `ControllerMode=bredr`; this avoids the unsupported GATT setup
required by the target's MGMT 1.2 kernel. BlueALSA 4.0.0 registers an A2DP sink, and the patched `bluealsa-aplay` targets
ALSA card 1 (`plughw:1,0`) under MCU-owned safety policy.

The owned [bluez-pairing-agent.c](../../tools/control/bluez-pairing-agent.c)
registers as the default `org.bluez.Agent1` through a private D-Bus socket. It
accepts only the operator-supplied peer address and the A2DP/AVRCP service UUIDs;
all other peers and services are rejected. Pairing, D-Bus state, and BlueZ
configuration remain in volatile runtime storage. They disappear on reboot. The source and artifact
hashes are recorded in [P1-045](../../metadata/P1-045.json).

Launch a clean stack with an explicit peer and bounded pairing window:

```bash
tools/usb-boot/start-bluez-audio.sh \
  --rootfs <donor-rootfs-region> \
  --bluetoothd <path-to>/bluetoothd \
  --bluealsa <path-to>/bluealsa \
  --bluealsa-aplay <path-to>/bluealsa-aplay \
  --hci-init <path-to>/hci-init \
  --pairing-agent <path-to>/bluez-pairing-agent \
  --peer-address XX:XX:XX:XX:XX:XX \
  --pair-seconds 60
```

The initiating host must also be pairable during this window. On the tested
BlueZ host, initiating `bluetoothctl pair` while the host adapter was not
pairable completed a transient `No Bonding` exchange. Enabling host pairability
for the bounded exchange created a bond that persisted across disconnect. Host
pairability was disabled immediately afterward.

The source workstation completed a fresh bond with the RAM-only stack. The target then
returned to `Pairable=false` and `Discoverable=false`. After disconnecting and
waiting for the pairing window to close, the host retained `Paired=true`, the
target retained its volatile bond record, and A2DP reconnected without opening
another pairing window.

With the MCU amplifier and DAC mute asserted and the host sink limited to one
percent, a streamed test tone put ALSA card 1 into `RUNNING` state. The PCM was
stereo `S16_LE` at 44.1 kHz, and its hardware pointer advanced from `192000` to
`238080`. This proves A2DP transport, SBC decode, BlueALSA handoff, ALSA
playback, and DMA operation. A later attended run produced audible Bluetooth
playback and validated rotary volume. The accepted patch set has reproducible
host and machine validation; its newest integrated image still needs the final
attended continuity run.

The UIPC command values, sink queue, decoder-reset path, and expected automatic
decode trigger were cross-checked against official AOSP `system/bt` tag
`android-6.0.1_r81`, commit
`3ba689bd4e88946eeb40b8d8b91fb7f42db46529`. The pinned source snapshot and
Apache license evidence are recorded in `P1-043`.

## Historical donor-emulation gaps

Whether `qemu-user` forwards `AF_BLUETOOTH` sockets faithfully enough for the
Bluedroid stack to complete initialization. This is untested.

Whether the host's `/dev/vhci` can be used safely. The workstation has real
Bluetooth hardware at `hci0`, so any virtual adapter work must target a
separate created adapter and must never drive the host's own controller.

Whether donor Bluedroid could be made to release decoded PCM remains
unresolved. This is not a current product blocker: maintained BlueZ and patched
BlueALSA decode and feed the proven ALSA path.

No donor Bluetooth procedure was shown to complete under `qemu-user` emulation.
That limit applies to historical donor research, not physical reInvoke
validation.

## Practical assessment

The physical unit remains the useful source of truth for transport timing,
controller behavior, and attended sound. `qemu-user` emulation remains useful
only for historical Bluedroid interface research because the missing dependency
is a stateful HCI protocol stack. Current development should validate the owned
BlueZ/BlueALSA path with host tests, reproducible builds, machine continuity
checks, and bounded attended hardware runs.
