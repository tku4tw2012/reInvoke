#!/bin/bash
# Bring up the Marvell usb_boot session for the Harman Kardon Invoke.
#
# Starts usb_boot and attaches the console client that it requires before it
# will watch USB. Logs:
#   usbboot.log  - usb_boot protocol output
#   /tmp/uboot.log - U-Boot console transcript
#
# Send commands to the prompt with:  echo 'help' > /tmp/uboot_cmd
set -u

DIR="$(cd "$(dirname "$0")" && pwd)"
PORT=8141
cd "$DIR"

for f in usb_boot bcm_erom.bin.usb bootloader.img sysinit.img drm_erom.img; do
  if [ ! -f "$f" ]; then
    echo "FATAL: missing required file: $f" >&2
    exit 1
  fi
done

for f in 83_IMAGE 99_IMAGE; do
  if [ -f "$f" ]; then
    echo "FATAL: $f is present. Remove it before running against a working unit." >&2
    exit 1
  fi
done

if [ ! -f 79_IMAGE ]; then
  cp 79_IMAGE.uboot_cmdline 79_IMAGE
fi
if grep -qvE '^#|^$' 79_IMAGE 2>/dev/null; then
  echo "WARNING: 79_IMAGE contains commands that will run automatically:" >&2
  grep -vE '^#|^$' 79_IMAGE >&2
fi

for pid in $(pgrep -f "usb_boot $((1286))" 2>/dev/null) $(pgrep -f "uboot-console.py" 2>/dev/null); do
  kill "$pid" 2>/dev/null
done
sleep 1

rm -f usbboot.log /tmp/uboot.log

# stdbuf keeps output line-buffered; without it stdout is block-buffered to the
# log file and progress stays invisible.
nohup stdbuf -oL -eL ./usb_boot 1286 8174 ./ "$PORT" > usbboot.log 2>&1 &
sleep 2

if ! ss -ltn 2>/dev/null | grep -q ":$PORT"; then
  echo "FATAL: usb_boot did not open port $PORT" >&2
  exit 1
fi

# usb_boot stays blocked on "wait for connection" until a console client
# attaches, and only then begins polling USB.
nohup python3 "$DIR/uboot-console.py" > /tmp/uboot-console-err.log 2>&1 &
sleep 3

if grep -q "polling_for_hotplug_event" usbboot.log; then
  echo "READY: usb_boot is polling for the device."
  echo "Arm download mode: hold Reset, reconnect power, press MicOff 4x within 5s."
else
  echo "FATAL: usb_boot never entered the hotplug loop." >&2
  tail -5 usbboot.log >&2
  exit 1
fi
