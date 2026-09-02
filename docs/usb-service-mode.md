---
title: USB service mode and the RAM boot path
description: Hardware observations and evidence limits for the Invoke Micro-USB boot endpoint
ms.date: 2026-09-02
ms.topic: troubleshooting
---

Status: partially verified on hardware, 2026-09-01, on unit `myInvoke-1`.
The installed firmware version has not been read from the device. Its
Bluetooth-only behavior is consistent with the final firmware line.

Reached: the Marvell boot endpoint enumerates, `usb_boot` claims the interface,
and the host reports a complete `08_IMAGE` transfer. Not reached: device
subclass `0xFF`, the host tool's iROM bootstrap branch, or a U-Boot console.
Sixteen descriptor captures all report device and interface subclass `0xFE`.

This document records what the Micro-USB port actually exposes, how far the
download path can be driven without opening the enclosure, where that path
currently stops, and where the safe boundary sits.

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

This identifier is the Marvell BG2CDP boot/download endpoint, not a runtime
gadget. The exact device component that emits it remains unidentified. Two
independent artifacts establish its intended boot-tool use:

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

The device requests image type `0x08`, receives it, disconnects, and boots from
NAND. It never requests `bcm_erom.bin.usb`, `bootloader.img`, or `sysinit.img`.

The numbered files are a mix of protocol data and payloads, not firmware.
`06_IMAGE` is written by the tool on every run and holds the USB path (`3-1.2`);
its mtime updates each session. `07_IMAGE` holds an image size as a
little-endian `uint32` and is likewise tool-written; the copy in the bundle
reads 107,934,810, exactly the size of `83_IMAGE`, so it is a leftover from the
vendor's own working directory. `08_IMAGE` and `09_IMAGE` are opaque structured
binary records shipped in the bundle.

## Device subclass selects the host-tool path

The original `usb_boot` binary branches on `bDeviceSubClass`:

```text
4045ed:  mov    -0x18(%rbp),%eax     ; sub_class
4045f0:  cmp    $0xff,%eax
4045f5:  jne    40461d               ; not 255 -> ordinary request loop
404610:  call   4041eb <serve_irom_stage_dongle>
```

Only device subclass `0xFF` enters `serve_irom_stage_dongle`, which sends
`bcm_erom.bin.usb`. This establishes the host binary's behavior. Community
reports associate that branch with the iROM bootstrap, but the descriptor alone
does not identify which device component emits `0xFE`.

This unit reports **subclass 254 on every attempt**, including attempts where
the top panel turned yellow. The host therefore never takes its iROM branch, and
the `bcm_erom` to `bootloader` to `sysinit` to `drm_erom` chain never begins.

The image type codes are confirmed as: `0x01` no-op, `0x02` `sysinit.img`,
`0x03` `bootloader.img`, `0x05` `drm_erom.img`, and anything else formatted as
`NN_IMAGE`. The `0x08` request is outside those special mappings. Its device-side
meaning and executing stage remain unknown.

Observed behaviour, both confirmed on hardware:

| Entry method | USB windows | Subclass | Request |
|---|---|---|---|
| Ordinary power-on | one, then boots | 254 | `0x08` |
| Reset held plus four MicOff presses, panel yellow | fourteen retries over roughly two minutes, then white, then chime | 254 | `0x08` |

Arming download mode changes the retry count but not the observed descriptor.
The yellow indication and retry loop are genuine behavioral differences, but
they do not identify the executing boot stage.

One unproven hypothesis is that NAND or boot-state differences determine whether
the device presents `0xFF` or `0xFE`. The available success and failure reports
do not establish that relationship, and no destructive test is justified by it.

## The console proxy works, and the device announces an ID

Phase 2 includes a console proxy: bytes arriving on interrupt IN `0x82` are
stripped of the `i*m*g*r*q*` markers and forwarded to the TCP client, and client
input is sent back on interrupt OUT `0x02`. Both directions are confirmed
working here.

Before requesting an image, the device emits an ASCII string that reaches the
console:

```text
one target device connected.
 424091892ef47412 target device disconnected.
```

`424091892ef47412` was byte-identical across every attempt, so it is a stable
identifier rather than a per-boot nonce. The exchange is therefore not a
randomised challenge.

Host-to-device input also reaches the hardware. With keystrokes fed
continuously so they were waiting when the window opened, `usb_boot` logged:

```text
tcp_server_func, 891: sending \n, len 1 to dongle.
cb_intr_out, 456: actual data wrote 1
```

The host controller completed the transfer, but the device produced no visible
reaction, banner, countdown, or prompt. This does not prove that firmware
consumed the byte or that the transfer reached the relevant timing window.

## The descriptor, captured

The boot window is too short to run `lsusb -v` by hand, so a udev rule was used
to dump the descriptor automatically on attach. Captured 2026-09-01:

