#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Run the complete host validation surface for the native RAM platform.

set -euo pipefail

err() {
  printf "ERROR: %s\n" "$1" >&2
  exit 1
}

main() {
  local script_dir
  local repo_root
  local archive_root
  local pairing_policy_test

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  repo_root="$(cd "${script_dir}/.." && pwd)"
  archive_root="${REINVOKE_ARCHIVE:-${repo_root}/../reinvoke-archive}"
  if [[ "${1:-}" == "--archive-root" ]]; then
    [[ -n "${2:-}" ]] || err "--archive-root requires a path"
    archive_root="$2"
    shift 2
  fi
  (( $# == 0 )) || err "unknown argument: $1"

  for command_name in bash cc find node rm sh xargs; do
    command -v "${command_name}" >/dev/null ||
      err "'${command_name}' is required"
  done

  "${script_dir}/mcu-interface/test.sh" --archive-root "${archive_root}"
  "${script_dir}/dsp-interface/test.sh" --archive-root "${archive_root}"
  "${script_dir}/mic-capture/test.sh" --archive-root "${archive_root}"
  "${script_dir}/provisioning/test.sh" --archive-root "${archive_root}"
  pairing_policy_test="${repo_root}/.bluez-pairing-policy-test.$$"
  [[ ! -e "${pairing_policy_test}" ]] ||
    err "stale pairing policy test exists: ${pairing_policy_test}"
  trap 'rm -f -- "${pairing_policy_test}"' EXIT
  cc -std=c11 -O2 -Wall -Wextra -Werror \
    "${script_dir}/control/bluez-pairing-policy_test.c" \
    -o "${pairing_policy_test}"
  "${pairing_policy_test}"
  rm -f -- "${pairing_policy_test}"
  trap - EXIT
  node --test \
    "${script_dir}/control/"*.test.mjs \
    "${script_dir}/emulation/"*.test.mjs \
    "${script_dir}/provisioning/"*.test.mjs
  "${script_dir}/usb-boot/boot-native-ram-test.sh"
  find "${script_dir}" -type f -name "*.sh" -print0 |
    xargs --null --max-args=1 bash -n
  sh -n "${script_dir}/usb-boot/native-ram-init"
}

main "$@"
