#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Capture each Marvell USB enumeration into one attempt directory.

set -euo pipefail

err() {
  printf "ERROR: %s\n" "$1" >&2
  exit 1
}

read_value() {
  local path="$1"

  [[ -r "${path}" ]] || return 1
  tr -d '\n' < "${path}"
}

main() {
  local output="${1:-}"
  local script_dir
  local seen_file
  local device
  local vendor
  local bus
  local dev
  local key

  [[ "${output}" = /* ]] || err "Output path must be absolute"
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  seen_file="${output%.log}-events.txt"
  touch "${output}" "${seen_file}"

  while true; do
    for device in /sys/bus/usb/devices/*; do
      vendor="$(read_value "${device}/idVendor" 2>/dev/null || true)"
      [[ "${vendor}" == "1286" ]] || continue
      bus="$(read_value "${device}/busnum" 2>/dev/null || true)"
      dev="$(read_value "${device}/devnum" 2>/dev/null || true)"
      [[ -n "${bus}" && -n "${dev}" ]] || continue

      key="$(basename "${device}"):${bus}:${dev}"
      if ! grep -Fxq "${key}" "${seen_file}"; then
        printf "%s\t%s\n" "$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)" "${key}" \
          >> "${seen_file}"
        "${script_dir}/capture-descriptor.sh" "${bus}" "${dev}" "${output}" ||
          printf "capture failed for %s\n" "${key}" >> "${output}"
      fi
    done
    sleep 0.05
  done
}

main "$@"