```text
idVendor=1286  idProduct=8174  bcdDevice=0001  speed=480  version=2.00
manufacturer=Marvell   product=BG2CD S/N:12345678A   serial=S/N:12345678A
bDeviceClass=ff   bDeviceSubClass=fe   bDeviceProtocol=ff
bNumConfigurations=1   bNumInterfaces=1

interface 3-1.2:1.0
  bInterfaceNumber=00   bAlternateSetting=0
  bInterfaceClass=ff    bInterfaceSubClass=fe   bInterfaceProtocol=ff
  bNumEndpoints=04
  ep_01 Bulk       addr=01 maxpacket=0200
  ep_02 Interrupt  addr=02 maxpacket=0200
```

This closes a question that looked promising. The original `usb_boot` reads
`bDeviceSubClass` from the device descriptor, at offset 5, loaded from
`-0xbb(%rbp)` after `libusb_get_device_descriptor`. The community ARM
reimplementation instead reads `bInterfaceSubClass` from interface 0. Those are
different bytes, so a device reporting `0xFE` in one and `0xFF` in the other
would explain the whole failure and would mean the ARM tool could succeed where
this one cannot.

**Both fields read `0xFE` on this unit.** There is no descriptor discrepancy, so
both tools would skip the iROM branch on those observed enumerations. Their host
timing and re-enumeration behavior remain separate controls.

The endpoint layout matches the documented protocol: bulk `0x01` for image data
and interrupt `0x02` for console input, with the IN counterparts `0x81` and
`0x82`.

## Failure signature and what it means

Every cycle ends identically. The image is delivered in full and correctly
framed: an eight-byte header carrying the size as a little-endian `uint32`
followed by four zero bytes, then the payload. The subsequent bulk IN transfer
then reports status 1.

That value is a libusb transfer status, not a device status code:
`LIBUSB_TRANSFER_ERROR` is 1 and `LIBUSB_TRANSFER_NO_DEVICE` is 5. Both were
observed, and both are what a host sees when a device drops off the bus
mid-transfer. On any non-zero status `cb_data_recv` calls `request_exit(2)` and
tears the session down.

A disconnect after an image is expected between boot phases, so the transfer
error alone does not prove rejection. Two observations point at a timeout rather
than a data-dependent rejection:

- Every cycle lasts four to five seconds from attach to disconnect, measured
  across many attempts. A validation failure would be expected to vary with the
  work done; a fixed interval looks like a timer.
- The device returns to the same request every time instead of advancing to the
  next image in the chain.

The host reports a complete, correctly framed exchange. `cb_img_write` sends
every block, calls `fclose`, and sends no trailing completion marker. Whether
the device accepts or acts on the record remains unknown.

## What 08_IMAGE and 09_IMAGE contain

Both are opaque records sharing a common layout:

```text
08_IMAGE  01 00 00 00  6a e7 03 00  ...   144 bytes
09_IMAGE  01 00 00 00  37 c2 00 13  ...  4096 bytes
```

The first word is 1 in both. The second differs: `0x0003e76a` in `08_IMAGE` and
`0x1300c237` in `09_IMAGE`. The remainder is high-entropy binary consistent with
several possible encrypted or authenticated formats.

The unit's announced identifier `424091892ef47412` does not appear in
`08_IMAGE` in either byte order, and no four-byte or eight-byte prefix of it
matches at any offset. The copy being served is the vendor's unmodified blob.
This plaintext comparison does not exclude hashing, derivation, family binding,
or device-side version checks.

## Documented entry procedures disagree

Three sources describe entry, and they differ in ways that matter:

| Source | Sequence |
|---|---|
| Vendor `Instructions.pdf`, Process 2 | Hold Reset, repower, four MicOff presses, hold until the U-Boot console appears |
| Vendor `Instructions.pdf`, Process 1 | Hold Reset, repower, four MicOff presses, release once the panel turns yellow |
| Community ARM flasher README | Hold Reset, repower, release roughly four seconds after yellow appears, no MicOff presses at all |

The ARM flasher also sequences the host differently: it puts the unit into
service mode first, starts the tool, opens the console, and connects the USB
cable last. Every attempt here kept USB connected throughout, which may change
how the device enumerates.

Attempts so far, all ending in subclass 254:

| Variant | Outcome |
|---|---|
| Reset held, four MicOff, released early | Three enumerate cycles, then normal boot |
| Reset held, four MicOff, held throughout | Yellow, fourteen retry cycles, then white, then chime |
| Same, with keystrokes fed to the console | Host transfer completed, no visible response, same exit |

### Published yellow-mode procedures were tried

Those variants have now been tested on this unit:

| Variant | Result |
|---|---|
| USB connected throughout, Reset held, four MicOff presses, held through yellow | Subclass 254, repeated requests for `08_IMAGE` |
| Same, with keystrokes fed to the console | Host transfer completed, no visible response, subclass 254 |
| Reset and power only, no MicOff presses | Panel never turned yellow at all |
| Reset, four MicOff presses, released at yellow, USB connected last while still yellow | Subclass 254, repeated requests for `08_IMAGE` |

