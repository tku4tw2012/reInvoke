# USB service mode and the RAM boot path

Status: partially verified on hardware, 2026-09-01, on unit `myInvoke-1` running
the final `Barracuda_libre-12.2134.0` build.

Reached: the Marvell boot endpoint enumerates, `usb_boot` claims the interface,
and a full image request and transfer completes. Not reached: interface subclass
`0xFF`, and therefore no U-Boot console.

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

The device requests image type `0x08`, receives it, disconnects, and boots from
NAND. It never requests `bcm_erom.bin.usb`, `bootloader.img`, or `sysinit.img`.

The numbered files are a mix of protocol data and payloads, not firmware.
`06_IMAGE` is written by the tool on every run and holds the USB path (`3-1.2`);
its mtime updates each session. `07_IMAGE` holds an image size as a
little-endian `uint32` and is likewise tool-written; the copy in the bundle
reads 107,934,810, exactly the size of `83_IMAGE`, so it is a leftover from the
vendor's own working directory. `08_IMAGE` and `09_IMAGE` are signed binary
blobs shipped in the bundle.

## Interface subclass selects the boot path

This is the decisive detail. `usb_boot` branches on the USB interface subclass:

```text
4045ed:  mov    -0x18(%rbp),%eax     ; sub_class
4045f0:  cmp    $0xff,%eax
4045f5:  jne    40461d               ; not 255 -> ordinary request loop
404610:  call   4041eb <serve_irom_stage_dongle>
```

Only subclass `0xFF` enters `serve_irom_stage_dongle`, which is the function
that sends `bcm_erom.bin.usb`. The community reverse-engineering report states
the same: a unit in service mode presents `1286:8174` with interface subclass
`0xFF`, and re-enumerates with a different subclass after the iROM bootstrap
completes.

This unit reports **subclass 254 on every attempt**, including attempts where
the top panel turned yellow. It therefore never enters the iROM path, and the
`bcm_erom` to `bootloader` to `sysinit` to `drm_erom` chain never begins.

The image type codes are confirmed as: `0x01` no-op, `0x02` `sysinit.img`,
`0x03` `bootloader.img`, `0x05` `drm_erom.img`, and anything else formatted as
`NN_IMAGE`. The `0x08` this unit requests is outside the documented boot chain,
which is consistent with the request coming from the NAND bootloader's update
check rather than from the mask ROM.

Observed behaviour, both confirmed on hardware:

| Entry method | USB windows | Subclass | Request |
|---|---|---|---|
| Ordinary power-on | one, then boots | 254 | `0x08` |
| Reset held plus four MicOff presses, panel yellow | fourteen retries over roughly two minutes, then white, then chime | 254 | `0x08` |

Arming download mode changes the retry count but not the subclass. The yellow
indication is real, and the retry loop is a genuine behavioural difference, but
the interface identity stays in the post-iROM phase.

A plausible reading, not yet proven: the mask ROM only holds the USB download
path open when the NAND boot chain fails to validate. On a healthy unit the
bootloader takes over first and offers its own update window. If that is
correct, reaching subclass `0xFF` on a working unit may not be possible without
invalidating NAND, which is outside the safe boundary of this work.

## The console proxy works, and the device announces an ID

Phase 2 includes a console proxy: bytes arriving on interrupt IN `0x82` are
stripped of the `i*m*g*r*q*` markers and forwarded to the TCP client, and client
input is sent back on interrupt OUT `0x02`. That path is confirmed working here.

Before requesting an image, the device emits an ASCII string that reaches the
console:

```text
one target device connected.
 424091892ef47412 target device disconnected.
```

`424091892ef47412` was byte-identical across all fourteen attempts, so it is a
stable identifier rather than a per-boot nonce. The exchange is therefore not a
randomised challenge, which leaves open the possibility that a static response
image is the correct answer.

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
error alone does not prove rejection. What does indicate a problem is that the
device returns to the same request every time rather than advancing to the next
image in the chain.

## Untested next steps

None have been run, and all are RAM-only with no NAND writes:

1. **Interrupt an autoboot countdown.** U-Boot's `usbload N` command requests
   image type `N`, so a request for `0x08` is consistent with U-Boot already
   running and executing an update check rather than with the mask ROM. If that
   is what is happening, the console proxy is connected to a live U-Boot, and a
   keypress during an autoboot countdown would drop it to a prompt. The
   `interrupt-autoboot.py` helper feeds newlines continuously so keys are
   already waiting when the short USB window opens. Newlines are harmless: an
   empty command at a prompt, and any key stops a countdown. The full path is
   verified end to end, with keystrokes confirmed arriving at `usb_boot`.
2. **Remove `08_IMAGE`** so the request cannot be satisfied. Expected to be
   equivalent to running no tool at all, so this is mainly a control.
3. **Serve `bcm_erom.bin.usb` as `08_IMAGE`.** Weaker than it first appears: the
   iROM path sends the bootstrap with no size header, whereas the Phase 2 path
   prepends the eight-byte header, so the device would likely misparse it.

All require a power cycle, since the window only opens at boot.

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
