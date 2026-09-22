#!/usr/bin/env bash
# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT
#
# Install a reInvoke NAND bundle. Five steps, no ceremony:
#   1 release any stale session   2 start the helper polling
#   3 operator powers into yellow 4 wait for the U-Boot prompt
#   5 run the vendor program
#
# Usage: flash.sh BUNDLE_DIR EXPECTED_SHA256 EVIDENCE_DIR [APPROVAL_REF]

set -euo pipefail

bundle="${1:?BUNDLE_DIR}"
sha="${2:?EXPECTED_SHA256}"
evidence="${3:?EVIDENCE_DIR}"
approval="${4:-owner approved at the console}"

here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "${here}/../.." && pwd)"
archive="${REINVOKE_ARCHIVE:-${repo}/../reinvoke-archive}"
firmware="${INVOKE_FIRMWARE_DIR:?INVOKE_FIRMWARE_DIR must name the recovery-only staging}"
# Discovered, never assumed: the unit enumerated on 2-1.2 while every tool
# here assumed 3-1.2, and a watcher pinned to the wrong path reported that
# the iROM window never appeared. Vendor and product are the stable identity.
usb_path="${INVOKE_USB_PATH:-$("${here}/../find-invoke-usb.sh" 2>/dev/null || echo "")}"
helper="${INVOKE_USB_BOOT_BIN:-${archive}/tools/hk-invoke-arm-flasher/63444e82/usb_boot_arm}"

[[ -d "${evidence}" ]] && { echo "EVIDENCE_DIR must not already exist" >&2; exit 1; }
session="${evidence%/}.session"
rm -rf "${session}"; mkdir -p "${session}"; chmod 0700 "${session}"

# 1. A previous run that died leaves the flock held; nothing else may hold it.
lock="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/reinvoke-usb-boot-$(id -u).lock"
if [[ -e "${lock}" ]]; then
  holder="$(fuser "${lock}" 2>/dev/null | tr -s ' ' '\n' | grep -E '^[0-9]+$' | head -1 || true)"
  [[ -n "${holder}" ]] && { echo "releasing stale session ${holder}"; kill "${holder}" 2>/dev/null || true; sleep 1; }
fi

# 2. The helper must already be polling before the device appears; starting it
#    afterwards races re-enumeration and fails with LIBUSB_ERROR_NO_DEVICE.
INVOKE_ATTEMPT_DIR="${session}" INVOKE_FIRMWARE_DIR="${firmware}" \
INVOKE_USB_BOOT_BIN="${helper}" INVOKE_USB_BOOT_KIND=arm \
  setsid nohup "${here}/start-session.sh" absent \
  >"${session}/start.log" 2>&1 </dev/null &
for _ in $(seq 1 100); do grep -q READY "${session}/start.log" 2>/dev/null && break; sleep 0.2; done
grep -q READY "${session}/start.log" || { cat "${session}/start.log" >&2; exit 1; }

# 3. Operator action. No timeout: the person at the speaker decides.
echo "READY. Power off, hold Reset, restore power, press MicOff 4x within 5s."
until grep -q 'MV88DE3100' "${session}/uboot-console.log" 2>/dev/null; do sleep 1; done

# 4/5. U-Boot is up; program once and never retry automatically.
echo "U-Boot reached; programming."
exec node "${here}/flash-native-once.mjs" "${bundle}" flash \
  --expected-sha256 "${sha}" --confirm ERASE-AND-FLASH-NATIVE \
  --approval-ref "${approval}" --firmware-dir "${firmware}" \
  --session-dir "${session}" --usb-path "${usb_path}" --evidence "${evidence}"
