#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Build the hardware-verified Invoke kernel path with Android NDK r10e GCC 4.9.

set -euo pipefail

readonly SOURCE_ARCHIVE_SHA256="bd19dff0f8ef8879b82d4cdeec9f127a105905ea0aa47e76de31192a79a79126"
readonly NDK_ARCHIVE_SHA256="ee5f405f3b57c4f5c3b3b8b5d495ae12b660e03d2112e4ed5c728d349f1e520c"
readonly COMPILER_SHA256="a838490fd49184f1f104027239f0a46671c743c29c17a33f6d5daad3c2a379a6"
readonly LOAD_ADDRESS="0x02008000"

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: build-native-kernel.sh --profile PROFILE --dtb PATH \
  --dtb-sha256 SHA256 --output-dir PATH [options]

Profiles:
  baseline   Known-good USB, SDIO, I2C, and GPIO kernel configuration
  spi-gpio   Baseline plus DesignWare SPI and spidev
  audio      SPI/GPIO plus Berlin ASoC, WM8904, and ALSA loopback

Options:
  --archive-root PATH  External reInvoke archive root
  --source-dir PATH    Preserved extracted Invoke kernel source
  --ndk-dir PATH       Extracted Android NDK r10e host directory
  --build-dir PATH     Out-of-tree kernel build directory
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
  local source_archive
  local ndk_archive
  local source_dir=""
  local ndk_dir=""
  local build_dir=""
  local output_dir=""
  local partial_output
  local profile=""
  local dtb_path=""
  local dtb_sha256=""
  local actual_dtb_sha256
  local localversion
  local image_suffix
  local image_name
  local kernel_release
  local jobs
  local tool_bin
  local cross_prefix
  local compiler
  local linker
  local module_count
  local mkimage_version
  local bt_module_dir="arch/arm/mach-berlin/modules/bt_sd8887"

  repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
  archive_root="${REINVOKE_ARCHIVE:-${repo_root}/../reinvoke-archive}"
  jobs="$(nproc)"

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
      --ndk-dir)
        [[ -n "${2:-}" ]] || err "--ndk-dir requires a path"
        ndk_dir="$2"
        shift 2
        ;;
      --build-dir)
        [[ -n "${2:-}" ]] || err "--build-dir requires a path"
        build_dir="$2"
        shift 2
        ;;
      --profile)
        [[ -n "${2:-}" ]] || err "--profile requires a value"
        profile="$2"
        shift 2
        ;;
      --dtb)
        [[ -n "${2:-}" ]] || err "--dtb requires a path"
        dtb_path="$2"
        shift 2
        ;;
      --dtb-sha256)
        [[ "${2:-}" =~ ^[0-9a-fA-F]{64}$ ]] ||
          err "--dtb-sha256 requires 64 hexadecimal characters"
        dtb_sha256="${2,,}"
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

  case "${profile}" in
    baseline)
      localversion="-reinvoke"
      image_suffix="baseline"
      ;;
    spi-gpio)
      localversion="-reinvoke-spi"
      image_suffix="spi-gpio"
      ;;
    audio)
      localversion="-reinvoke-audio"
      image_suffix="audio"
      ;;
    *)
      err "--profile must be baseline, spi-gpio, or audio"
      ;;
  esac

  archive_root="$(realpath "${archive_root}")"
  source_archive="${archive_root}/originals/harman/invoke/Invoke-kernel.tar"
  ndk_archive="${archive_root}/toolchains/android-ndk-r10e/android-ndk-r10e-linux-x86_64.zip"
  source_dir="${source_dir:-${archive_root}/sources/harman/invoke-kernel/Invoke-kernel}"
  ndk_dir="${ndk_dir:-${archive_root}/toolchains/android-ndk-r10e/extracted/android-ndk-r10e/toolchains/arm-linux-androideabi-4.9/prebuilt/linux-x86_64}"
  build_dir="${build_dir:-${archive_root}/build/invoke-kernel-gcc49-${profile}-build}"
  partial_output="${output_dir}.partial"

  [[ -n "${output_dir}" ]] || err "--output-dir is required"
  [[ -n "${dtb_path}" ]] || err "--dtb is required"
  [[ -n "${dtb_sha256}" ]] || err "--dtb-sha256 is required"
  [[ -f "${source_archive}" ]] ||
    err "source archive not found: ${source_archive}"
  [[ -f "${ndk_archive}" ]] || err "NDK archive not found: ${ndk_archive}"
  [[ -f "${source_dir}/Makefile" ]] ||
    err "extracted source not found: ${source_dir}"
  [[ -f "${dtb_path}" ]] || err "device tree not found: ${dtb_path}"
  [[ ! -e "${output_dir}" ]] ||
    err "refusing to overwrite output directory: ${output_dir}"
  [[ ! -e "${partial_output}" ]] ||
    err "stale partial output exists: ${partial_output}"

  for command_name in find make mkimage realpath sha256sum; do
    require_command "${command_name}"
  done

  tool_bin="${ndk_dir}/bin"
  cross_prefix="${tool_bin}/arm-linux-androideabi-"
  compiler="${cross_prefix}gcc"
  linker="${cross_prefix}ld.bfd"
  [[ -x "${compiler}" ]] || err "NDK compiler not found: ${compiler}"
  [[ -x "${linker}" ]] || err "NDK BFD linker not found: ${linker}"

  printf "%s  %s\n" "${SOURCE_ARCHIVE_SHA256}" "${source_archive}" |
    sha256sum --check --status ||
    err "Invoke kernel source archive checksum mismatch"
  printf "%s  %s\n" "${NDK_ARCHIVE_SHA256}" "${ndk_archive}" |
    sha256sum --check --status ||
    err "Android NDK r10e archive checksum mismatch"
  printf "%s  %s\n" "${COMPILER_SHA256}" "${compiler}" |
    sha256sum --check --status ||
    err "Android NDK r10e compiler checksum mismatch"

  actual_dtb_sha256="$(sha256sum "${dtb_path}" | cut -d " " -f 1)"
  [[ "${actual_dtb_sha256}" == "${dtb_sha256}" ]] ||
    err "device-tree checksum mismatch"

  mkdir -p "${build_dir}"
  make -C "${source_dir}" \
    O="${build_dir}" \
    ARCH=arm \
    CROSS_COMPILE="${cross_prefix}" \
    LD="${linker}" \
    HOSTCFLAGS=-fcommon \
    berlin2cdp_amp_defconfig

  "${source_dir}/scripts/config" \
    --file "${build_dir}/.config" \
    --set-str LOCALVERSION "${localversion}" \
    --disable LOCALVERSION_AUTO \
    --enable USB_GADGET \
    --set-val USB_GADGET_VBUS_DRAW 2 \
    --set-val USB_GADGET_STORAGE_NUM_BUFFERS 2 \
    --enable USB_MV_UDC \
    --enable USB_LIBCOMPOSITE \
    --enable USB_F_ACM \
    --enable USB_U_SERIAL \
    --enable USB_G_ANDROID \
    --disable BERLIN_GPU \
    --disable BERLIN_GPU3D

  case "${profile}" in
    baseline)
      "${source_dir}/scripts/config" \
        --file "${build_dir}/.config" \
        --disable SPI \
        --disable SOUND
      ;;
    spi-gpio)
      "${source_dir}/scripts/config" \
        --file "${build_dir}/.config" \
        --enable SPI \
        --enable SPI_DESIGNWARE \
        --enable SPI_DW_MMIO \
        --enable SPI_SPIDEV \
        --disable SOUND
      ;;
    audio)
      "${source_dir}/scripts/config" \
        --file "${build_dir}/.config" \
        --disable BERLIN_FASTLOGO \
        --enable SPI \
        --enable SPI_DESIGNWARE \
        --enable SPI_DW_MMIO \
        --enable SPI_SPIDEV \
        --enable SOUND \
        --enable SND \
        --enable SND_TIMER \
        --enable SND_PCM \
        --enable SND_HWDEP \
        --enable SND_COMPRESS_OFFLOAD \
        --enable SND_JACK \
        --enable SND_DRIVERS \
        --enable SND_ALOOP \
        --enable SND_ARM \
        --enable SND_SOC \
        --enable SND_SOC_BERLIN \
        --enable SND_SOC_I2C_AND_SPI \
        --enable SND_SOC_WM8904
      ;;
  esac

  make -C "${source_dir}" \
    O="${build_dir}" \
    ARCH=arm \
    CROSS_COMPILE="${cross_prefix}" \
    LD="${linker}" \
    HOSTCFLAGS=-fcommon \
    olddefconfig

  make -C "${source_dir}" \
    O="${build_dir}" \
    ARCH=arm \
    CROSS_COMPILE="${cross_prefix}" \
    LD="${linker}" \
    HOSTCFLAGS=-fcommon \
    -j"${jobs}" \
    zImage

  make -C "${source_dir}" \
    O="${build_dir}" \
    ARCH=arm \
    CROSS_COMPILE="${cross_prefix}" \
    LD="${linker}" \
    HOSTCFLAGS=-fcommon \
    KCFLAGS="-fno-pic -fno-pie" \
    -j"${jobs}" \
    modules

  # The vendor Bluetooth directory is selected by BERLIN_SDIO_BT_8887, but its
  # local Makefile mistakenly keys bt8xxx.o on the WLAN-8887 symbol.
  make -C "${source_dir}" \
    O="${build_dir}" \
    ARCH=arm \
    CROSS_COMPILE="${cross_prefix}" \
    LD="${linker}" \
    HOSTCFLAGS=-fcommon \
    KCFLAGS="-fno-pic -fno-pie" \
    CONFIG_BERLIN_SDIO_WLAN_8887=m \
    M="${bt_module_dir}" \
    -j"${jobs}" \
    modules

  [[ -f "${build_dir}/arch/arm/boot/zImage" ]] ||
    err "kernel zImage was not produced"
  kernel_release="$(make -s -C "${source_dir}" \
    O="${build_dir}" \
    ARCH=arm \
    CROSS_COMPILE="${cross_prefix}" \
    LD="${linker}" \
    kernelrelease)"

  mkdir -p "${partial_output}/modules"
  make -C "${source_dir}" \
    O="${build_dir}" \
    ARCH=arm \
    CROSS_COMPILE="${cross_prefix}" \
    LD="${linker}" \
    INSTALL_MOD_PATH="${partial_output}/modules" \
    modules_install >"${partial_output}/modules-install.log" 2>&1
  make -C "${source_dir}" \
    O="${build_dir}" \
    ARCH=arm \
    CROSS_COMPILE="${cross_prefix}" \
    LD="${linker}" \
    HOSTCFLAGS=-fcommon \
    CONFIG_BERLIN_SDIO_WLAN_8887=m \
    M="${bt_module_dir}" \
    INSTALL_MOD_PATH="${partial_output}/modules" \
    modules_install >>"${partial_output}/modules-install.log" 2>&1

  install -m 0644 "${build_dir}/arch/arm/boot/zImage" \
    "${partial_output}/zImage"
  install -m 0644 "${dtb_path}" \
    "${partial_output}/reinvoke-${image_suffix}.dtb"
  cat "${partial_output}/zImage" \
    "${partial_output}/reinvoke-${image_suffix}.dtb" \
    >"${partial_output}/zImage-dtb.${image_suffix}"

  image_name="Linux-${kernel_release}"
  mkimage \
    -A arm \
    -O linux \
    -T kernel \
    -C none \
    -a "${LOAD_ADDRESS}" \
    -e "${LOAD_ADDRESS}" \
    -n "${image_name}" \
    -d "${partial_output}/zImage-dtb.${image_suffix}" \
    "${partial_output}/81_IMAGE.reinvoke-${image_suffix}" >/dev/null

  install -m 0644 "${build_dir}/.config" \
    "${partial_output}/kernel.config"
  install -m 0644 "${build_dir}/System.map" \
    "${partial_output}/System.map"

  module_count="$(find "${partial_output}/modules" -type f -name "*.ko" |
    wc -l)"
  mkimage_version="$(mkimage -V)"
  {
    printf "purpose=native RAM kernel profile %s\n" "${profile}"
    printf "source_archive_sha256=%s\n" "${SOURCE_ARCHIVE_SHA256}"
    printf "ndk_archive_sha256=%s\n" "${NDK_ARCHIVE_SHA256}"
    printf "device_tree_sha256=%s\n" "${actual_dtb_sha256}"
    printf "kernel_release=%s\n" "${kernel_release}"
    printf "kernel_load_address=%s\n" "${LOAD_ADDRESS}"
    printf "compiler=%s\n" "$("${compiler}" --version | sed -n '1p')"
    printf "compiler_sha256=%s\n" "${COMPILER_SHA256}"
    printf "linker=%s\n" "$("${linker}" --version | sed -n '1p')"
    printf "mkimage=%s\n" "${mkimage_version}"
    printf "module_count=%s\n" "${module_count}"
  } >"${partial_output}/build-manifest.txt"

  (
    cd "${partial_output}"
    find . -type f ! -name SHA256SUMS -print0 |
      LC_ALL=C sort --zero-terminated |
      xargs --null sha256sum >SHA256SUMS
  )

  mv "${partial_output}" "${output_dir}"
  printf "Built %s with %s modules\n" \
    "${output_dir}/81_IMAGE.reinvoke-${image_suffix}" "${module_count}"
  cat "${output_dir}/build-manifest.txt"
}

main "$@"
