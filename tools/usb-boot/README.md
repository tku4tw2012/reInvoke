# USB boot session tooling

Host-side tooling for reaching the Marvell BootROM on the Harman Kardon Invoke
over Micro-USB. Verified against hardware on 2026-09-01. Background and the
service-mode entry sequence are in [../../docs/usb-service-mode.md](../../docs/usb-service-mode.md).

No proprietary files are included here. The Marvell `usb_boot` binary and the
boot-chain images come from the community flashing bundle and must be staged
separately.

## Contents

| File | Purpose |
|---|---|
| `99-marvell-invoke.rules` | udev rule granting unprivileged access to the Marvell boot endpoint, and triggering descriptor capture on attach |
| `capture-descriptor.sh` | Dumps the full USB descriptor when the device appears. The boot window is only a few seconds, too short to run `lsusb -v` by hand |
| `uboot-console.py` | Console client for the `usb_boot` TCP relay. Strips telnet negotiation, logs the transcript, and forwards commands from a FIFO |
| `start-session.sh` | Brings up `usb_boot` and the console client, and refuses to run if flashable images are staged |

## Setup

Install the udev rule once:

```bash
sudo cp 99-marvell-invoke.rules /etc/udev/rules.d/
sudo udevadm control --reload-rules
```

The rule also runs `capture-descriptor.sh` on attach, which appends the full
descriptor to `/tmp/invoke-descriptor.log`. Edit the path in the rule if the
tooling lives elsewhere.

Stage a working directory containing `usb_boot`, `bcm_erom.bin.usb`,
`bootloader.img`, `sysinit.img`, `drm_erom.img`, the numbered protocol files,
and an empty `79_IMAGE`. Copy `uboot-console.py` and `start-session.sh`
alongside them, then run:

```bash
./start-session.sh
```

It reports `READY` only after `usb_boot` has entered its USB hotplug loop.

The script takes an optional variant argument controlling what is served for
image type `0x08`, which is what this unit requests:

| Variant | Effect |
|---|---|
| `stock` (default) | Serve the bundle's `08_IMAGE` |
| `absent` | Remove `08_IMAGE` so the request cannot be satisfied |
| `erom` | Serve a copy of `bcm_erom.bin.usb` as `08_IMAGE` |

The original is preserved as `08_IMAGE.stock` on first run and restored by
`./start-session.sh stock`. All variants are RAM-only and write nothing to the
device.

## Reading the session

```bash
tail -f usbboot.log      # protocol progress
tail -f /tmp/uboot.log   # U-Boot console transcript
```

Send a command to the prompt:

```bash
echo 'printenv' > /tmp/uboot_cmd
```

## Interrupting an autoboot countdown

`interrupt-autoboot.py` feeds harmless newlines into the console FIFO so
keystrokes are already waiting when the brief USB window opens. Start it, then
power cycle:

```bash
python3 interrupt-autoboot.py 180
```

This is aimed at the case where the device's request for image type `0x08`
comes from a running U-Boot executing `usbload 8`, rather than from the mask
ROM. If an autoboot countdown is in progress, a keypress drops it to a prompt.
Only newlines are sent, which are empty commands at a prompt.

## Why the console client is required

`usb_boot` blocks at `wait for connection on port: 8141` and does not watch USB
until a client attaches. Started without one, it silently misses the boot
window. The stock `run.sh` masks this by launching PuTTY as a fifth argument.

`usb_boot` also answers every telnet `DONT` and `WONT` with a further
negotiation command, so a client that replies will loop indefinitely.
`uboot-console.py` consumes those sequences without responding.

## Safety

`start-session.sh` aborts if `83_IMAGE` or `99_IMAGE` is present. The first
writes NAND; the second is reported to brick the unit unrecoverably. Keeping
them out of the working directory makes an accidental flash impossible.

The boot chain loads into RAM and writes nothing on its own. Avoid `nand`,
`nandinit`, `nanderase`, `tftp2nand`, `l2nand`, and `saveenv` at the prompt on a
working unit.
