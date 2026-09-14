#!/usr/bin/env bash
# Keep one USB boot helper camped on the Invoke so service mode is never missed.
#
# Why this exists
#
# The helper exits after a 120 s wait for the device, so an operator had to
# power-cycle on the host's schedule rather than their own. This supervises it
# instead: one helper, restarted whenever it exits, for as long as it takes.
#
# An earlier revision gated the helper on USB interface subclass 0xFF and
# refused to start at 0xFE. That was wrong, and docs/uboot-access.md already
# said so before it was written: "Successful traces included both FE and FF:
# the early claim that FE inherently blocked recovery was incorrect. Panel
# colour and subclass do not identify a particular executing stage." Gating on
# 0xFF made the catcher unable to fire at all on a unit that enumerated only at
# 0xFE, and cost several power cycles before the log showed it never triggered.
#
# The discriminator is the request sequence, not the subclass. The same doc:
# "Early ordinary-power and some yellow-mode trials requested only 08_IMAGE,
# then disconnected. Successful recovery requested the full 09_IMAGE chain."
# So this reports which request types arrived, and an 08-only session is named
# as a failed service-mode entry rather than left looking like a missed catch.
#
# Two helpers both claiming interface 0 make the device drop every transfer, so
# flock makes a second instance impossible and the loop is strictly sequential.
#
# Usage: catch-irom.sh STAGING_DIR [ATTEMPT_DIR]
set -euo pipefail

staging="${1:?STAGING_DIR}"
attempt="${2:-${staging}}"
here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
archive="${REINVOKE_ARCHIVE:-${here}/../../../reinvoke-archive}"
helper="${INVOKE_USB_BOOT_BIN:-${archive}/tools/hk-invoke-arm-flasher/63444e82/usb_boot_arm}"
# Discovered, never assumed: the unit enumerated on 2-1.2 while every tool
# here assumed 3-1.2, and a watcher pinned to the wrong path reported that
# the iROM window never appeared. Vendor and product are the stable identity.
usb_path="${INVOKE_USB_PATH:-$("${here}/../find-invoke-usb.sh" 2>/dev/null || echo "")}"
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
touch "${log}"
: >"${state}"
note() { printf '%s %s\n' "$(date -u +%H:%M:%S)" "$*" >>"${state}"; }

# The staged recovery files must be complete before the device appears; the
# helper reads them per request and a missing one aborts the attempt.
for required in 06_IMAGE 07_IMAGE 08_IMAGE 09_IMAGE 79_IMAGE 81_IMAGE 82_IMAGE \
  bcm_erom.bin.usb bootloader.img drm_erom.img sysinit.img; do
  [[ -f "${staging}/${required}" ]] ||
    { echo "staging is missing ${required}" >&2; exit 1; }
done

note "camping one helper on ${usb_path}; power-cycle into service mode whenever ready"
echo "READY. A helper is camped. There is no timeout; take as long as you need."
echo
echo "Reset at your own pace, as often as you like. The iROM (0xFF) window is"
echo "intermittent on this unit: the successful candidate 05.1 flash logged 12"
echo "ordinary 0x08 sessions and 20 helper exits over 31 minutes before it"
echo "caught one. An 0x08 session is a normal miss, not a fault to chase."

request_types_since() {
  local offset="$1"
  tail -c "+$((offset + 1))" "${log}" 2>/dev/null |
    grep -oE 'request type 0x[0-9a-f]+' | sort -u | tr '\n' ' ' || true
}

while true; do
  offset="$(wc -c <"${log}" 2>/dev/null || echo 0)"
  "${helper}" 1286 8174 "${staging}/" "${port}" >>"${log}" 2>&1 &
  helper_pid=$!
  printf '%s\n' "${helper_pid}" >"${attempt}/usb-boot.pid"
  status=0
  wait "${helper_pid}" || status=$?

  # Name the outcome from the request sequence. The subclass does not identify
  # the executing stage, but an 08-only session is a failed service-mode entry
  # and must not be reported as a missed catch.
  types="$(request_types_since "${offset}")"
  if [[ "${types}" == *0x09* ]]; then
    note "helper ${helper_pid} exited ${status}; served the 09 chain [${types}]"
    echo "Served the 09_IMAGE chain. Check the console driver for the flash."
  elif [[ -n "${types}" ]]; then
    note "helper ${helper_pid} exited ${status}; service mode NOT entered [${types}]"
    echo "Device answered but asked only for [${types}] - not in service mode."
    echo "Re-run the entry sequence above; keep Reset held the whole time."
  else
    note "helper ${helper_pid} exited ${status}; device did not appear"
  fi
  sleep 0.5
done
