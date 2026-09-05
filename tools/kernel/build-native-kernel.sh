#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Build the hardware-verified Invoke kernel path with Android NDK r10e GCC 4.9.

set -euo pipefail

readonly SOURCE_ARCHIVE_SHA256="bd19dff0f8ef8879b82d4cdeec9f127a105905ea0aa47e76de31192a79a79126"
readonly NDK_ARCHIVE_SHA256="ee5f405f3b57c4f5c3b3b8b5d495ae12b660e03d2112e4ed5c728d349f1e520c"
readonly COMPILER_SHA256="a838490fd49184f1f104027239f0a46671c743c29c17a33f6d5daad3c2a379a6"
readonly SPI_SOURCE_SHA256="684795ce44de9d10133260c3195dfb42b454478bba7e5406decabda3f4edbe9f"
readonly SPI_PATCHED_SOURCE_SHA256="51a8b5b88df83a7396b7d4d9de891be30efd1c6690fca1d638d5496dd44a7e31"
readonly SPI_PATCH_SHA256="285ef9751879254eab8d36e28fd9fbfadd9c203cee9717b01cfddd169de61fef"
readonly YAFFS_SOURCE_SHA256="a8862b2bae267204045d30464b8e98b9cb9d5707ee6cb777cc6dc69401c96914"
readonly YAFFS_PATCHED_SOURCE_SHA256="c40dcedece786648b2f3c3573ef506fda8b286e6b83f01ac627532b4690c0d35"
readonly YAFFS_PATCH_SHA256="e520068b84dd8ca3e6592444e1a78f284fcabf5474bae9a4aa718a4ba38d2dd5"
readonly LZO_SOURCE_SHA256="ab1933ac33d984fe0565b7053502c2b6499260b1fe7f6b37f6883a134c87dffa"
readonly LZO_PATCHED_SOURCE_SHA256="e0a247828ed4f283043c5ee9f74b3938354f08f4a778c89be873cb11a2f28013"
readonly LZO_PATCH_SHA256="609b241a5317906a8ae4990e0ef6a8e7641f3817fd8a31416e6384ac213df0d7"
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
  audio-sd8887
             Audio profile with native SD8887 STA/uAP modules

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
  local spi_patch
  local spi_source
  local spi_source_sha256
  local yaffs_patch
  local lzo_patch
  local lzo_source
  local lzo_source_sha256
  local yaffs_source
  local yaffs_source_sha256
  local module_count
  local mkimage_version
  local bt_module_dir="arch/arm/mach-berlin/modules/bt_sd8887"
  local bt_module_built_separately=0

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
    audio-sd8887)
      localversion="-reinvoke-audio-sd8887"
      image_suffix="audio-sd8887"
      ;;
    *)
      err "--profile must be baseline, spi-gpio, audio, or audio-sd8887"
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

  for command_name in find make mkimage patch realpath sha256sum; do
    require_command "${command_name}"
  done

  tool_bin="${ndk_dir}/bin"
  cross_prefix="${tool_bin}/arm-linux-androideabi-"
  compiler="${cross_prefix}gcc"
  linker="${cross_prefix}ld.bfd"
  [[ -x "${compiler}" ]] || err "NDK compiler not found: ${compiler}"
  [[ -x "${linker}" ]] || err "NDK BFD linker not found: ${linker}"
  export KBUILD_BUILD_TIMESTAMP="Thu Jan  1 00:00:00 UTC 1970"
  export KBUILD_BUILD_USER="reinvoke"
  export KBUILD_BUILD_HOST="reinvoke"
  export SOURCE_DATE_EPOCH=0

  printf "%s  %s\n" "${SOURCE_ARCHIVE_SHA256}" "${source_archive}" |
    sha256sum --check --status ||
    err "Invoke kernel source archive checksum mismatch"
  printf "%s  %s\n" "${NDK_ARCHIVE_SHA256}" "${ndk_archive}" |
    sha256sum --check --status ||
    err "Android NDK r10e archive checksum mismatch"
  printf "%s  %s\n" "${COMPILER_SHA256}" "${compiler}" |
    sha256sum --check --status ||
    err "Android NDK r10e compiler checksum mismatch"

  spi_patch="${repo_root}/patches/invoke-kernel/0002-bound-spi-gpio-ready-wait.patch"
  spi_source="${source_dir}/drivers/spi/spi-dw.c"
  [[ -f "${spi_patch}" ]] || err "SPI timeout patch not found: ${spi_patch}"
  [[ -f "${spi_source}" ]] || err "DesignWare SPI source not found"
  printf "%s  %s\n" "${SPI_PATCH_SHA256}" "${spi_patch}" |
    sha256sum --check --status ||
    err "SPI timeout patch checksum mismatch"
  spi_source_sha256="$(sha256sum "${spi_source}" | cut -d " " -f 1)"
  case "${spi_source_sha256}" in
    "${SPI_SOURCE_SHA256}")
      patch --batch --forward --directory="${source_dir}" --strip=1 \
        < "${spi_patch}"
      ;;
    "${SPI_PATCHED_SOURCE_SHA256}")
      ;;
    *)
      err "DesignWare SPI source has unexpected modifications"
      ;;
  esac
  printf "%s  %s\n" "${SPI_PATCHED_SOURCE_SHA256}" "${spi_source}" |
    sha256sum --check --status ||
    err "failed to apply the fail-fast SPI GPIO check"

  yaffs_patch="${repo_root}/patches/invoke-kernel/0003-reproducible-yaffs-build-id.patch"
  yaffs_source="${source_dir}/fs/yaffs2/yaffs_vfs.c"
  [[ -f "${yaffs_patch}" ]] ||
    err "YAFFS reproducibility patch not found: ${yaffs_patch}"
  [[ -f "${yaffs_source}" ]] || err "YAFFS source not found"
  printf "%s  %s\n" "${YAFFS_PATCH_SHA256}" "${yaffs_patch}" |
    sha256sum --check --status ||
    err "YAFFS reproducibility patch checksum mismatch"
  yaffs_source_sha256="$(sha256sum "${yaffs_source}" | cut -d " " -f 1)"
  case "${yaffs_source_sha256}" in
    "${YAFFS_SOURCE_SHA256}")
      patch --batch --forward --directory="${source_dir}" --strip=1 \
        < "${yaffs_patch}"
      ;;
    "${YAFFS_PATCHED_SOURCE_SHA256}")
      ;;
    *)
      err "YAFFS source has unexpected modifications"
      ;;
  esac
  printf "%s  %s\n" "${YAFFS_PATCHED_SOURCE_SHA256}" "${yaffs_source}" |
    sha256sum --check --status ||
    err "failed to apply the YAFFS reproducibility patch"

  lzo_patch="${repo_root}/patches/invoke-kernel/0004-reproducible-lzo-piggy.patch"
  lzo_source="${source_dir}/scripts/Makefile.lib"
  [[ -f "${lzo_patch}" ]] ||
    err "LZO reproducibility patch not found: ${lzo_patch}"
  [[ -f "${lzo_source}" ]] || err "LZO build rule source not found"
  printf "%s  %s\n" "${LZO_PATCH_SHA256}" "${lzo_patch}" |
    sha256sum --check --status ||
    err "LZO reproducibility patch checksum mismatch"
  lzo_source_sha256="$(sha256sum "${lzo_source}" | cut -d " " -f 1)"
  case "${lzo_source_sha256}" in
    "${LZO_SOURCE_SHA256}")
      patch --batch --forward --directory="${source_dir}" --strip=1 \
        < "${lzo_patch}"
      ;;
    "${LZO_PATCHED_SOURCE_SHA256}")
      ;;
    *)
      err "LZO build rule has unexpected modifications"
      ;;
  esac
  printf "%s  %s\n" "${LZO_PATCHED_SOURCE_SHA256}" "${lzo_source}" |
    sha256sum --check --status ||
    err "failed to apply the LZO reproducibility patch"

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
    audio|audio-sd8887)
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

  if [[ "${profile}" == "audio-sd8887" ]]; then
    "${source_dir}/scripts/config" \
      --file "${build_dir}/.config" \
      --disable BERLIN_SDIO_WLAN_8801 \
      --module BERLIN_SDIO_WLAN_8887 \
      --module BERLIN_SDIO_BT_8887
  fi

  make -C "${source_dir}" \
    O="${build_dir}" \
    ARCH=arm \
    CROSS_COMPILE="${cross_prefix}" \
    LD="${linker}" \
    HOSTCFLAGS=-fcommon \
    olddefconfig

  rm -f "${build_dir}/.version" \
    "${build_dir}/include/generated/compile.h"

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

  if [[ "${profile}" != "audio-sd8887" ]]; then
    # The vendor Bluetooth directory is selected by BERLIN_SDIO_BT_8887, but
    # its local Makefile mistakenly keys bt8xxx.o on the WLAN-8887 symbol.
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
    bt_module_built_separately=1
  fi

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
  if ((bt_module_built_separately == 1)); then
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
  fi

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
    printf "spi_timeout_patch_sha256=%s\n" "${SPI_PATCH_SHA256}"
    printf "yaffs_reproducibility_patch_sha256=%s\n" "${YAFFS_PATCH_SHA256}"
    printf "lzo_reproducibility_patch_sha256=%s\n" "${LZO_PATCH_SHA256}"
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
