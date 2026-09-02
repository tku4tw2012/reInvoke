#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Triggered by udev when the Marvell boot endpoint appears.
# Captures the descriptor during the brief window, which is too short to catch
# by hand. Invoked as: capture-descriptor.sh <busnum> <devnum> [output]

set -euo pipefail

read_value() {
  local path="$1"

  [[ -r "${path}" ]] || return 1
  tr -d '\n' < "${path}" 2>/dev/null
}

main() {
  local requested_bus="${1:-}"
  local requested_dev="${2:-}"
  local output="${3:-/tmp/invoke-descriptor.log}"
  local sysfs="/sys/bus/usb/devices"
  local device
  local interface
  local endpoint
  local field
  local value
  local bus
  local dev

  [[ "${output}" = /* ]] || {
    printf "ERROR: output path must be absolute\n" >&2
    exit 2
  }
  [[ -z "${requested_bus}" || "${requested_bus}" =~ ^[0-9]+$ ]] || {
    printf "ERROR: bus number must be numeric\n" >&2
    exit 2
  }
  [[ -z "${requested_dev}" || "${requested_dev}" =~ ^[0-9]+$ ]] || {
    printf "ERROR: device number must be numeric\n" >&2
    exit 2
  }
  mkdir -p "$(dirname "${output}")"

  {
    printf "=== %s requested_bus=%s requested_dev=%s ===\n" \
      "$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)" \
      "${requested_bus}" "${requested_dev}"
    for device in "${sysfs}"/*; do
      [[ "$(read_value "${device}/idVendor" 2>/dev/null || true)" == "1286" ]] ||
        continue
      bus="$(read_value "${device}/busnum" 2>/dev/null || true)"
      dev="$(read_value "${device}/devnum" 2>/dev/null || true)"
      if [[ -n "${requested_bus}" ]] &&
        ((10#${bus:-0} != 10#${requested_bus})); then
        continue
      fi
      if [[ -n "${requested_dev}" ]] &&
        ((10#${dev:-0} != 10#${requested_dev})); then
        continue
      fi

      printf "%s\n" "--- ${device} ---"
      for field in idVendor idProduct bcdDevice bDeviceClass bDeviceSubClass \
        bDeviceProtocol bNumConfigurations bNumInterfaces speed version \
        manufacturer product serial; do
        value="$(read_value "${device}/${field}" 2>/dev/null || true)"
        [[ -n "${value}" ]] && printf "%s=%s\n" "${field}" "${value}"
      done
      for interface in "${device}"/*:*; do
        [[ -r "${interface}/bInterfaceClass" ]] || continue
        printf "  interface %s\n" "$(basename "${interface}")"
        for field in bInterfaceNumber bAlternateSetting bInterfaceClass \
          bInterfaceSubClass bInterfaceProtocol bNumEndpoints interface; do
          value="$(read_value "${interface}/${field}" 2>/dev/null || true)"
          [[ -n "${value}" ]] && printf "    %s=%s\n" "${field}" "${value}"
        done
        for endpoint in "${interface}"/ep_*; do
          [[ -d "${endpoint}" ]] || continue
          printf "    endpoint %s type=%s addr=%s maxpacket=%s\n" \
            "$(basename "${endpoint}")" \
            "$(read_value "${endpoint}/type" 2>/dev/null || true)" \
            "$(read_value "${endpoint}/bEndpointAddress" 2>/dev/null || true)" \
            "$(read_value "${endpoint}/wMaxPacketSize" 2>/dev/null || true)"
        done
      done
      if [[ -r "${device}/descriptors" ]]; then
        printf "  raw descriptors:\n"
        od -An -tx1 -v "${device}/descriptors" 2>/dev/null |
          sed 's/^/    /' || printf "    unavailable after disconnect\n"
      fi
    done
    printf "=== end ===\n"
  } >> "${output}" 2>&1
}

main "$@"
