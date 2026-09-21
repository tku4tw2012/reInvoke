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

`rmmod g_android` followed by `insmod` panicked this unit on every candidate up
to and including 2.2.7. The unload itself is safe, and the kernel refuses the
genuinely dangerous ones on its own: module dependency refcounting returned
`EBUSY` for `udc_core` with two users, and `f_adb` sets `.owner = THIS_MODULE`
so the VFS holds a reference while adbd has the device open. What panicked was
the *re-insert*, and with no pstore or `last_kmsg` on this unit there was no
log saying why.

2.2.8 shipped a module intended to fix it and a test to say whether it did.
The test has now run and the answer is no: reload still fails, and the reason
is not the one this page gave. See below. Bring USB ADB back with a reboot.

### The asymmetry, and the fix that was never shipped

> The reasoning in this section is kept because it is how the fix was arrived
> at and it is honest about the binaries. Its conclusion about the *running*
> system is wrong, which only the hardware test showed. Read it with the
> section after it.

Read out of `g_android.ko` with `objdump`, so this is about binaries rather
than the source they came from.

The module built on 2026-09-17, which every candidate up to and including 2.2.7
shipped, has a `cleanup_module` that makes exactly three calls:

```
usb_composite_unregister    →  unbind, which destroys the device android_bind
                               created
class_destroy               →  tears down the android_usb class
kfree                       →  releases _android_dev
```

`init_module` creates a *second* device: `device_create` and
`device_create_file` for `android0`. Nothing destroyed that one. An unload
therefore left a device behind whose class had been destroyed and whose module
text had been freed, which is the state a re-insert walked into. The asymmetry
is the vendor's, inherited from upstream `android.c` of this era.

`android_destroy_device()` was added to `cleanup()` in `android.c` on
2026-09-18 and compiled the same minute. It was never packaged. The artifact
directory the build pinned still held the module from the day before, so every
reload attempt afterwards ran against a binary that did not contain the fix,
and the conclusion that "the panic persisted" was drawn from it. The module
hash guard in `usb-adb-config.js` did not catch this and could not have: the
pin and the artifact agreed with each other. Both were simply a day old.

2.2.8 ships the 09-18 module. Verified before the pin moved:

| check | 09-17 | 09-18 |
| --- | --- | --- |
| `cleanup_module` calls | unregister, class_destroy, kfree | unregister, `device_remove_file`, `device_destroy`, class_destroy, kfree |
| `init_module` relocation | `0xbc` | `0xbc` |
| `.gnu.linkonce.this_module` | `0x144` | `0x144` |
| vermagic | `3.8.13-yocto-standard SMP preempt mod_unload ARMv7` | identical |
| other five modules | — | byte-identical |

### Tested, and the diagnosis above was wrong

`usb-adb-reload-test.sh` ran on 2026-09-20 against the fixed module. It set
`panic_on_oops=0` first, which is what made the result readable: the kernel
stayed up, the trace was written to `/persist`, and it survived the power
cycle. Evidence is in `evidence/reload-test-229-*`.

Two things happened, and neither was what this page predicted.

**The unload now warns, and the warning is in the fix.**

```
sysfs: kobject android0 without dirent
  sysfs_attr_ns <- sysfs_remove_file <- device_remove_file
  <- cleanup+0x58 [g_android] <- sys_delete_module
```

`cleanup()` reaches its new `device_remove_file` and finds `android0` has no
sysfs dirent left, because `usb_composite_unregister` ran first and the unbind
path had already taken the device down. The asymmetry this page described --
`init_module` creating a device nothing destroys -- is not what the running
kernel does. `rmmod` still returns 0; a warning is not fatal. But the fix
removes something already removed.

**The re-insert fails inside `init`, not on a leaked kobject.**

```
misc_deregister
  <- accessory_function_cleanup [g_android]
  <- android_usb_unbind <- composite_unbind
  <- composite_bind <- usb_gadget_probe_driver
  <- usb_composite_probe <- init+0x158 [g_android]
  <- do_one_initcall <- load_module <- sys_init_module
```

`insmod` exited 139 and left `/sys/module/g_android` present. Read it from the
bottom: `init()` calls `usb_composite_probe`, `composite_bind` fails, and the
failure path calls `composite_unbind`, which runs `accessory_function_cleanup`,
which calls `misc_deregister` on a misc device this load never successfully
registered. The oops is in the error path, not in the thing that errored.

So the real sequence is that state from the first load survives the unload,
the second `composite_bind` cannot get what it needs, and the teardown it
invokes tears down things that were never set up. Which piece of state
survives is not yet identified; the accessory function's misc device is the
obvious suspect and has not been confirmed.

Adding `device_destroy` to `cleanup()` was therefore necessary reasoning from
a real asymmetry in the disassembly, and still the wrong conclusion about the
running system. It neither fixes reload nor is harmless: it adds a warning to
every unload.

### What the trace actually named, and the fix in 2.2.9

The vendor source explains every line of that trace.

**`android0` and the first function share a device number.** `android0` is
created with `MKDEV(0, 0)`; `android_init_functions()` created function
devices with `MKDEV(0, index)` starting at index 0. `device_destroy()` finds
its victim by `devt` alone, so cleaning up function 0 destroyed `android0`.
That is the missing dirent, and it means the earlier `device_destroy` in
`cleanup()` was removing a device something else had already taken. Functions
now start at `index + 1`.

**Teardown runs for functions that were never set up.**

```c
while (*functions) {
        f = *functions++;
        if (f->dev) { device_destroy(...); kfree(f->dev_name); }
        if (f->cleanup) f->cleanup(f);   /* whether or not init ever ran */
}
```

`android_init_functions()` stops at the first failure, but
`android_cleanup_functions()` walks the whole table, and `composite_bind()`
calls it on its own failure. So `acc_cleanup()` ran for a load that never
reached `misc_register()`:

```c
static void acc_cleanup(void)
{
        misc_deregister(&acc_device);   /* never registered this time */
        kfree(_acc_dev);
        _acc_dev = NULL;
}
```

Deregistering a misc device that is not on the list unlinks a node that is not
there. That is the oops. It now returns early when `_acc_dev` is NULL.

**`acc_setup()` leaves a freed pointer behind.** It publishes `_acc_dev`
before `misc_register()` and its error path frees the allocation without
clearing it, so the next `acc_cleanup()` frees it again. It now clears it.

**Two more cleanups had the same shape.** `acm_function_cleanup()`
dereferenced `f->config` without checking it, which faults for a function
whose init never ran; `adb` and `ffs` freed `f->config` without clearing it.

Four fixes, kept as patches beside the modules. Exactly one module changed:
the other five have byte-identical `.text` to the set already validated on
hardware, and the `init_module` relocation and `.gnu.linkonce.this_module`
size are unchanged.

Whether this makes reload work is unproven until the test runs again.

