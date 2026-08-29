# Bluetooth Stack

The final firmware does not use BlueZ. Recording this because earlier documents
in this repository claimed it did, and that claim shaped a wrong conclusion
about what emulation would require.

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

## The real emulation boundary

`libbt-vendor.so` opens `/dev/rfkill` and waits for an `hci%d` interface. That
places the boundary at the kernel Bluetooth subsystem: an HCI device plus
`/dev/rfkill`, reached through `AF_BLUETOOTH` sockets rather than a session bus.

Consequences for the sandbox:

* `bluetoothd` and a D-Bus session would not help, because nothing calls them.
* The Marvell SDIO driver cannot load, because `qemu-user` runs no guest kernel.
* A virtual HCI adapter from the host's `hci_vhci` module is the plausible
  substitute, since Bluedroid speaks HCI directly.

## What is unresolved

Whether `qemu-user` forwards `AF_BLUETOOTH` sockets faithfully enough for the
Bluedroid stack to complete initialization. This is untested.

Whether the host's `/dev/vhci` can be used safely. The workstation has real
Bluetooth hardware at `hci0`, so any virtual adapter work must target a
separate created adapter and must never drive the host's own controller.

Whether the transport procedures can be exercised without a peer device. Even
with an adapter present, `bluetooth.resume`, `pause`, `next`, and `skipTo`
operate on an active media session.

## Practical assessment

The physical unit is the cheaper source of truth here. It has a working
Bluedroid stack, a real controller, and a real peer whenever a phone is paired.
The emulator advantage that applied to I2C and ALSA, where a narrow ioctl
boundary could be answered synthetically, is weaker for Bluetooth because the
missing piece is a stateful protocol stack rather than a handful of calls.
