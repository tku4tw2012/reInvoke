#!/usr/bin/env bash
# Report the sysfs port path where the Invoke's boot ROM is attached.
#
# Every tool here used to assume 3-1.2. The unit actually enumerated on 2-1.2,
# and a watcher pinned to the old path reported that the iROM window never
# appeared while the boot helper was seeing the device the whole time. Port
# paths change with a different hub, a different socket, or a second unit, so
# nothing may hard code one.
#
# Vendor and product are stable: the boot ROM always presents 1286:8174, which
# is what the pinned helper matches on.
#
# Prints the port path and exits 0 when attached, prints nothing and exits 1
# when not.
set -euo pipefail

vendor="${INVOKE_USB_VENDOR:-1286}"
product="${INVOKE_USB_PRODUCT:-8174}"

for device in /sys/bus/usb/devices/*/; do
  [[ -r "${device}idVendor" && -r "${device}idProduct" ]] || continue
  [[ "$(<"${device}idVendor")" == "${vendor}" ]] || continue
  [[ "$(<"${device}idProduct")" == "${product}" ]] || continue
  basename "${device}"
  exit 0
done
exit 1
