#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Build a RAM-bootable ACast kernel from the archived Invoke GPL source.

set -euo pipefail

readonly SOURCE_ARCHIVE_SHA256="bd19dff0f8ef8879b82d4cdeec9f127a105905ea0aa47e76de31192a79a79126"
readonly IMAGE_TARGET="uImage-dtb.berlin2cdp-a0-acast"

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: build-invoke-kernel.sh --output-dir PATH [options]

Options:
  --archive-root PATH  External reInvoke archive root
  --source-dir PATH    Preserved extracted Invoke kernel source
  --work-dir PATH      Disposable patched source copy
  --build-dir PATH     Out-of-tree build directory
  --output-dir PATH    New artifact output directory
  --jobs COUNT         Parallel build jobs
  --help               Show this help
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

main() {
  local repo_root
  local archive_root
  local source_archive=""
  local source_dir=""
  local work_dir=""
  local build_dir=""
  local output_dir=""
  local jobs
  local patch_file
  local patch_sha256
  local marker_file
  local image_path
  local compiler
  local linker
  local lzop_version
  local mkimage_version
  local module_count

  repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
  archive_root="${REINVOKE_ARCHIVE:-${repo_root}/../reinvoke-archive}"
  jobs="$(nproc)"
  patch_file="${repo_root}/patches/invoke-kernel/0001-modern-host-toolchain.patch"

  while (( $# > 0 )); do
    case "$1" in
      --archive-root)
        [[ -n "${2:-}" ]] || err "--archive-root requires a path"
        archive_root="$2"
        shift 2
        ;;
      --source-dir)
        [[ -n "${2:-}" ]] || err "--source-dir requires a path"
        source_dir="$2"
        shift 2
        ;;
      --work-dir)
        [[ -n "${2:-}" ]] || err "--work-dir requires a path"
        work_dir="$2"
        shift 2
        ;;
      --build-dir)
        [[ -n "${2:-}" ]] || err "--build-dir requires a path"
        build_dir="$2"
        shift 2
        ;;
      --output-dir)
        [[ -n "${2:-}" ]] || err "--output-dir requires a path"
        output_dir="$2"
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

  source_archive="${archive_root}/originals/harman/invoke/Invoke-kernel.tar"
  source_dir="${source_dir:-${archive_root}/sources/harman/invoke-kernel/Invoke-kernel}"
  work_dir="${work_dir:-${archive_root}/build/invoke-kernel-reinvoke-source}"
  build_dir="${build_dir:-${archive_root}/build/invoke-kernel-reinvoke-build}"

  [[ -n "${output_dir}" ]] || err "--output-dir is required"
  [[ ! -e "${output_dir}" ]] ||
    err "refusing to overwrite output directory: ${output_dir}"
  [[ -f "${source_archive}" ]] ||
    err "source archive not found: ${source_archive}"
  [[ -f "${source_dir}/Makefile" ]] ||
    err "extracted source not found: ${source_dir}"
  [[ -f "${patch_file}" ]] || err "compatibility patch not found"

  for command_name in \
    arm-linux-gnueabihf-gcc arm-linux-gnueabihf-ld \
    cpio lzop make mkimage patch sha256sum; do
    require_command "${command_name}"
  done

  printf "%s  %s\n" "${SOURCE_ARCHIVE_SHA256}" "${source_archive}" |
    sha256sum --check --status ||
    err "Invoke kernel source archive checksum mismatch"

  patch_sha256="$(sha256sum "${patch_file}" | cut -d " " -f 1)"
  marker_file="${work_dir}/.reinvoke-patch-sha256"
  if [[ ! -d "${work_dir}" ]]; then
    mkdir -p "$(dirname "${work_dir}")"
    cp -a --reflink=auto "${source_dir}" "${work_dir}"
    patch --directory="${work_dir}" --strip=1 < "${patch_file}"
    printf "%s\n" "${patch_sha256}" > "${marker_file}"
  else
    [[ -f "${marker_file}" ]] ||
      err "existing work directory has no reInvoke patch marker"
    [[ "$(<"${marker_file}")" == "${patch_sha256}" ]] ||
      err "existing work directory uses a different compatibility patch"
  fi

  mkdir -p "${build_dir}"
  make -C "${work_dir}" \
    O="${build_dir}" \
    ARCH=arm \
    CROSS_COMPILE=arm-linux-gnueabihf- \
    berlin2cdp_a0_amp_acast_defconfig

  "${work_dir}/scripts/config" \
    --file "${build_dir}/.config" \
    --set-str LOCALVERSION -reinvoke \
    --enable USB_GADGET \
    --set-val USB_GADGET_VBUS_DRAW 2 \
    --set-val USB_GADGET_STORAGE_NUM_BUFFERS 2 \
    --enable USB_MV_UDC \
    --enable USB_LIBCOMPOSITE \
    --enable USB_F_ACM \
    --enable USB_U_SERIAL \
    --enable USB_G_ANDROID

  make -C "${work_dir}" \
    O="${build_dir}" \
    ARCH=arm \
    CROSS_COMPILE=arm-linux-gnueabihf- \
    olddefconfig

  make -C "${work_dir}" \
    O="${build_dir}" \
    ARCH=arm \
    CROSS_COMPILE=arm-linux-gnueabihf- \
    KCFLAGS=-march=armv7-a \
    -j"${jobs}" \
    "${IMAGE_TARGET}" \
    modules

  image_path="${build_dir}/arch/arm/boot/${IMAGE_TARGET}"
  [[ -f "${image_path}" ]] || err "kernel image was not produced"

  mkdir -p "${output_dir}/modules"
  make -C "${work_dir}" \
    O="${build_dir}" \
    ARCH=arm \
    CROSS_COMPILE=arm-linux-gnueabihf- \
    INSTALL_MOD_PATH="${output_dir}/modules/" \
    modules_install

  install -m 0644 "${image_path}" "${output_dir}/81_IMAGE.reinvoke"
  install -m 0644 "${build_dir}/.config" "${output_dir}/kernel.config"
  install -m 0644 "${build_dir}/System.map" "${output_dir}/System.map"

  compiler="$(arm-linux-gnueabihf-gcc --version | sed -n '1p')"
  linker="$(arm-linux-gnueabihf-ld --version | sed -n '1p')"
  lzop_version="$(lzop --version | sed -n '1p')"
  mkimage_version="$(mkimage -V)"
  module_count="$(find "${output_dir}/modules" -type f -name "*.ko" | wc -l)"
  {
    printf "source_archive_sha256=%s\n" "${SOURCE_ARCHIVE_SHA256}"
    printf "compatibility_patch_sha256=%s\n" "${patch_sha256}"
    printf "compiler=%s\n" "${compiler}"
    printf "linker=%s\n" "${linker}"
    printf "lzop=%s\n" "${lzop_version}"
    printf "mkimage=%s\n" "${mkimage_version}"
    printf "image_target=%s\n" "${IMAGE_TARGET}"
    printf "module_count=%s\n" "${module_count}"
  } > "${output_dir}/build-manifest.txt"

  (
    cd "${output_dir}"
    find . -type f ! -name SHA256SUMS -print0 |
      LC_ALL=C sort --zero-terminated |
      xargs --null sha256sum > SHA256SUMS
  )

  printf "Built %s with %s modules\n" "${image_path}" "${module_count}"
  cat "${output_dir}/build-manifest.txt"
}

main "$@"