Two observations follow. First, on this unit the four MicOff presses are
required: Reset and power alone never produce the yellow indication, which
differs from the ARM flasher README. Second, every tested yellow-mode sequence,
including connecting USB while the panel was still yellow, produced subclass
254. The separate community-reported green startup mode was not tested.

## External reports

Primary community evidence confirms that the RAM-boot path has worked:

- In [HKHacking Discussion #3](https://github.com/coggy9/HKHacking/discussions/3),
  `rbeesley` reported: "success booting into u-boot ... And now I have an ADB
  shell." The report used Windows for the device connection after building the
  image in WSL.
- In [HKHacking Discussion #11](https://github.com/coggy9/HKHacking/discussions/11),
  `baccccccc` reported a failure nearly identical to this unit: the panel turned
  yellow, the console repeatedly printed `one target device connected`,
  a stable hexadecimal identifier, `i*m*g*r*q`, and `target device
  disconnected`, then the unit booted normally. No resolution was posted.
- The later
  [ARM flasher project](https://github.com/jryruegas92/hk-invoke-arm-flasher)
  reports a successful flash on a bricked unit. Its exact procedure differs
  from Harman's PDF: enter service mode with power and Reset only, release Reset
  after yellow, start the tool and console, then connect Micro-USB last to a
  direct Raspberry Pi 4 USB 2.0 port.
- In Discussion #3, `coggy9` also described holding Bluetooth and Mic while
  starting as another, green-light "bootloader" mode. That single report came
  after the unit had been damaged by writing `99_IMAGE`; the mode produced the
  same connection loop and was not shown to reach U-Boot.

The public successes prove the path exists on some units, but do not establish
which device state selects `0xFF`. The successful ARM test began with an old
unit that had missed the final OTA; that application state does not itself prove
an invalid NAND boot chain. This unit boots normally and presents `0xFE`, but
the causal difference remains unknown.

USB topology is another concrete difference. The ARM success used a direct
Raspberry Pi 4 USB-A 2.0 port. Its author reports that an Ubuntu-hosted Dell
behind a USB-C adapter sent the initial bootstrap and then stalled, while docks,
virtual machines, and USB passthrough were less reliable. This Mac mini exposes
the external port through one internal EHCI hub. That may matter after iROM
starts, but it does not explain why the initial descriptor is subclass 254.
The ARM executable was reviewed but was not run on Raspberry Pi hardware here.

## Untested next steps

Direct measurement establishes three narrower constraints:

- Every tested yellow-mode sequence produced `0xFE`.
- Both descriptor subclass fields were `0xFE`, so both reviewed tools would
  skip their iROM branch on the observed enumerations.
- An interrupt-OUT byte completed with no visible reaction; firmware consumption
  and exact timing remain unknown.

Sixteen retained descriptor captures report `bDeviceSubClass=fe`,
`bInterfaceSubClass=fe`, and `iInterface="DEFAULT"`. Operator notes associate
them with normal and service-mode attempts, but the descriptor log itself does
not label each physical sequence. Observed tool sessions requested image type
`0x08`; no capture showed `0xFF`, and no U-Boot prompt appeared.

The relationship among NAND state, firmware version, button state, and subclass
selection remains unresolved. Discussion #11 records this unit's failure
signature on a normally booting Cortana unit, while other reports omit the
starting boot state or raw descriptors. Destructive NAND changes are not a
valid way to test the hypothesis.

One host-side diagnostic remains available through `start-session.sh`:

* `absent` removes `08_IMAGE` so the request cannot be satisfied. It is a
  control, to confirm the request is not itself what triggers the disconnect.

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

Holding Reset alone is not sufficient. The four MicOff presses are what produce
the yellow indication. Timing is tight, and the sequence needs both hands: hold
the pinhole with one hand throughout, and use the other to seat the barrel
connector and then tap MicOff. A switched power strip makes it easier.

Releasing Reset early resets the unit. That was observed directly as a burst of
enumerate-and-disconnect cycles followed by a fall-through to normal boot.

On this unit the sequence produces yellow and a sustained retry loop, but not
subclass `0xFF`, so the U-Boot console has not been reached. The step above that
says to release Reset once the console appears is reproduced from the vendor
document and remains untested here.

## Safe boundary

The intended boot chain loads into RAM, and no known NAND-write command was
issued during these tests. Complete absence of device-side persistent writes
has not been independently verified with before-and-after storage captures.

Keep `83_IMAGE` and `99_IMAGE` out of the working directory. `83_IMAGE` is the
NAND firmware image and `99_IMAGE` is reported to brick the unit
unrecoverably. Keep `79_IMAGE` comment-only so no command runs automatically.
These controls prevent the known vendor NAND-write workflow from proceeding.

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
