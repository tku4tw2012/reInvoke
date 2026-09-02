#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Build the pinned open-source Invoke flasher for the current Linux host.

set -euo pipefail

readonly EXPECTED_COMMIT="63444e82cc5274abe31ec49ad55ee552b50b64b3"
readonly EXPECTED_SOURCE_SHA256="1dcee95d828026727760cb7f32699ef7ba40ccc69e258a5b961b5f42b0e1c2db"

err() {
  printf "ERROR: %s\n" "$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null || err "'$1' is required"
}

main() {
  local repo_root
  local archive_root
  local mirror_root
  local output_root
  local output_file
  local source_snapshot
  local temporary_source
  local temporary_binary
  local actual_source_sha256
  local binary_sha256
  local source_date_epoch
  local -a usb_flags

  repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
  archive_root="${REINVOKE_ARCHIVE:-${repo_root}/../reinvoke-archive}"
  mirror_root="${archive_root}/git-mirrors/community/hk-invoke-arm-flasher.git"
  output_root="${archive_root}/tools/hk-invoke-arm-flasher/${EXPECTED_COMMIT:0:8}"
  output_file="${output_root}/usb_boot_arm"
  source_snapshot="${output_root}/usb_boot_arm.c"

  require_command gcc
  require_command git
  require_command pkg-config
  require_command sha256sum

  [[ -d "${mirror_root}" ]] || err "Source mirror not found: ${mirror_root}"
  git --git-dir="${mirror_root}" cat-file -e "${EXPECTED_COMMIT}^{commit}" ||
    err "Pinned commit is absent from the source mirror"

  pkg-config --exists libusb-1.0 ||
    err "libusb headers are missing; install libusb-1.0-0-dev"
  read -r -a usb_flags <<< "$(pkg-config --cflags --libs libusb-1.0)"

  mkdir -p "${output_root}"
  temporary_source="$(mktemp "${output_root}/.usb_boot_arm.XXXXXX.c")"
  temporary_binary="$(mktemp "${output_root}/.usb_boot_arm.XXXXXX")"
  trap 'rm -f "${temporary_source:-}" "${temporary_binary:-}"' EXIT

  git --git-dir="${mirror_root}" show \
    "${EXPECTED_COMMIT}:src/usb_boot_arm.c" > "${temporary_source}"
  actual_source_sha256="$(sha256sum "${temporary_source}" | cut -d' ' -f1)"
  [[ "${actual_source_sha256}" == "${EXPECTED_SOURCE_SHA256}" ]] ||
    err "Source hash does not match the reviewed commit"
  mv "${temporary_source}" "${source_snapshot}"
  source_date_epoch="$(git --git-dir="${mirror_root}" show -s \
    --format=%ct "${EXPECTED_COMMIT}")"

  SOURCE_DATE_EPOCH="${source_date_epoch}" gcc -o "${temporary_binary}" \
    "${source_snapshot}" \
    "${usb_flags[@]}" \
    -lpthread
  chmod 0755 "${temporary_binary}"
  mv "${temporary_binary}" "${output_file}"
  trap - EXIT

  binary_sha256="$(sha256sum "${output_file}" | cut -d' ' -f1)"
  {
    printf "source_url=https://github.com/jryruegas92/hk-invoke-arm-flasher.git\n"
    printf "source_commit=%s\n" "${EXPECTED_COMMIT}"
    printf "source_sha256=%s\n" "${EXPECTED_SOURCE_SHA256}"
    printf "source_date_epoch=%s\n" "${source_date_epoch}"
    printf "compiler=%s\n" "$(gcc --version | head -n 1)"
    printf "build_flags=pkg-config libusb-1.0; -lpthread\n"
    printf "libusb=%s\n" "$(pkg-config --modversion libusb-1.0)"
    printf "binary_sha256=%s\n" "${binary_sha256}"
    printf "built_utc=%s\n" "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } > "${output_root}/build-metadata.txt"

  printf "Built: %s\n" "${output_file}"
  printf "SHA-256: %s\n" "${binary_sha256}"
}

main "$@"
