#!/usr/bin/env bash
# Flash a staged image to the Invoke's NAND in one command.
#
# Run this, wait for READY, then put the speaker into service mode. It seizes
# the iROM window, serves the boot chain, drives U-Boot to a finished write and
# stops. One log, one verdict.
#
# The shape of this script is dictated by measurement, not preference. Of 27
# archived flashes, 23 seized on the first sighting in three to four seconds.
# The three slow ones took 28-32 minutes, and every one of those began with the
# helper already attached to a running device rather than idle on an absent
# one. So the operator is asked to reset only once the rig is genuinely idle,
# and anything that could abort after that point runs before it.
#
# Every check is a preflight. Guards that fired mid-window used to abort the
# flash over cosmetic mismatches - a U-Boot banner interleaved with boot
# chatter, a stale success line in a reused log - while the device sat waiting.
# Losing the window costs far more than the mistakes those guards caught.
#
# Usage: flash-nand.sh STAGING_DIR EXPECTED_SHA256 [EVIDENCE_DIR]
set -euo pipefail

staging="${1:?STAGING_DIR}"
expected_sha="${2:?EXPECTED_SHA256 of 83_IMAGE}"
evidence="${3:-${staging}}"

here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
archive="${REINVOKE_ARCHIVE:-${here}/../../../reinvoke-archive}"
helper="${INVOKE_USB_BOOT_BIN:-${archive}/tools/hk-invoke-arm-flasher/63444e82/usb_boot_arm}"
driver="${here}/nand-console-driver.py"
usb_path="${INVOKE_USB_PATH:-3-1.2}"
port="${INVOKE_CONSOLE_PORT:-8141}"
command="${INVOKE_NAND_COMMAND:-l2nand 83}"

log="${evidence}/flash-nand.log"

say() { printf '%s %s\n' "$(date -u +%H:%M:%S)" "$*" | tee -a "${log}"; }
die() { printf '%s FAIL %s\n' "$(date -u +%H:%M:%S)" "$*" | tee -a "${log}" >&2; exit 1; }

# ---------------------------------------------------------------- preflight
if [[ ! -d "${staging}" ]]; then
  printf '%s FAIL staging not found: %s\n' "$(date -u +%H:%M:%S)" "${staging}" >&2
  exit 1
fi

mkdir -p "${evidence}"
: >"${log}"

say "preflight"
[[ -x "${helper}" ]] || die "helper not found: ${helper}"
[[ -r "${driver}" ]] || die "console driver not found: ${driver}"

for required in 06_IMAGE 07_IMAGE 08_IMAGE 09_IMAGE 79_IMAGE 81_IMAGE 82_IMAGE \
  83_IMAGE bcm_erom.bin.usb bootloader.img drm_erom.img sysinit.img; do
  [[ -f "${staging}/${required}" ]] || die "staging is missing ${required}"
done
say "  staging complete (12 files)"

actual_sha="$(sha256sum "${staging}/83_IMAGE" | cut -d' ' -f1)"
[[ "${actual_sha}" == "${expected_sha}" ]] ||
  die "83_IMAGE is ${actual_sha}, expected ${expected_sha}"
say "  83_IMAGE ${actual_sha:0:16}... $(stat -c%s "${staging}/83_IMAGE") bytes"

# A helper from an earlier run keeps its claim on interface 0, and the device
# then drops every transfer. A stale console client is worse: the relay serves
# exactly one, so the driver connects, receives nothing and never sees U-Boot.
stale_helpers="$(pgrep -x "$(basename "${helper}")" 2>/dev/null || true)"
if [[ -n "${stale_helpers}" ]]; then
  say "  stopping stale helpers: ${stale_helpers//$'\n'/ }"
  for pid in ${stale_helpers}; do kill "${pid}" 2>/dev/null || true; done
  sleep 1
fi
pgrep -x "$(basename "${helper}")" >/dev/null 2>&1 &&
  die "a helper is still running; stop it before retrying"
say "  no competing helper"

if command -v ss >/dev/null 2>&1 &&
   ss -tn 2>/dev/null | grep -q "ESTAB.*:${port}\b"; then
  die "something already holds the console on ${port}; stop it before retrying"
fi
say "  console ${port} free"

if [[ -d "/sys/bus/usb/devices/${usb_path}" ]]; then
  say "  NOTE device is attached now; power it off so the helper starts idle"
fi

# ------------------------------------------------------------------- arm
say "READY - put the speaker into service mode now"
say "  a healthy seize takes 3-4 seconds from the first sighting"

"${driver}" --port "${port}" --command "${command}" --timeout 86400 >>"${log}" 2>&1 &
driver_pid=$!

cleanup() {
  kill "${driver_pid}" 2>/dev/null || true
  [[ -n "${helper_pid:-}" ]] && kill "${helper_pid}" 2>/dev/null || true
}
trap cleanup EXIT

# One helper at a time, restarted whenever it exits. It aborts on a libusb
# assertion (exit 134) when the device vanishes mid-session, which is routine
# rather than fatal.
while kill -0 "${driver_pid}" 2>/dev/null; do
  "${helper}" 1286 8174 "${staging}/" "${port}" >>"${log}" 2>&1 &
  helper_pid=$!
  wait "${helper_pid}" 2>/dev/null || true
  helper_pid=""
  kill -0 "${driver_pid}" 2>/dev/null || break
  sleep 0.3
done

set +e
wait "${driver_pid}"
status=$?
set -e

# ---------------------------------------------------------------- verdict
if grep -q "u2nand succeed" "${log}"; then
  say "FLASHED - device confirmed u2nand succeed"
  say "  chain: $(grep -oE 'request type 0x[0-9a-f]+' "${log}" |
    tail -7 | grep -oE '0x[0-9a-f]+' | tr '\n' ' ')"
  say "  log: ${log}"
  exit 0
fi

say "did not complete (driver exit ${status})"
say "  sightings: $(grep -c 'Device found\|Device detected' "${log}" || true)"
say "  iROM: $(grep -c 'iROM mode' "${log}" || true)"
say "  0x08 only: $(grep -c 'request type 0x08' "${log}" || true)"
say "  An 0x08-only session means service mode was not entered; reset again."
exit "${status}"
