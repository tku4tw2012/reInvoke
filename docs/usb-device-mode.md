---
title: USB device mode on BG2CD
description: Kernel and device-tree work required to provide USB ADB
ms.date: 2026-09-14
ms.topic: concept
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

## What is missing: one device-tree node

The sibling variant declares a UDC. Ours does not.

`arch/arm/boot/dts/berlin2cdp.dtsi` has both a PHY and a UDC:

	usbphy0: usbphy@F7B74000 {
		compatible = "marvell,berlin-usbphy";
		reg = <0xF7B74000 128>;
		phy-mode = <2>;
	};

	udc@F7ED0000 {
		compatible = "marvell,berlin-udc";
		reg = <0xF7ED0100 0x1ff>;
		interrupts = <0 11 0x4>;
		usb-phy = <&usbphy0>;
		status = "okay";
	};

`arch/arm/boot/dts/berlin2cd-common.dtsi`, which our board includes through
`berlin2cd.dtsi`, declares the same controller host-only and has no PHY node at
all:

	usb@F7ED0000 {
		compatible = "mrvl,berlin-ehci";
		reg = <0xF7ED0000 0x10000>;
		interrupts = <0 11 4>;
		phy-base = <0xF7B74000>;
		reset-bit = <23>;
		pwr-gpio = <8>;
	};

Same controller address, same PHY address. One dual-role block, with the device
side at offset 0x100. Both the EHCI and PHY drivers already write
`USB2_OTG_REG0`.

## The hardware question is already answered

The boot ROM enumerates this SoC as USB device `1286:8174` every time the unit
enters service mode. Device mode demonstrably works on this silicon; Linux is
simply never told to use it.

## The change

1. Add a `usbphy0` node to `berlin2cd-common.dtsi`, copied from the CDP
   variant. The CD tree references its PHY inline as `phy-base` and has no
   phandle, so this introduces one.
2. Add the `udc@F7ED0000` node referencing it.
3. Rebuild with `tools/kernel/build-native-kernel.sh`.
4. Confirm `/sys/class/udc` is populated and `g_android` binds.
5. Start `adbd` against it.

Unknowns worth expecting:

* the CD and CDP PHY programming may differ despite identical addresses;
  `phy-mode = <2>` is unexplained and may be device versus host
* both USB ports are currently host; making one dual-role may need the EHCI
  node for that address removed or made conditional
* the vendor bootloader may configure the PHY for host before Linux runs

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
