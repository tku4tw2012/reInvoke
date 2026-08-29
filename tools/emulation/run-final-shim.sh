#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Run a final-firmware ARM binary with synthetic I2C and ALSA ioctls.

set -euo pipefail

err() {
  printf "ERROR: %s\n" "$1" >&2
  exit 1
}

require_isolated_network() {
  local interface_name
  local line_number=0

  while IFS=: read -r interface_name _; do
    line_number=$((line_number + 1))
    if (( line_number <= 2 )); then
      continue
    fi
    interface_name="${interface_name//[[:space:]]/}"
    if [[ -n "${interface_name}" && "${interface_name}" != "lo" ]]; then
      err "Refusing network namespace with non-loopback interface ${interface_name}"
    fi
  done < /proc/net/dev
}

main() {
  local repo_root
  local archive_root
  local emulation_root
  local sandbox_root
  local shim_path

  repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
  archive_root="${REINVOKE_ARCHIVE:-${repo_root}/../reinvoke-archive}"
  emulation_root="${archive_root}/emulation"
  sandbox_root="${emulation_root}/sandbox-final"
  shim_path="${emulation_root}/invoke-ioctl-shim.so"

  command -v bwrap >/dev/null || err "'bwrap' is required"
  command -v qemu-arm-static >/dev/null || err "'qemu-arm-static' is required"
  [[ -d "${sandbox_root}" ]] || err "Final-firmware sandbox not found"
  [[ -f "${shim_path}" ]] || err "Build ${shim_path} before running"
  (( $# > 0 )) || err "Provide a guest command and optional arguments"
  require_isolated_network

  exec bwrap \
    --bind "${sandbox_root}" / \
    --ro-bind "$(command -v qemu-arm-static)" /qemu-arm-static \
    --ro-bind "${shim_path}" /invoke-ioctl-shim.so \
    --dev /dev \
    --dir /dev/snd \
    --dev-bind /dev/null /dev/snd/controlC0 \
    --dev-bind /dev/null /dev/i2c-0 \
    --proc /proc \
    --share-net \
    --die-with-parent \
    --setenv LD_PRELOAD /invoke-ioctl-shim.so \
    --setenv INVOKE_IOCTL_LOG "${INVOKE_IOCTL_LOG:-/tmp/invoke-ioctl.log}" \
    /qemu-arm-static -L / "$@"
}

main "$@"
