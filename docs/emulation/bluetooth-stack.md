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

## What is unresolved

Whether `qemu-user` forwards `AF_BLUETOOTH` sockets faithfully enough for the
Bluedroid stack to complete initialization. This is untested.

Whether the host's `/dev/vhci` can be used safely. The workstation has real
Bluetooth hardware at `hci0`, so any virtual adapter work must target a
separate created adapter and must never drive the host's own controller.

Whether the transport procedures can be exercised without a peer device. Even
with an adapter present, `bluetooth.resume`, `pause`, `next`, and `skipTo`
operate on an active media session.

No Bluetooth procedure has yet been shown to complete under emulation. Current
evidence is limited to service registration on the WAMP bus and static/runtime
evidence for the stack boundary.

## Practical assessment

The physical unit is the cheaper source of truth here. It has a working
Bluedroid stack, a real controller, and a real peer whenever a phone is paired.
The emulator advantage that applied to I2C and ALSA, where a narrow ioctl
boundary could be answered synthetically, is weaker for Bluetooth because the
missing piece is a stateful protocol stack rather than a handful of calls.
