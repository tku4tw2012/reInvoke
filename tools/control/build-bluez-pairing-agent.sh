#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Build the static ARMv7 reInvoke BlueZ pairing agent.

set -euo pipefail

readonly COMPILER_SHA256="da77d2b40ffceb388d2a201877a83fb30f054d1f196e63f366572e307d7a63d6"
readonly STRIP_SHA256="fb5832708c993a6f196aac6fca7593a24c90f7b8316ede91382e5b55a88608dc"

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: build-bluez-pairing-agent.sh \
  --dbus-source PATH --sysroot PATH --output PATH
EOF
  exit "${exit_code}"
}

err() {
  printf "ERROR: %s\n" "$1" >&2
  exit 1
}

verify_sha256() {
  local path="$1"
  local expected="$2"

  printf "%s  %s\n" "${expected}" "${path}" |
    sha256sum --check --status ||
    err "checksum mismatch: ${path}"
}

main() {
  local dbus_source=""
  local sysroot=""
  local output=""
  local partial
  local compiler
  local strip_tool
  local script_dir
  local sysroot_include

  while (( $# > 0 )); do
    case "$1" in
      --dbus-source)
        dbus_source="${2:-}"
        shift 2
        ;;
      --sysroot)
        sysroot="${2:-}"
        shift 2
        ;;
      --output)
        output="${2:-}"
        shift 2
        ;;
      --help|-h)
        usage
        ;;
      *)
        err "unknown argument: $1"
        ;;
    esac
  done

  [[ -f "${dbus_source}/dbus/.libs/libdbus-1.a" ]] ||
    err "--dbus-source must name a built D-Bus source tree"
  if [[ -d "${sysroot}/usr/include" ]]; then
    sysroot_include="${sysroot}/usr/include"
  elif [[ -d "${sysroot}/include" ]]; then
    sysroot_include="${sysroot}/include"
  else
    err "--sysroot must name the ARM build sysroot"
  fi
  [[ -n "${output}" ]] || err "--output is required"
  output="$(realpath --canonicalize-missing "${output}")"
  [[ ! -e "${output}" ]] || err "refusing to overwrite output: ${output}"

  compiler="$(command -v arm-linux-gnueabihf-gcc)"
  strip_tool="$(command -v arm-linux-gnueabihf-strip)"
  verify_sha256 "${compiler}" "${COMPILER_SHA256}"
  verify_sha256 "${strip_tool}" "${STRIP_SHA256}"

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  partial="${output}.partial"
  [[ ! -e "${partial}" ]] || err "stale partial output exists: ${partial}"
  trap 'rm -f -- "${partial}"' EXIT
  mkdir -p "$(dirname "${output}")"
  "${compiler}" -std=c11 -O2 -Wall -Wextra -Werror -static \
    -ffile-prefix-map="${dbus_source}"=. \
    -ffile-prefix-map="${script_dir}"=. \
    -I"${dbus_source}" -I"${dbus_source}/dbus" \
    -I"${sysroot_include}" \
    "${script_dir}/bluez-pairing-agent.c" \
    "${dbus_source}/dbus/.libs/libdbus-1.a" \
    -lpthread -o "${partial}"
  "${strip_tool}" "${partial}"
  file "${partial}" | grep -q "ELF 32-bit.*ARM.*statically linked" ||
    err "output is not a static 32-bit ARM ELF"
  mv "${partial}" "${output}"
  trap - EXIT
  file "${output}"
  sha256sum "${output}"
}

main "$@"
