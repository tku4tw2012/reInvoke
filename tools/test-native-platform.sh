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

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  repo_root="$(cd "${script_dir}/.." && pwd)"
  archive_root="${REINVOKE_ARCHIVE:-${repo_root}/../reinvoke-archive}"
  if [[ "${1:-}" == "--archive-root" ]]; then
    [[ -n "${2:-}" ]] || err "--archive-root requires a path"
    archive_root="$2"
    shift 2
  fi
  (( $# == 0 )) || err "unknown argument: $1"

  for command_name in bash find node sh xargs; do
    command -v "${command_name}" >/dev/null ||
      err "'${command_name}' is required"
  done

  "${script_dir}/mcu-interface/test.sh" --archive-root "${archive_root}"
  "${script_dir}/dsp-interface/test.sh" --archive-root "${archive_root}"
  "${script_dir}/provisioning/test.sh" --archive-root "${archive_root}"
  node --test \
    "${script_dir}/control/"*.test.mjs \
    "${script_dir}/emulation/"*.test.mjs
  find "${script_dir}" -type f -name "*.sh" -print0 |
    xargs --null --max-args=1 bash -n
  sh -n "${script_dir}/usb-boot/native-ram-init"
}

main "$@"
