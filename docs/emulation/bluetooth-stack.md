---
title: Bluetooth stack
description: Owned BlueZ and BlueALSA path and the historical Bluedroid boundary
ms.date: 2026-09-12
ms.topic: reference
---

reInvoke uses BlueZ 5.55, patched BlueALSA 4.0.0, private D-Bus and owned
HCI/pairing helpers. The [current contract](../current-product-contract.md)
defines the supported runtime; Harman's final firmware used Bluedroid instead.
Build inputs and commands are in the [control tools](../../tools/control/README.md).

## Current reInvoke stack

The path is SD8887 HCI -> BlueZ -> BlueALSA A2DP Sink/SBC decode ->
`bluealsa-aplay` -> ALSA card 1 (`plughw:1,0`) -> board audio output.
MCU mute authorization and DSP firmware/control run in parallel, not in the
PCM stream. See the [speaker diagram](owned-speaker-control.md#pcm-and-speaker-safety).

BlueZ uses `ControllerMode=bredr` to avoid unsupported GATT setup on the
MGMT 1.2 kernel. The HCI helper resets volatile controller state. The pairing
agent accepts one configured peer and A2DP/AVRCP UUIDs during a bounded window;
bonds and configuration disappear on power loss.

The accepted BlueALSA build applies six patches:

1. Active-PCM lease containing the ALSA-owning worker thread
2. Invoke ALSA parameters and partial-write/underrun recovery
3. Decoded PCM jitter buffering
4. Bounded silence for timestamp-confirmed SBC RTP gaps
5. Draining clips below the normal prefill threshold
6. Draining buffered audio after PCM FIFO closure

The MCU verifies lease, ALSA `owner_pid`, packaged executable and `RUNNING`
state before opening physical mute gates. Loss of authorization remutes after
1.5 seconds; shutdown requests mute directly. Positive PCM can contain silence.

## Validation scope

Historical RAM tests established encrypted bonding, SBC transport/decode,
ALSA DMA, attended sound and rotary volume. A muted test observed stereo
`S16_LE` at 44.1 kHz with ALSA's hardware pointer advancing. Candidate 02
independently established native audible playback and rotary control.
Candidate 03 established its changed identity and a host-observed A2DP
connection, not another acoustic acceptance run. See the
[native evidence](../native-nand-platform.md).

On the tested BlueZ source, pairing while the host was not pairable produced
a transient `No Bonding` exchange. Bounded host pairability allowed a bond
and subsequent reconnect after the target window closed. Host pairability
was then disabled; the target bond still remained volatile.

The historical `start-bluez-audio.sh` launcher pins older third-party binaries;
new integrated runtime artifacts are not interchangeable inputs. Use
[`build-native-runtime.sh`](../../tools/usb-boot/build-native-runtime.sh)
for the packaged stack. Source provenance is in
[P1-045](../../metadata/P1-045.json).

## Historical Harman Bluedroid stack

Static final-rootfs evidence and physical RAM trials establish:

| Component           | Donor path                                                      |
| ------------------- | --------------------------------------------------------------- |
| Service             | `usr/bin/bluetooth`                                             |
| Android HAL         | `system/lib/hw/bluetooth.default.so`                            |
| Vendor library      | `system/lib/libbt-vendor.so`                                    |
| Kernel module       | `bt_sd8887/bt8xxx.ko` under `lib/modules/3.8.13-yocto-standard` |
| Controller firmware | `lib/firmware/mrvl/sd8887_bt_a2_new.bin`                        |
| Configuration       | `etc/bluetooth_orig/bt_stack.conf`                              |

The service links Android HAL/utilities and ALSA, not `libbluetooth` or
`libdbus`. `bluetooth.sh` copies configuration to `/data/bluetooth`, inserts
the module with `fw_name=mrvl/sd8887_bt_a2_new.bin`, then starts the service.
`libbt-vendor.so` opens `/dev/rfkill` and waits for `hci%d` through the kernel
Bluetooth subsystem and `AF_BLUETOOTH` sockets.

Qemu-user has no guest kernel for the SDIO module. Adding BlueZ/D-Bus cannot
satisfy a Bluedroid handler. The
[ioctl shim](../../tools/emulation/invoke-ioctl-shim.c) provides only synthetic
`HCIGETDEVLIST`, `HCIGETDEVINFO` and `HCIDEVUP`, not HCI commands, ACL traffic,
pairing or rfkill. No donor Bluetooth procedure completed under qemu-user.
A separate virtual HCI adapter remains untested.

## Donor transport and decoder limit

Physical RAM identified SDIO functions `02df:9135`, `02df:9136` and
`02df:9137`; Wi-Fi binds `9135`, Bluetooth aliases `9136`. A matching GPL-built
`3.8.13-reinvoke-audio` Bluetooth module replaced an earlier vermagic-edited
probe. It loaded 272,656 firmware bytes, created `hci0`/rfkill, initialized
A2DP Sink and AVRCP Controller/Target, and enabled pairing.

The donor requires `com.harman.identifiersGet` kwargs `mac-hex` and `unique-hex`.
A narrow fixed-response service supplied them without the stock supervisor,
producing a name of form `HK Invoke_<address-suffix>`.

Phone and Linux sources negotiated A2DP and delivered RTP/SBC on dynamic
L2CAP `0x44`; the rotary changed ALSA music control and AVRCP absolute volume.
However, Bluedroid never emitted its audio-start callback: `media_worker`
slept, ALSA stayed closed, and `.a2dp_data` returned no PCM. `CHECK_READY`
returned failure acknowledgment `1`, so START was not sent. Increased tracing
did not disable decoding. This unresolved donor boundary motivated the
replacement; it is not a limitation of the current BlueALSA decoder.

UIPC/decoder expectations were checked against AOSP `system/bt`
`android-6.0.1_r81`, commit `3ba689bd4e88946eeb40b8d8b91fb7f42db46529`;
see [P1-043](../../metadata/P1-043.json).
