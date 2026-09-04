#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Build the patched static ARMv7 BlueALSA playback process.

set -euo pipefail

readonly SOURCE_SHA256="ce5e060e61669d61d44f5f9bad34a7b88378376e9d49d31482406a68127a6b29"
readonly COMPILER_SHA256="da77d2b40ffceb388d2a201877a83fb30f054d1f196e63f366572e307d7a63d6"
readonly STRIP_SHA256="fb5832708c993a6f196aac6fca7593a24c90f7b8316ede91382e5b55a88608dc"
readonly PATCH_SHA256="627e3d3e8649054a8aa73b1f716bb438b4c8ca0828f71e840625775d4c1d3317"
readonly OUTPUT_SHA256="4c9978214873589991b995b482b5503fe16b9607e6a8c8896cef251ad3b1d937"

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: build-bluealsa-aplay.sh --source-archive PATH --sysroot PATH \
  --output PATH [--jobs COUNT]

Builds the checksum-gated BlueALSA 4.0.0 player with the active-PCM lease
patch as a deterministic static ARMv7 executable.
EOF
  exit "${exit_code}"
}

err() {
  printf "ERROR: %s\n" "$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null || err "'$1' is required"
}

verify_sha256() {
  local path="$1"
  local expected="$2"

  printf "%s  %s\n" "${expected}" "${path}" |
    sha256sum --check --status ||
    err "checksum mismatch: ${path}"
}

main() {
  local source_archive=""
  local sysroot=""
  local output=""
  local jobs
  local repo_root
  local patch_path
  local compiler
  local strip_tool
  local build_root
  local source_dir
  local partial_output

  jobs="$(nproc)"
  repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
  patch_path="${repo_root}/patches/bluealsa/0001-emit-active-pcm-playback-lease.patch"

  while (( $# > 0 )); do
    case "$1" in
      --source-archive)
        [[ -n "${2:-}" ]] || err "--source-archive requires a path"
        source_archive="$2"
        shift 2
        ;;
      --sysroot)
        [[ -n "${2:-}" ]] || err "--sysroot requires a path"
        sysroot="$2"
        shift 2
        ;;
      --output)
        [[ -n "${2:-}" ]] || err "--output requires a path"
        output="$2"
        shift 2
        ;;
      --jobs)
        [[ "${2:-}" =~ ^[1-9][0-9]*$ ]] ||
          err "--jobs requires a positive integer"
        jobs="$2"
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

  [[ -f "${source_archive}" ]] ||
    err "--source-archive must name the BlueALSA 4.0.0 archive"
  [[ -d "${sysroot}/usr/include" ]] ||
    err "--sysroot must name the prepared ARM build sysroot"
  [[ -n "${output}" ]] || err "--output is required"
  [[ ! -e "${output}" ]] || err "refusing to overwrite output: ${output}"

  for command_name in arm-linux-gnueabihf-gcc arm-linux-gnueabihf-strip \
    autoreconf file grep install make mkdir mktemp mv nproc patch pkg-config \
    readelf realpath rm sha256sum tar; do
    require_command "${command_name}"
  done

  source_archive="$(realpath "${source_archive}")"
  sysroot="$(realpath "${sysroot}")"
  output="$(realpath --canonicalize-missing "${output}")"
  compiler="$(command -v arm-linux-gnueabihf-gcc)"
  strip_tool="$(command -v arm-linux-gnueabihf-strip)"
  verify_sha256 "${source_archive}" "${SOURCE_SHA256}"
  verify_sha256 "${compiler}" "${COMPILER_SHA256}"
  verify_sha256 "${strip_tool}" "${STRIP_SHA256}"
  verify_sha256 "${patch_path}" "${PATCH_SHA256}"

  build_root="$(mktemp -d)"
  partial_output="${output}.partial"
  trap 'rm -rf -- "${build_root}" "${partial_output}"' EXIT
  mkdir -p "$(dirname "${output}")"
  tar -xzf "${source_archive}" -C "${build_root}"
  source_dir="${build_root}/bluez-alsa-4.0.0"
  patch -s -d "${source_dir}" -p1 <"${patch_path}"

  (
    cd "${source_dir}"
    autoreconf -fi
    PKG_CONFIG_LIBDIR="${sysroot}/usr/lib/arm-linux-gnueabihf/pkgconfig" \
      PKG_CONFIG_SYSROOT_DIR="${sysroot}" \
      ./configure \
        --host=arm-linux-gnueabihf \
        --prefix=/usr \
        --disable-shared \
        --enable-static \
        --enable-cli \
        --disable-aac \
        --disable-aptx \
        --disable-aptx-hd \
        --disable-faststream \
        --disable-mp3lame \
        --disable-ofono \
        --disable-systemd \
        --disable-rfcomm \
        --disable-manpages \
        --disable-test \
        CC=arm-linux-gnueabihf-gcc \
        CFLAGS="-O2 -ffunction-sections -fdata-sections \
          -I${sysroot}/usr/include -ffile-prefix-map=${source_dir}=." \
        LDFLAGS="-L${sysroot}/usr/lib/arm-linux-gnueabihf \
          -Wl,--gc-sections" \
        LIBS="-lpcre -lffi -lz -lmd -lm -ldl -lpthread -lrt" \
        DBUS1_CFLAGS="-I${sysroot}/usr/include/dbus-1.0 \
          -I${sysroot}/usr/lib/arm-linux-gnueabihf/dbus-1.0/include" \
        DBUS1_LIBS="-L${sysroot}/usr/lib/arm-linux-gnueabihf -ldbus-1"
    make -C utils/aplay -j"${jobs}" bluealsa-aplay AM_LDFLAGS=-all-static
    "${strip_tool}" utils/aplay/bluealsa-aplay
    install -m 0755 utils/aplay/bluealsa-aplay "${partial_output}"
  )

  file "${partial_output}" |
    grep -q "ELF 32-bit.*ARM.*statically linked" ||
    err "output is not a static 32-bit ARM ELF"
  if readelf -l "${partial_output}" | grep -q "INTERP"; then
    err "output unexpectedly has a dynamic interpreter"
  fi
  verify_sha256 "${partial_output}" "${OUTPUT_SHA256}"
  mv "${partial_output}" "${output}"
  rm -rf -- "${build_root}"
  trap - EXIT

  file "${output}"
  sha256sum "${output}"
}

main "$@"
