---
title: USB ADB on the vendor kernel
description: How USB ADB was built and proven on this unit without replacing the kernel
ms.date: 2026-09-17
ms.topic: reference
---

USB ADB works on this unit. It was proven on hardware before anything was
written to NAND: the modules, the device node and the property area all live in
tmpfs, and a reboot returns the speaker to exactly its flashed state.

An earlier note in this project concluded that "no loadable UDC module ships in
either module tree, so USB ADB cannot be added to the vendor kernel without
replacing it". The premise was true and the conclusion was wrong. Nothing ships
one, but one can be built.

## Why it is worth having

Network ADB is unavailable exactly when it is most needed. If a build comes up
but Wi-Fi does not, network ADB is gone and the only remaining route is yellow
mode. USB ADB does not depend on the network coming up at all.

## What the hardware actually has

| fact | evidence |
| --- | --- |
| The SoC has a device controller | `berlin2cdp-a0.dtsi` declares `udc@F7ED0000`, registers at `0xF7ED0100`, IRQ 11 |
| A driver for it exists | `mv_udc_core.c` matches `marvell,berlin-udc` |
| The live device tree has the node | `/sys/bus/platform/devices/f7ed0100.udc` is present on the running unit |
| The USB PHY framework is present | `devm_usb_get_phy`, `usb_add_phy` and `berlin_phy_probe` are all in the running kernel |
| Gadget support was switched off deliberately | `CONFIG_USB_GADGET=n` in `berlin2cdp_a0_amp_acast_defconfig`, while the non-acast defconfigs set it to `y` |
| The kernel boots expecting a gadget console | the kernel command line carries `console=ttyGS0`, a USB gadget serial console |

Only the gadget framework itself was missing: `usb_gadget_probe_driver` and
friends do not appear in the running kernel at all.

## Matching the vendor's module ABI

`CONFIG_MODVERSIONS` is off on this kernel, so a module needs only a matching
vermagic and resolvable symbols. That is necessary but nowhere near sufficient:
vermagic does not cover `struct module` itself, and the module loader writes
into that structure using its own layout. Get it wrong and the kernel writes a
garbage pointer.

The required vermagic is:

```text
3.8.13-yocto-standard SMP preempt mod_unload ARMv7
```

Two configuration differences had to be found by measurement rather than
guessed. The vendor ships `sd8xxx.ko`, and this build produces the same module
from the same source, so the vendor's own module is the reference:

| measurement | vendor | required |
| --- | --- | --- |
| `init_module` relocation offset | `0xbc` | `0xbc` |
| `.gnu.linkonce.this_module` size | `0x144` | `0x144` |

* `CONFIG_UNUSED_SYMBOLS` must be **off**. It adds six fields before `init`,
  moving that pointer by 24 bytes.
* `CC_HAVE_ASM_GOTO` must be **defined**, which makes `HAVE_JUMP_LABEL` add its
  8 bytes after `init`. The NDK gcc 4.9 fails the kernel's own
  `scripts/gcc-goto.sh` because of GCC bug 48637, so the kernel build drops the
  define and the structure comes out 8 bytes short. Passing
  `-DCC_HAVE_ASM_GOTO` restores it. If any module ever does use a static key,
  that bug produces a compile error rather than silent corruption.

A module built without both corrections either panics the kernel on load or
loads with its `init` pointer read as NULL, so nothing runs and nothing reports
an error. Both were observed on hardware.

Before trusting any driver, check the build with a module that only calls
`printk`. If that cannot load and unload cleanly, the problem is the build
environment and not the driver.

## Building

Two source changes, both preserved as patches beside the modules:

* `EXPORT_SYMBOL_GPL(usb_remove_config)` in `composite.c`. Upstream only ever
  built the Android gadget into the kernel, so this never needed exporting.
* `USB_G_ANDROID` from `boolean` to `tristate` in the gadget `Kconfig`. The
  source is already module-ready: it has `MODULE_LICENSE`, `module_exit`, and a
  `late_initcall` that becomes `module_init` under `MODULE`.

Build with `ARCH=arm`, the NDK gcc 4.9, `KCFLAGS="-fno-pic -DCC_HAVE_ASM_GOTO"`
and `HOSTCFLAGS` including `-fcommon`, from
`berlin2cdp_a0_amp_acast_defconfig` with `CONFIG_LOCALVERSION="-yocto-standard"`,
`USB_GADGET=m`, `USB_MV_UDC=m`, `USB_G_ANDROID=m` and `UNUSED_SYMBOLS=n`.

`-fno-pic` matters: without it the NDK compiler emits GOT references that the
vendor's own modules do not have.

The result is six modules totalling about 186 KB.

## Bringing it up

Ordering is the whole problem. `usb-adb-up.sh` beside the modules encodes it.

**The port is shared.** The EHCI host at `f7ed0000` and the device controller at
`f7ed0100` are one hardware block. The device driver selects device mode with

```c
tmp = readl(&udc->op_regs->usbmode);
tmp |= USBMODE_CTRL_MODE_DEVICE;
```

which is an OR. Host mode is `3` and device mode is `2`, so if the EHCI driver
has already put the block in host mode, that OR leaves it in host mode and the
controller never answers as a device. The host driver must release the port
first.

**The gadget withholds its configuration until adbd is ready.** This is
deliberate, in `f_adb`:

