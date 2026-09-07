#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Build the static ARMv7 reInvoke HCI initializer from retained BlueZ source.

set -euo pipefail

readonly BLUEZ_ARCHIVE_SHA256="8863717113c4897e2ad3271fc808ea245319e6fd95eed2e934fae8e0894e9b88"
readonly COMPILER_SHA256="da77d2b40ffceb388d2a201877a83fb30f054d1f196e63f366572e307d7a63d6"
readonly STRIP_SHA256="fb5832708c993a6f196aac6fca7593a24c90f7b8316ede91382e5b55a88608dc"
readonly CC1_SHA256="97115f5b191cf21bc2e178622aa6b3921438975df31944d6ab26c82757463e2a"
readonly COLLECT2_SHA256="0e31b155b9c09edae3de5a9b8343757e37c1f3810be45490b9e9b8a5fba5cfba"
readonly ASSEMBLER_SHA256="ac5370affef85aabd57abb3c3767a9ac95a76bd80b710b70dee1059518773653"
readonly LINKER_SHA256="d22dba386e10e11a340d6ebc0f37254782ee6b6aea315e49e0ac6b350cc8a78d"
readonly SYSROOT_MANIFEST_SHA256="bd39640b96ef4adc6ef4bff1870a5bef2ddacb63b06f6f623816136565124012"

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: build-hci-init.sh --bluez-archive PATH --sysroot PATH --output PATH
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

tree_manifest_sha256() {
  local root="$1"

  (
    cd "${root}"
    find -L . -type f -print0 |
      sort -z |
      xargs -0 sha256sum
  ) |
    sha256sum |
    cut -d " " -f 1
}

main() {
  local bluez_archive=""
  local sysroot=""
  local output=""
  local compiler
  local strip_tool
  local cc1
  local collect2
  local assembler
  local linker
  local script_dir
  local work_dir
  local source_dir
  local include_dir
  local partial
  local actual_manifest

  while (($# > 0)); do
    case "$1" in
      --bluez-archive)
        bluez_archive="${2:-}"
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

  [[ -f "${bluez_archive}" ]] ||
    err "--bluez-archive must name the BlueZ 5.55 source archive"
  [[ -d "${sysroot}/usr/include" && -d "${sysroot}/usr/lib" ]] ||
    err "--sysroot must name the retained ARM build sysroot"
  [[ -n "${output}" ]] || err "--output is required"
  output="$(realpath --canonicalize-missing "${output}")"
  [[ ! -e "${output}" ]] || err "refusing to overwrite output: ${output}"

  for command_name in \
    cut file find mktemp realpath sha256sum sort tar xargs; do
    command -v "${command_name}" >/dev/null ||
      err "'${command_name}' is required"
  done

  compiler="$(command -v arm-linux-gnueabihf-gcc)"
  strip_tool="$(command -v arm-linux-gnueabihf-strip)"
  cc1="$(realpath "$("${compiler}" -print-prog-name=cc1)")"
  collect2="$(realpath "$("${compiler}" -print-prog-name=collect2)")"
  assembler="$(realpath "$("${compiler}" -print-prog-name=as)")"
  linker="$(realpath "$("${compiler}" -print-prog-name=ld)")"
  verify_sha256 "${bluez_archive}" "${BLUEZ_ARCHIVE_SHA256}"
  verify_sha256 "${compiler}" "${COMPILER_SHA256}"
  verify_sha256 "${strip_tool}" "${STRIP_SHA256}"
  verify_sha256 "${cc1}" "${CC1_SHA256}"
  verify_sha256 "${collect2}" "${COLLECT2_SHA256}"
  verify_sha256 "${assembler}" "${ASSEMBLER_SHA256}"
  verify_sha256 "${linker}" "${LINKER_SHA256}"
  actual_manifest="$(tree_manifest_sha256 "${sysroot}")"
  [[ "${actual_manifest}" == "${SYSROOT_MANIFEST_SHA256}" ]] ||
    err "ARM sysroot manifest mismatch"

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  work_dir="$(mktemp -d)"
  partial="${output}.partial"
  trap 'rm -rf -- "${work_dir}"; rm -f -- "${partial}"' EXIT
  tar -xJf "${bluez_archive}" -C "${work_dir}" \
    bluez-5.55/lib/bluetooth.c \
    bluez-5.55/lib/bluetooth.h \
    bluez-5.55/lib/hci.c \
    bluez-5.55/lib/hci.h \
    bluez-5.55/lib/hci_lib.h \
    bluez-5.55/lib/mgmt.h
  source_dir="${work_dir}/bluez-5.55"
  include_dir="${work_dir}/include/bluetooth"
  mkdir -p "${include_dir}"
  cp "${source_dir}/lib/bluetooth.h" "${include_dir}/"
  cp "${source_dir}/lib/hci.h" "${include_dir}/"
  cp "${source_dir}/lib/hci_lib.h" "${include_dir}/"
  cp "${source_dir}/lib/mgmt.h" "${include_dir}/"

  "${compiler}" --sysroot="${sysroot}" \
    -std=c11 -O2 -Wall -Wextra -Werror -static \
    -ffile-prefix-map="${work_dir}"=. \
    -ffile-prefix-map="${script_dir}"=. \
    -I"${work_dir}/include" \
    "${script_dir}/hci-init.c" \
    "${source_dir}/lib/bluetooth.c" \
    "${source_dir}/lib/hci.c" \
    -o "${partial}"
  "${strip_tool}" "${partial}"
  file "${partial}" | grep -q "ELF 32-bit.*ARM.*statically linked" ||
    err "output is not a static 32-bit ARM ELF"
  mv "${partial}" "${output}"
  rm -rf -- "${work_dir}"
  trap - EXIT
  file "${output}"
  sha256sum "${output}"
}

main "$@"
