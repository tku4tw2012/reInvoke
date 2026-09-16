#!/bin/bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Arm the complete NAND flash path before the operator enters yellow mode.
#
# The reliable sequence is deliberately simple:
#   1. validate the staged image while no hardware interaction is underway;
#   2. start one console client and leave it waiting;
#   3. start one boot helper and leave it waiting;
#   4. restart only when the helper's own device wait expires;
#   5. stop only after the device prints "u2nand succeed".
#
# Nothing watches USB subclass, kills a helper, assumes a port path, or waits
# for an operator message. The helper matches the Invoke by VID:PID and serves
# whatever stage appears. This is the same ready-before-yellow path that
# completed candidate 05.7.
#
# Usage:
#   arm-flash.sh STAGING_DIR EXPECTED_SHA256 EVIDENCE_DIR
set -euo pipefail

here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "${here}/../.." && pwd)"
archive="${REINVOKE_ARCHIVE:-${repo}/../reinvoke-archive}"
staging="${1:?STAGING_DIR}"
expected_sha="${2:?EXPECTED_SHA256}"
evidence="${3:?EVIDENCE_DIR}"
helper="${INVOKE_USB_BOOT_BIN:-${archive}/tools/hk-invoke-arm-flasher/63444e82/usb_boot_arm}"
driver_path="${here}/nand-console-driver.py"
port="${INVOKE_CONSOLE_PORT:-8141}"
log="${evidence}/arm.log"

fail() {
  printf 'FAIL %s\n' "$*" >&2
  exit 1
}

[[ -d "${staging}" ]] || fail "staging not found: ${staging}"
[[ -x "${helper}" ]] || fail "helper not executable: ${helper}"
[[ -r "${driver_path}" ]] || fail "console driver missing: ${driver_path}"

for required in 06_IMAGE 07_IMAGE 08_IMAGE 09_IMAGE 79_IMAGE 81_IMAGE 82_IMAGE \
  83_IMAGE bcm_erom.bin.usb bootloader.img drm_erom.img sysinit.img; do
  [[ -f "${staging}/${required}" ]] ||
    fail "staging is missing ${required}"
done

actual_sha="$(sha256sum "${staging}/83_IMAGE" | cut -d' ' -f1)"
[[ "${actual_sha}" == "${expected_sha}" ]] ||
  fail "83_IMAGE is ${actual_sha}, expected ${expected_sha}"

[[ "$(pgrep -cx usb_boot_arm || true)" == "0" ]] ||
  fail "a boot helper is already running"
[[ "$(ps -eo cmd --no-headers | grep -c '[n]and-console-driver' || true)" == "0" ]] ||
  fail "a console driver is already running"

mkdir -p "${evidence}"
: >"${log}"

python3 -u "${driver_path}" --port "${port}" --timeout 0 \
  --console-log "${evidence}/console.raw" >>"${log}" 2>&1 &
driver=$!
helper_pid=""

cleanup() {
  [[ -z "${helper_pid}" ]] || kill "${helper_pid}" 2>/dev/null || true
  kill "${driver}" 2>/dev/null || true
}
trap cleanup EXIT

printf 'READY helper and console driver are waiting; enter yellow mode at any time\n'
printf 'payload %s %s bytes\n' "${actual_sha}" "$(stat -c%s "${staging}/83_IMAGE")"

while kill -0 "${driver}" 2>/dev/null; do
  "${helper}" 1286 8174 "${staging}/" "${port}" >>"${log}" 2>&1 &
  helper_pid=$!
  while kill -0 "${helper_pid}" 2>/dev/null &&
    kill -0 "${driver}" 2>/dev/null; do
    sleep 0.1
  done
  if ! kill -0 "${driver}" 2>/dev/null; then
    kill "${helper_pid}" 2>/dev/null || true
    wait "${helper_pid}" 2>/dev/null || true
    helper_pid=""
    break
  fi
  wait "${helper_pid}" 2>/dev/null || true
  helper_pid=""
  sleep 0.2
done

set +e
wait "${driver}"
status=$?
set -e

if [[ "${status}" == "4" ]]; then
  fail "device reported completion but command/evidence verification is incomplete; do not retry automatically"
fi

if [[ "${status}" == "0" ]] && grep -q "u2nand succeed" "${log}"; then
  printf 'FLASHED device confirmed u2nand succeed\n'
  exit 0
fi

fail "console driver exited ${status} without a write confirmation; see ${log}"
