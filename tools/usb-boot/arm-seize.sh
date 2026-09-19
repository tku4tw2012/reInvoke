#!/bin/bash
# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT
#
# Seize the U-Boot console and hold it. Nothing is written to NAND here.
#
# arm-flash.sh couples catching the device to writing it: its console driver
# waits for the prompt and immediately sends `l2nand 83`. That is one operation
# with two failure points, and the one that fails is entry, which is a dice
# roll. Every lost entry also loses the prompt.
#
# This splits them, which is how the RAM-boot era worked. The helper and a
# console relay stay armed through as many entry attempts as it takes. When
# one finally lands, the prompt is simply held: no command is sent and nothing
# times out. Commands go in afterwards through a FIFO, at whatever pace the
# work needs.
#
# Usage: arm-seize.sh STAGING_DIR EVIDENCE_DIR
set -euo pipefail

here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "${here}/../.." && pwd)"
archive="${REINVOKE_ARCHIVE:-${repo}/../reinvoke-archive}"
staging="${1:?STAGING_DIR}"
evidence="${2:?EVIDENCE_DIR}"

# The faster-polling helper. The device presents its iROM identity briefly
# before settling, and this is the build the sessions that caught it every
# time were using.
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

# Both restart. The helper exits after its own device wait expires, and the
# relay exits when the console closes at the end of a failed entry; neither is
# a reason to stop waiting for the next attempt.
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
