#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Build the patched static ARMv7 BlueALSA daemon and playback process.

set -euo pipefail

readonly SOURCE_SHA256="ce5e060e61669d61d44f5f9bad34a7b88378376e9d49d31482406a68127a6b29"
readonly COMPILER_SHA256="da77d2b40ffceb388d2a201877a83fb30f054d1f196e63f366572e307d7a63d6"
readonly STRIP_SHA256="fb5832708c993a6f196aac6fca7593a24c90f7b8316ede91382e5b55a88608dc"
readonly PATCH_SHA256="627e3d3e8649054a8aa73b1f716bb438b4c8ca0828f71e840625775d4c1d3317"
readonly PLAYBACK_PATCH_SHA256="0746ecb1049e9552cb286eb7027dced7727a9ec59f5b2bf0c888e37596c888a0"
readonly JITTER_PATCH_SHA256="bc9e4b6ada5f615f4b29f95fef17f922a65ab8f6adac151712b82af5245389b6"
readonly RTP_PATCH_SHA256="5646103608cccee4c0676f0dfb5ec424e93bcc842151525f914b54ff7365a987"
readonly SHORT_CLIP_PATCH_SHA256="29f01a75126d113d72b82b3c67ecb8474f4ac77854eadbc98121f6f8cc3cae17"
readonly FIFO_DRAIN_PATCH_SHA256="c0e600caae492e56cf3f661eaba9a2c0172a7a5ef080093d171d9bab85350400"
readonly OUTPUT_SHA256="edc3a6cccb01bf4ac5ab8ab2898fad29e7fbafbebc1e9053aeed8b4c5f006558"
readonly DAEMON_OUTPUT_SHA256="62a3c8c465437240b9c8f1fa41bddbbde8fd98796a51527636c70a5ede605348"

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: build-bluealsa-aplay.sh --source-archive PATH --sysroot PATH \
  --output PATH [--daemon-output PATH] [--jobs COUNT]

