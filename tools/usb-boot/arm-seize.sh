#!/bin/bash
# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT
#
# Seize the U-Boot console and hold it. Nothing is written to NAND here.
#
# Acquisition is a timed USB handshake. Arm the fast poller and holding relay
# before the operator's power cycle; the 2026-09-28 baseline succeeded on
# attempt 1. Seizing never waits for prompt-text matching or triggers a flash.
# Commands go through the FIFO only after acquisition, as a separate action.
#
# Usage: arm-seize.sh STAGING_DIR EVIDENCE_DIR
set -euo pipefail

here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "${here}/../.." && pwd)"
archive="${REINVOKE_ARCHIVE:-${repo}/../reinvoke-archive}"
staging="${1:?STAGING_DIR}"
evidence="${2:?EVIDENCE_DIR}"

# Poll every 20 ms with a 150 ms initial attach delay to fit the iROM window.
helper="${INVOKE_USB_BOOT_BIN:-${archive}/tools/hk-invoke-arm-flasher/fast-poll/usb_boot_arm}"
port="${INVOKE_CONSOLE_PORT:-8141}"
log="${evidence}/seize.log"

fail() { printf 'FAIL %s\n' "$*" >&2; exit 1; }

[[ -d "${staging}" ]] || fail "staging not found: ${staging}"
[[ -x "${helper}" ]] || fail "helper not executable: ${helper}"
[[ -r "${here}/uboot-console.py" ]] || fail "console relay missing"

# 08_IMAGE must be absent. A device that has not entered recovery asks for
# 0x08 and resumes its normal boot once it is answered, so answering helps it
# leave the state being caught.
[[ ! -e "${staging}/08_IMAGE" ]] ||
  fail "staging contains 08_IMAGE; rename it to 08_IMAGE.withheld-for-uboot-access"

for required in 06_IMAGE 07_IMAGE 09_IMAGE 79_IMAGE 81_IMAGE 82_IMAGE \
  bcm_erom.bin.usb bootloader.img drm_erom.img sysinit.img; do
  [[ -f "${staging}/${required}" ]] || fail "staging is missing ${required}"
done

[[ "$(pgrep -cx usb_boot_arm || true)" == "0" ]] ||
  fail "a boot helper is already running"
[[ "$(ps -eo cmd --no-headers | grep -c '[u]boot-console.py' || true)" == "0" ]] ||
  fail "a console relay is already running"

mkdir -p "${evidence}"
: >"${log}"
: >/tmp/uboot.log

cleanup() {
  for pid in "${console_pid:-}" "${helper_pid:-}"; do
    [[ -z "${pid}" ]] || kill "${pid}" 2>/dev/null || true
  done
}
trap cleanup EXIT

# Supervisors preserve arming across host wait expiry or transport closure.
# Process restarts and USB re-enumeration are not operator attempt counts.
(
  while true; do
    python3 -u "${here}/uboot-console.py" >>"${log}" 2>&1 || true
    sleep 0.5
  done
) &
console_pid=$!

(
  while true; do
    "${helper}" 1286 8174 "${staging}/" "${port}" >>"${log}" 2>&1 || true
    sleep 0.2
  done
) &
helper_pid=$!

printf 'READY helper and console relay are waiting; enter yellow mode at any time\n'
printf 'staging  %s\n' "${staging}"
printf 'console  /tmp/uboot.log\n'
printf 'commands /tmp/uboot_cmd\n'
printf 'NOTHING is sent to the device; the prompt is held until a command is written\n'

wait