```c
/* Disable the gadget until adbd is ready */
if (!data->opened)
        android_disable(dev);
```

`adb_ready_callback` releases it when adbd opens `/dev/android_adb`. If the
gadget is enabled before adbd holds the device open, `disable_depth` never
reaches zero, no configuration is ever added, and a host sees a device
advertising zero configurations.

**Nothing creates device nodes here**, so `/dev/android_adb` has to be made by
hand from `/sys/class/misc/android_adb/dev`.

**adbd chooses its transport from a property.** The shipped property area sets
`service.adb.tcp.port=5555`, which sends adbd down the TCP path so it never
touches USB. A copy with that property cleared selects USB. The property area
is handed to adbd as an inherited file descriptor, never opened by path:

```sh
exec 9</tmp/props-usb.bin
ANDROID_PROPERTY_WORKSPACE=9,32768 /sbin/adbd-root
```

**There is a window that must be closed.** `/dev/android_adb` only appears once
the composite binds to a UDC, and that same bind asserts the D+ pullup. Between
that moment and adbd opening the device, a host will enumerate a device with no
configuration, answer `can't read configurations, error -22`, retry a few times
and then latch the port off until the cable is physically replugged. The script
drops the pullup immediately after loading and re-asserts it only once the
configuration exists.

The re-assert has to be a real transition. `mv_udc_pullup` returns early when
`softconnect` already holds the value being written, so a bare `connect` after
the driver has internally set it leaves the controller stopped with its run bit
clear while everything else looks correct.

## What it looks like when it works

On the speaker:

```text
gadget: high-speed config #1: android
android_work: sent uevent USB_STATE=CONFIGURED
```

On the host:

```text
usb 3-1.2: New USB device found, idVendor=18d1, idProduct=0001
$ adb devices -l
0123456789ABCDEF  device usb:3-1.2
```

Verified from a cold boot with no physical interaction: `adb shell` runs
commands and `adb push` round-trips a file unchanged.

## How it is shipped

The payload lives at `/opt/reinvoke/usb-adb`: the six modules, a property area
with no `service.adb.tcp.port`, and the teardown script. Bring-up runs from
init; teardown runs on the shutdown path.

It is gated the same way the peer firewall is:

* `usbAdb.enabled` in the build configuration decides whether the payload is
  installed at all.
* `/persist/reinvoke/usb-adb-disabled` turns it off at runtime across reboots
  without reflashing and without removing anything.
* `/opt/reinvoke/usb-adb/usb-adb-down.sh` stops it for the current boot.

Teardown is on the shutdown path because leaving it up broke reboots. adbd
sleeps inside the gadget driver and the driver holds the USB controller; with
both left in place this unit could not complete a soft `reboot` and had to be
power cycled.

Network ADB was removed in the same change. It needed an associated Wi-Fi link
and a healthy runtime, which is exactly what SSH already needs, so it only ever
helped in the narrow case where SSH specifically broke while networking did
not. It was also limited to a 300 second window and a single `/32` peer, and it
had been sitting in the `failed` state with a stale peer address for an entire
session without either of us noticing, because SSH did everything. USB ADB
covers the case that actually matters: the build boots but the network does
not.

## Unloading

`rmmod g_android` followed by `insmod` panics this unit. The unload itself is
safe, and the kernel refuses the genuinely dangerous ones on its own: module
dependency refcounting returned `EBUSY` for `udc_core` with two users, and
`f_adb` sets `.owner = THIS_MODULE` so the VFS holds a reference while adbd has
the device open. What panicked was the *re-insert*, and with no pstore or
`last_kmsg` on this unit there is no log saying why. So teardown is supported
and reload is not: bring USB ADB back with a reboot.

### What the module does on the way out

Read out of the shipped `g_android.ko` with `objdump`, so this is the binary
that runs, not the source it was built from. `cleanup_module` makes exactly
three calls, in this order:

```
usb_composite_unregister    →  unbind, which does device_destroy for the
                               device android_bind created
class_destroy               →  tears down the android_usb class
kfree                       →  releases _android_dev
```

`init_module` creates a *second* device: it calls `device_create` and
`device_create_file` for `android0` directly. **Nothing destroys that one.**
The only other `device_destroy` call sites in the module are in `android_bind`'s
error path and in `android_usb_unbind`, and both concern the bind-time device.
This asymmetry is the vendor's, inherited from upstream `android.c` of this
era; it is not something the two build patches introduced.

So an unload leaves an `android0` device behind whose class has been destroyed
and whose module text has been freed.

*Inference, not yet observed:* the leaked device holds a reference to the old
class, so its sysfs name is never released, and the re-insert's
`class_create(THIS_MODULE, "android_usb")` collides with it. This unit runs
`panic_on_oops=1` and `panic=1`, which turns the resulting warning into a panic
and reboots one second later, taking the evidence with it.

That prediction is cheap to test and has not been tested: set
`panic_on_oops=0` first. If the panic is an escalated warning the kernel
survives, `insmod` fails with an ordinary error, and `dmesg` holds the trace.
If it panics anyway, the cause is something harder and the guess above is
wrong. Either result is worth more than the current silence. The cost of being
wrong is one reboot, because the unit boots from NAND and nothing writes NAND
at runtime.

Fixing it means adding the missing `device_destroy` to `cleanup` and rebuilding
with the toolchain and ABI corrections above. That has not been done, because
the only thing it buys is reloading USB ADB without a reboot.