Builds the checksum-gated BlueALSA 4.0.0 player with active-PCM lease and
jitter buffering. When --daemon-output is supplied, it also builds the SBC
decoder with bounded RTP-gap concealment.
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
  local daemon_output=""
  local jobs
  local repo_root
  local patch_path
  local playback_patch_path
  local jitter_patch_path
  local rtp_patch_path
  local short_clip_patch_path
  local fifo_drain_patch_path
  local compiler
  local strip_tool
  local build_root
  local source_dir
  local partial_output
  local daemon_partial_output
  local sysroot_lib
  local daemon_ldadd

  jobs="$(nproc)"
  repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
  patch_path="${repo_root}/patches/bluealsa/0001-emit-active-pcm-playback-lease.patch"
  playback_patch_path="${repo_root}/patches/bluealsa/0002-match-invoke-alsa-playback.patch"
  jitter_patch_path="${repo_root}/patches/bluealsa/0003-buffer-decoded-pcm-jitter.patch"
  rtp_patch_path="${repo_root}/patches/bluealsa/0004-conceal-sbc-rtp-gaps.patch"
  short_clip_patch_path="${repo_root}/patches/bluealsa/0005-drain-short-clips-after-prefill.patch"
  fifo_drain_patch_path="${repo_root}/patches/bluealsa/0006-drain-closed-pcm-fifo.patch"

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
      --daemon-output)
        [[ -n "${2:-}" ]] || err "--daemon-output requires a path"
        daemon_output="$2"
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
  if [[ -n "${daemon_output}" && -e "${daemon_output}" ]]; then
    err "refusing to overwrite daemon output: ${daemon_output}"
  fi

  for command_name in arm-linux-gnueabihf-gcc arm-linux-gnueabihf-strip \
    autoreconf file grep install make mkdir mktemp mv nproc patch pkg-config \
    readelf realpath rm sha256sum tar; do
    require_command "${command_name}"
  done

  source_archive="$(realpath "${source_archive}")"
  sysroot="$(realpath "${sysroot}")"
  output="$(realpath --canonicalize-missing "${output}")"
  if [[ -n "${daemon_output}" ]]; then
    daemon_output="$(realpath --canonicalize-missing "${daemon_output}")"
  fi
  compiler="$(command -v arm-linux-gnueabihf-gcc)"
  strip_tool="$(command -v arm-linux-gnueabihf-strip)"
  verify_sha256 "${source_archive}" "${SOURCE_SHA256}"
  verify_sha256 "${compiler}" "${COMPILER_SHA256}"
  verify_sha256 "${strip_tool}" "${STRIP_SHA256}"
  verify_sha256 "${patch_path}" "${PATCH_SHA256}"
  verify_sha256 "${playback_patch_path}" "${PLAYBACK_PATCH_SHA256}"
  verify_sha256 "${jitter_patch_path}" "${JITTER_PATCH_SHA256}"
  verify_sha256 "${rtp_patch_path}" "${RTP_PATCH_SHA256}"
  verify_sha256 "${short_clip_patch_path}" "${SHORT_CLIP_PATCH_SHA256}"
  verify_sha256 "${fifo_drain_patch_path}" "${FIFO_DRAIN_PATCH_SHA256}"

  build_root="$(mktemp -d)"
  partial_output="${output}.partial"
  if [[ -n "${daemon_output}" ]]; then
    daemon_partial_output="${daemon_output}.partial"
    trap 'rm -rf -- "${build_root}" "${partial_output}" \
      "${daemon_partial_output}"' EXIT
  else
    trap 'rm -rf -- "${build_root}" "${partial_output}"' EXIT
  fi
  mkdir -p "$(dirname "${output}")"
  if [[ -n "${daemon_output}" ]]; then
    mkdir -p "$(dirname "${daemon_output}")"
  fi
  tar -xzf "${source_archive}" -C "${build_root}"
  source_dir="${build_root}/bluez-alsa-4.0.0"
  patch -s -d "${source_dir}" -p1 <"${patch_path}"
  patch -s -d "${source_dir}" -p1 <"${playback_patch_path}"
  patch -s -d "${source_dir}" -p1 <"${jitter_patch_path}"
  patch -s -d "${source_dir}" -p1 <"${rtp_patch_path}"
  patch -s -d "${source_dir}" -p1 <"${short_clip_patch_path}"
  patch -s -d "${source_dir}" -p1 <"${fifo_drain_patch_path}"

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
    if [[ -n "${daemon_output}" ]]; then
      sysroot_lib="${sysroot}/usr/lib/arm-linux-gnueabihf"
      daemon_ldadd="-L${sysroot_lib} -lbluetooth -Wl,--start-group \
        -lgio-2.0 -lgmodule-2.0 -lmount -lblkid -lselinux -lsepol \
        -lpcre2-8 -lgobject-2.0 -lglib-2.0 -lffi -lz -lpcre \
        -Wl,--end-group -lsbc"
      make -C src -j"${jobs}" bluealsa \
        AM_LDFLAGS=-all-static LDADD="${daemon_ldadd}"
      "${strip_tool}" src/bluealsa
      install -m 0755 src/bluealsa "${daemon_partial_output}"
    fi
  )

  file "${partial_output}" |
    grep -q "ELF 32-bit.*ARM.*statically linked" ||
    err "output is not a static 32-bit ARM ELF"
  if readelf -l "${partial_output}" | grep -q "INTERP"; then
    err "output unexpectedly has a dynamic interpreter"
  fi
  verify_sha256 "${partial_output}" "${OUTPUT_SHA256}"
  if [[ -n "${daemon_output}" ]]; then
    file "${daemon_partial_output}" |
      grep -q "ELF 32-bit.*ARM.*statically linked" ||
      err "daemon output is not a static 32-bit ARM ELF"
    if readelf -l "${daemon_partial_output}" | grep -q "INTERP"; then
      err "daemon output unexpectedly has a dynamic interpreter"
    fi
    verify_sha256 "${daemon_partial_output}" "${DAEMON_OUTPUT_SHA256}"
  fi
  mv "${partial_output}" "${output}"
  if [[ -n "${daemon_output}" ]]; then
    mv "${daemon_partial_output}" "${daemon_output}"
  fi
  rm -rf -- "${build_root}"
  trap - EXIT

  file "${output}"
  sha256sum "${output}"
  if [[ -n "${daemon_output}" ]]; then
    file "${daemon_output}"
    sha256sum "${daemon_output}"
  fi
}

main "$@"
