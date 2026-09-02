#!/bin/bash
# Triggered by udev when the Marvell boot endpoint appears.
# Captures the descriptor during the brief window, which is too short to catch
# by hand. Invoked as: capture-descriptor.sh <busnum> <devnum>
OUT=/tmp/invoke-descriptor.log
BUS="${1:-}"
DEV="${2:-}"

{
  echo "=== $(date +%H:%M:%S.%N) bus=$BUS dev=$DEV ==="
  SYS="/sys/bus/usb/devices"
  for d in "$SYS"/*; do
    [ -r "$d/idVendor" ] || continue
    if [ "$(cat "$d/idVendor" 2>/dev/null)" = "1286" ]; then
      echo "--- $d ---"
      for f in idVendor idProduct bcdDevice bDeviceClass bDeviceSubClass \
               bDeviceProtocol bNumConfigurations bNumInterfaces speed version \
               manufacturer product serial; do
        [ -r "$d/$f" ] && echo "$f=$(cat "$d/$f" 2>/dev/null)"
      done
      for i in "$d"/*:*; do
        [ -r "$i/bInterfaceClass" ] || continue
        echo "  interface $(basename "$i")"
        for f in bInterfaceNumber bAlternateSetting bInterfaceClass \
                 bInterfaceSubClass bInterfaceProtocol bNumEndpoints interface; do
          [ -r "$i/$f" ] && echo "    $f=$(cat "$i/$f" 2>/dev/null)"
        done
        for e in "$i"/ep_*; do
          [ -d "$e" ] || continue
          echo "    endpoint $(basename "$e") type=$(cat "$e/type" 2>/dev/null) addr=$(cat "$e/bEndpointAddress" 2>/dev/null) maxpacket=$(cat "$e/wMaxPacketSize" 2>/dev/null)"
        done
      done
      if [ -r "$d/descriptors" ]; then
        echo "  raw descriptors:"
        od -An -tx1 -v "$d/descriptors" 2>/dev/null | sed 's/^/    /'
      fi
    fi
  done
  echo "=== end ==="
} >> "$OUT" 2>&1
