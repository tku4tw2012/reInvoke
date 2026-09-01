# USB service mode and the RAM boot path

Status: verified on hardware, 2026-09-01, on unit `myInvoke-1` running the final
`Barracuda_libre-12.2134.0` build.

This document records what the Micro-USB port actually exposes, how to reach the
Marvell download path without opening the enclosure, and where the safe boundary
sits.

## What the port exposes

The unit presents `1286:8174` for approximately four seconds on every power-on,
then disconnects and continues booting from NAND.

```text
usb 3-1.2: New USB device found, idVendor=1286, idProduct=8174
usb 3-1.2: Product: BG2CD S/N:12345678A
usb 3-1.2: Manufacturer: Marvell
[4 seconds later]
usb 3-1.2: USB disconnect
```

This identifier is the BootROM endpoint, not a runtime gadget. Two independent
artifacts in this repository confirm it:

- `marvell_flash_tool/run.sh` invokes `usb_boot 1286 8174 ./ 8141`
- `Mrvl_WinUSB.inf` declares
  `USB\VID_1286&PID_8174.DeviceDesc="Marvell(R) WTP: Tools package USB Driver for BG2CDP Boot Device"`

An earlier revision of the observation procedure described `8174` as a
post-handoff ramdisk gadget. That was wrong and is corrected.

## Cable requirement

Charge-only Micro-USB cables produce a false negative: the speaker powers and
boots normally, but no USB device ever enumerates on the host. Several cables
salvaged from LED-light chargers showed no enumeration at all. A data cable is
required before any negative result means anything.

## Host preparation

`usb_boot` is an x86-64 ELF binary and runs natively on a 64-bit Linux host. It
links against `libusb-1.0`, `libudev`, and `libpthread`.

Grant unprivileged access to the boot endpoint rather than running the flasher
as root. Running `usb_boot` as root would give it write access to block devices
for no benefit.

```text
# /etc/udev/rules.d/99-marvell-invoke.rules
SUBSYSTEM=="usb", ATTR{idVendor}=="1286", ATTR{idProduct}=="8174", MODE="0666"
SUBSYSTEM=="usb", ATTR{idVendor}=="1286", ATTR{idProduct}=="8100", MODE="0666"
SUBSYSTEM=="usb", ATTR{idVendor}=="1286", ATTR{idProduct}=="8101", MODE="0666"
```

## The console client is a hard prerequisite

`usb_boot` does not begin watching USB until a client connects to its TCP
console port. Started without a client, it stops here and the boot window passes
untouched:

```text
main, 1446: wait for connection on port: 8141
tcp_server_func, 869: server listening on port 8141
```

Once a client attaches, it proceeds into the USB hotplug loop:

```text
tcp_server_func, 875: client connected.
polling_for_hotplug_event, 1336: libusb_init
```

The stock `run.sh` hides this by passing `putty telnet://127.0.0.1:8141` as a
fifth argument, which launches the client automatically. Omitting that argument
silently disables USB detection.

Two further details matter when substituting a different client:

- `usb_boot` replies to every `DONT` and `WONT` it receives with another
  negotiation command. A client that answers those replies creates an endless
  loop; a 62 MB log accumulated in seconds during testing. Consume the telnet
  `IAC` sequences without responding.
- Redirecting `usb_boot` output to a file makes stdout block-buffered, which
  hides its progress. Use `stdbuf -oL -eL`.

## Normal power-on is not download mode

With the console attached, the four-second window is caught, but a normally
booting unit only performs a short handshake:

```text
serve_one_target, 1080: sub_class: 254
serve_one_target, 1092: claimed interface
process_img_request, 370: received request from dongle to send image ./08_IMAGE.
cb_img_write, 570: actual data wrote 144, total_data_wrote 144
cb_data_recv, 299: img transfer status: 1
detach_cb, 1227: detach
```

The device requests `08_IMAGE`, receives it, disconnects, and boots from NAND.
It never requests `bcm_erom.bin.usb`, `bootloader.img`, or `sysinit.img`.
Download mode must be armed explicitly.

The numbered files are protocol data, not firmware. `06_IMAGE` is written by the
tool and holds the USB path (`3-1.2`). `08_IMAGE` and `09_IMAGE` are signed
binary blobs used in the secure-boot exchange.

## Arming download mode

From the vendor `Instructions.pdf` shipped in the flashing bundle:

1. Disconnect the unit from power. Leave the USB cable connected.
2. Start `usb_boot` and attach a console client.
3. Press and hold the Reset pinhole with a paper clip, and reconnect power while
   holding it.
4. Still holding Reset, press the MicOff button exactly four times within five
   seconds.
5. The top panel turns yellow when download mode is armed. If it does not, power
   down and repeat.
6. Release Reset only after the U-Boot console appears.

Holding Reset alone is not sufficient. The four MicOff presses are what arm the
mode.

## Safe boundary

The boot chain loads into RAM. Persistent storage is untouched unless a write
command is issued at the U-Boot prompt.

Keep `83_IMAGE` and `99_IMAGE` out of the working directory. `83_IMAGE` is the
NAND firmware image and `99_IMAGE` is reported to brick the unit
unrecoverably. Their absence makes an accidental flash impossible.

Read-only commands such as `help`, `printenv`, and `bdinfo` are safe. Do not
issue `nand`, `nandinit`, `nanderase`, `tftp2nand`, `l2nand`, or `saveenv`
against a working unit.

A working directory limited to `usb_boot`, `bcm_erom.bin.usb`, `bootloader.img`,
`sysinit.img`, `drm_erom.img`, the numbered protocol files, and an empty
`79_IMAGE` will stop at the U-Boot prompt and write nothing.

`79_IMAGE` holds U-Boot commands to run automatically. The shipped file contains
only comments, so execution halts at the prompt. `79_IMAGE.examples` documents
the RAM boot that the vendor used for provisioning:

```text
usbload 0x81 0x0c400000
usbload 0x82 0x08000000
set bootargs console=ttyS0,115200 debug init=/bin/sh root=/dev/ram ...
bootm 0x0c400000
```

That path boots a kernel and ramdisk entirely from RAM with `init=/bin/sh`, and
the vendor README notes the resulting ramdisk runs an ADB service. It requires
`81_IMAGE` and `82_IMAGE` in the working directory and still writes nothing to
NAND, but it has not yet been exercised on this unit.
