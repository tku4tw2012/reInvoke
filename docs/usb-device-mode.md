---
title: USB device mode on BG2CD
description: Kernel and device-tree work required to provide USB ADB
---

Status: investigated, not built. This records exactly what the change is so it
can be attempted deliberately rather than discovered again.

## Why it is wanted

The unit already recovers from most network loss. A Mic-Mute long press opens a
provisioning access point through `provision-windowd` and `hostapd`, so a
missing or wrong Wi-Fi network does not lock anyone out.

Two cases it does not cover:

* a Wi-Fi driver or radio fault
* userspace broken before provisioning starts

The second is exactly what made candidate 05.1 undiagnosable: the unit flashed
and then never completed boot, with no console, no network, and no way in.

## What is already in place

Nothing needs writing. The kernel source and the build system are both here,
and seventeen kernels have already been built from them.

From the config those builds use:

	CONFIG_USB_GADGET=y
	CONFIG_USB_MV_UDC=y      Marvell USB device controller
	CONFIG_USB_G_ANDROID=y   Android gadget, which carries the ADB function

`drivers/usb/gadget/f_adb.c` is present. `mv_udc_core.c` matches the
device-tree compatible `marvell,berlin-udc`, so the driver was written for this
SoC family rather than adapted to it.

## What is missing: the driver, not the device tree

An earlier revision of this document concluded that the BG2CD device tree
lacked a UDC node and that adding one was the work. That was wrong, and reading
the running system rather than the source tree shows why.

The unit reports `BG2CD` in its U-Boot banner, which is the family name. The
variant is CDP-A0: the vendor's own kernel payload contains
`MARVELL BG2CDP A0 Dongle board based on BERLIN2CDP-A0`, and the DTB this
project already builds carries that exact model string. The `berlin2cd-*.dtsi`
files examined earlier describe a different board and were never the ones in
use.

That device tree already declares the controller, and the vendor kernel already
instantiates it:

```text
/sys/bus/platform/devices/f7b74000.usbphy
/sys/bus/platform/devices/f7ed0000.usb
/sys/bus/platform/devices/f7ed0100.udc
```

The device is present and unbound. There is no `mv-udc` entry under
`/sys/bus/platform/drivers`, and no `/sys/class/udc` at all, because the vendor
kernel was built without gadget support.

The kernel this project builds already has it:

```text
CONFIG_USB_GADGET=y
CONFIG_USB_MV_UDC=y
CONFIG_USB_G_ANDROID=y
```

So USB device mode needs no device-tree work. It needs the reInvoke kernel to
be the one that boots.

## The change

1. Flash the kernel this project already builds, replacing `bootimgs`.
2. Confirm `/sys/class/udc` is populated and `mv-udc` has bound
   `f7ed0100.udc`.
3. Bring up the Android gadget and start `adbd` against it.

The module tree `3.8.13-reinvoke-audio-sd8887` already ships in the runtime and
becomes the live one at that point, so Wi-Fi and Bluetooth should come up from
the same image.

Unknowns worth expecting:

* no reInvoke kernel has ever booted on this unit; every flash to date records
  `bootimgs` as byte-identical to the vendor payload
* the vendor bootloader may configure the PHY for host before Linux runs
* `phy-mode = <2>` in the device tree is unexplained and may select device or
  host operation

## What it costs to try

The kernel lives in `bootimgs`, the one record still byte-identical to vendor
12.2134.0. Shipping a kernel changes what boots.

That was initially recorded here as a serious risk. It is not. Yellow-mode
recovery has been exercised fifty-five times in this project, twenty of them
ending in a confirmed NAND write. A kernel that does not boot costs one more
flash cycle, which is routine, not a brick.

The honest risk is narrower: a kernel that boots but breaks Wi-Fi or Bluetooth
would be harder to notice and harder to bisect than one that does not boot at
all. Keep the previous image staged so a revert is one flash away.
