#!/usr/bin/env bash
# Catch the Invoke's iROM window and hand off to the pinned USB boot helper.
#
# Why this exists
#
# The helper claims whatever it finds. Left polling across a power cycle it
# grabs the device at subclass 0xFE, which is past iROM, and then loops on
# 08_IMAGE forever. Every successful flash in the archive (candidates 01, 02,
# 03 and 05.2) started from subclass 0xFF instead.
#
# So this watches the kernel's own USB descriptors, never opening the device,
# and starts exactly one helper the moment 0xFF appears. The measured window
# is wide enough: the helper waits a full second before talking and the whole
# iROM bootstrap takes about 20 ms.
#
# It also solves the two failures that wasted the most time:
#   - the helper exits after a 120 s device wait, so operators had to be told
#     to hurry; this restarts the watch indefinitely and never asks
#   - two helpers both claiming interface 0 make the device drop immediately
#     after each transfer; flock makes a second instance impossible
#
# Usage: catch-irom.sh STAGING_DIR [ATTEMPT_DIR]
set -euo pipefail

staging="${1:?STAGING_DIR}"
attempt="${2:-${staging}}"
here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
archive="${REINVOKE_ARCHIVE:-${here}/../../../reinvoke-archive}"
helper="${INVOKE_USB_BOOT_BIN:-${archive}/tools/hk-invoke-arm-flasher/63444e82/usb_boot_arm}"
usb_path="${INVOKE_USB_PATH:-3-1.2}"
port="${INVOKE_CONSOLE_PORT:-8141}"
log="${attempt}/usbboot.log"
state="${attempt}/catch-irom.state"

[[ -d "${staging}" ]] || { echo "staging not found: ${staging}" >&2; exit 1; }
[[ -x "${helper}" ]] || { echo "helper not found: ${helper}" >&2; exit 1; }

# One catcher only. Two helpers contending for interface 0 is a real, observed
# failure: both log "Claimed interface 0" and the device drops every transfer.
exec 9>"${attempt}/catch-irom.lock"
flock -n 9 || { echo "another catcher already holds the lock" >&2; exit 1; }

mkdir -p "${attempt}"
: >"${state}"
note() { printf '%s %s\n' "$(date -u +%H:%M:%S)" "$*" >>"${state}"; }

# The staged recovery files must be complete before the device appears; the
# helper reads them per request and a missing one aborts the attempt.
for required in 06_IMAGE 07_IMAGE 08_IMAGE 09_IMAGE 79_IMAGE 81_IMAGE 82_IMAGE \
  bcm_erom.bin.usb bootloader.img drm_erom.img sysinit.img; do
  [[ -f "${staging}/${required}" ]] ||
    { echo "staging is missing ${required}" >&2; exit 1; }
done

subclass_now() {
  local file
  for file in /sys/bus/usb/devices/"${usb_path}":*/bInterfaceSubClass; do
    [[ -r "${file}" ]] || continue
    tr -d '\n' <"${file}" | tr '[:upper:]' '[:lower:]'
    return 0
  done
  return 1
}

is_marvell() {
  local device="/sys/bus/usb/devices/${usb_path}"
  [[ -r "${device}/idVendor" && -r "${device}/idProduct" ]] || return 1
  [[ "$(<"${device}/idVendor")" == "1286" ]] || return 1
  [[ "$(<"${device}/idProduct")" == "8174" ]]
}

note "watching ${usb_path} for iROM (subclass ff); power-cycle into service mode whenever ready"
echo "READY. Waiting for iROM. There is no timeout; take as long as you need."

last=""
while true; do
  if is_marvell && current="$(subclass_now)"; then
    if [[ "${current}" != "${last}" ]]; then
      note "subclass ${current}"
      last="${current}"
    fi
    if [[ "${current}" == "ff" ]]; then
      note "iROM detected; starting one helper"
      echo "iROM detected; serving the boot chain."
      "${helper}" 1286 8174 "${staging}/" "${port}" >>"${log}" 2>&1 &
      helper_pid=$!
      printf '%s\n' "${helper_pid}" >"${attempt}/usb-boot.pid"
      note "helper ${helper_pid}"
      wait "${helper_pid}" || true
      note "helper exited $?"
      exit 0
    fi
  elif [[ -n "${last}" ]]; then
    note "device gone"
    last=""
  fi
  sleep 0.05
done
