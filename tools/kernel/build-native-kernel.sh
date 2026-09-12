#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Build the hardware-verified Invoke kernel path with Android NDK r10e GCC 4.9.

set -euo pipefail

readonly SOURCE_ARCHIVE_SHA256="bd19dff0f8ef8879b82d4cdeec9f127a105905ea0aa47e76de31192a79a79126"
readonly NDK_ARCHIVE_SHA256="ee5f405f3b57c4f5c3b3b8b5d495ae12b660e03d2112e4ed5c728d349f1e520c"
readonly COMPILER_SHA256="a838490fd49184f1f104027239f0a46671c743c29c17a33f6d5daad3c2a379a6"
readonly LINKER_SHA256="a46bcacc5b9a240452305a16d10642f25e9edbed6be5912adfd1aede5d256f25"
readonly LZOP_SHA256="fbcad458eee62c728e8b5695c82805ef5c8640706b45169d509239b9fe0d1a86"
readonly MKIMAGE_SHA256="b77cea9537d5432123de6ca42cf88f07b259f815cd16266d9883b57ed27f057e"
# Host build tools. The kernel drives its own host programs through make and
# HOSTCC, so these transform build inputs just as much as the cross compiler.
readonly MAKE_SHA256="92f646030615cd98490a68a94c0aefd87b552be3158b941c02e43b0bfdb576db"
readonly HOSTCC_SHA256="821af3c74506283c179ca413bb33e6b528805a4dd8a5c09df125e5ad560a9e89"
readonly HOSTCC1_SHA256="31c2233432d9105001eea158b799f7d403dc5a1944c712283dba251f3ab8eb43"
readonly HOST_AS_SHA256="4e6b50c3faaa834150db32be778fd7d9440a4e1f5fa8beb8a72277b12159d689"
readonly HOST_LD_SHA256="58937fc20c21e147883b4fdaa0fc7438a8e8f2bb886cfcaa4896100ca91139e7"
readonly SOURCE_TREE_MANIFEST_SHA256="6ae65ab02757536de83e489b4db967bd39e0969d40ae5bcce7fb478cadd1b42f"
readonly SPI_SOURCE_SHA256="684795ce44de9d10133260c3195dfb42b454478bba7e5406decabda3f4edbe9f"
readonly SPI_PATCHED_SOURCE_SHA256="e02935b6f6d5c715a856d735f7274b3aab1214749686668db75059e659e108e7"
readonly SPI_PATCH_SHA256="a92b98acb2272575c0497770172d79b104a1377d0083d67943b62681eecb738d"
readonly YAFFS_SOURCE_SHA256="a8862b2bae267204045d30464b8e98b9cb9d5707ee6cb777cc6dc69401c96914"
readonly YAFFS_PATCHED_SOURCE_SHA256="c40dcedece786648b2f3c3573ef506fda8b286e6b83f01ac627532b4690c0d35"
readonly YAFFS_PATCH_SHA256="e520068b84dd8ca3e6592444e1a78f284fcabf5474bae9a4aa718a4ba38d2dd5"
readonly LZO_SOURCE_SHA256="ab1933ac33d984fe0565b7053502c2b6499260b1fe7f6b37f6883a134c87dffa"
readonly LZO_PATCHED_SOURCE_SHA256="e0a247828ed4f283043c5ee9f74b3938354f08f4a778c89be873cb11a2f28013"
readonly LZO_PATCH_SHA256="609b241a5317906a8ae4990e0ef6a8e7641f3817fd8a31416e6384ac213df0d7"
readonly MTD_CLEANUP_SOURCE_SHA256="64f08a3a4c45f3443b7dda4223abe41788b7ea28d9c7c35c4a820f0ff51d9b66"
readonly MTD_CLEANUP_PATCHED_SOURCE_SHA256="3380c5891573e861f20eb8ccc8e177e5ef8c8aeb4cbcd83b79b34beda397fce1"
readonly MTD_CLEANUP_PATCH_SHA256="60f2adea13f59c734479d9be002c6aa632e890e20baebba247a7e1df0c7230c7"
readonly MTD_CLEANUP_TREE_MANIFEST_SHA256="73d4b151ed3a6d17063e1830a4e505fde66b19b85b5e3c7ab9cb6ae8628bd6ad"
readonly MTD_CLEANUP_BUILD_VERSION="1-mtd-cleanup"
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
  --mtd-cleanup-fix    Opt in to the mtdblock removal lifetime fix
  --source-work-dir PATH
                       Required fresh source copy for --mtd-cleanup-fix
  --ndk-dir PATH       Extracted Android NDK r10e host directory
  --build-dir PATH     Out-of-tree kernel build directory
  --jobs COUNT         Parallel build jobs
  --help               Show this help
EOF
  exit "${exit_code}"
}

# Host tools are reached through symlinks and wrapper names, so resolve them
# before hashing.
verify_host_tool() {
  local expected="$1"
  local path="$2"
  local label="$3"
  local resolved

  [[ -n "${path}" ]] || err "${label} not found"
  resolved="$(readlink -f "${path}")"
  printf "%s  %s\n" "${expected}" "${resolved}" |
    sha256sum --check --status ||
    err "${label} checksum mismatch: ${resolved}"
}

err() {
  printf "ERROR: %s\n" "$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null || err "'$1' is required"
}

tree_manifest_sha256() {
  local root="$1"

  (
    cd "${root}"
    find . -type f -print0 |
      sort -z |
      xargs -0 sha256sum
  ) |
    sha256sum |
    cut -d " " -f 1
}

main() {
  local repo_root
  local archive_root
  local source_archive
  local ndk_archive
  local source_dir=""
  local source_work_dir=""
  local mtd_cleanup_fix=0
  local mtd_cleanup_patch
  local mtd_cleanup_source
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
  local kernel_uts_version
  local kernel_proc_version
  local jobs
  local tool_bin
  local cross_prefix
  local compiler
  local linker
  local actual_source_manifest
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
  local lzop_version
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
      --mtd-cleanup-fix)
        mtd_cleanup_fix=1
        shift
        ;;
      --source-work-dir)
        [[ -n "${2:-}" ]] || err "--source-work-dir requires a path"
        source_work_dir="$2"
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

  if ((mtd_cleanup_fix == 1)); then
    [[ -n "${source_work_dir}" ]] ||
      err "--mtd-cleanup-fix requires a fresh --source-work-dir"
    # Preserve LOCALVERSION and module ABI, but never reuse baseline build paths.
    image_suffix="${image_suffix}-mtd-cleanup"
  elif [[ -n "${source_work_dir}" ]]; then
    err "--source-work-dir requires --mtd-cleanup-fix"
  fi

  archive_root="$(realpath "${archive_root}")"
  source_archive="${archive_root}/originals/harman/invoke/Invoke-kernel.tar"
  ndk_archive="${archive_root}/toolchains/android-ndk-r10e/android-ndk-r10e-linux-x86_64.zip"
  source_dir="${source_dir:-${archive_root}/sources/harman/invoke-kernel/Invoke-kernel}"
  ndk_dir="${ndk_dir:-${archive_root}/toolchains/android-ndk-r10e/extracted/android-ndk-r10e/toolchains/arm-linux-androideabi-4.9/prebuilt/linux-x86_64}"
  build_dir="${build_dir:-${archive_root}/build/invoke-kernel-gcc49-${image_suffix}-build}"
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

  if ((mtd_cleanup_fix == 1)); then
    source_dir="$(realpath "${source_dir}")"
    source_work_dir="$(realpath -m "${source_work_dir}")"
    build_dir="$(realpath -m "${build_dir}")"
    output_dir="$(realpath -m "${output_dir}")"
    partial_output="${output_dir}.partial"
    local destination other
    for destination in "${source_work_dir}" "${build_dir}" \
      "${output_dir}" "${partial_output}"; do
      [[ ! -e "${destination}" && ! -L "${destination}" ]] ||
        err "fixed variant requires fresh paths: ${destination}"
      for other in "${source_dir}" "${source_work_dir}" "${build_dir}" \
        "${output_dir}" "${partial_output}"; do
        if [[ "${destination}" == "${other}/"* ||
              "${other}" == "${destination}/"* ]]; then
          err "fixed variant paths must not overlap: ${destination}, ${other}"
        fi
      done
    done
    [[ "${source_work_dir}" != "${build_dir}" &&
       "${source_work_dir}" != "${output_dir}" &&
       "${source_work_dir}" != "${partial_output}" &&
       "${build_dir}" != "${output_dir}" &&
       "${build_dir}" != "${partial_output}" ]] ||
      err "fixed variant source, build, and output paths must be distinct"
  fi

  for command_name in \
    cut find lzop make mkimage patch realpath sha256sum sort xargs; do
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
  if ((mtd_cleanup_fix == 1)); then
    # Distinguish the fixed kernel in /proc/version without changing module ABI.
    export KBUILD_BUILD_VERSION="${MTD_CLEANUP_BUILD_VERSION}"
    require_command strings
  fi

  printf "%s  %s\n" "${SOURCE_ARCHIVE_SHA256}" "${source_archive}" |
    sha256sum --check --status ||
    err "Invoke kernel source archive checksum mismatch"
  printf "%s  %s\n" "${NDK_ARCHIVE_SHA256}" "${ndk_archive}" |
    sha256sum --check --status ||
    err "Android NDK r10e archive checksum mismatch"
  printf "%s  %s\n" "${COMPILER_SHA256}" "${compiler}" |
    sha256sum --check --status ||
    err "Android NDK r10e compiler checksum mismatch"
  printf "%s  %s\n" "${LINKER_SHA256}" "${linker}" |
    sha256sum --check --status ||
    err "Android NDK r10e linker checksum mismatch"
  printf "%s  %s\n" "${LZOP_SHA256}" "$(command -v lzop)" |
    sha256sum --check --status ||
    err "lzop checksum mismatch"
  printf "%s  %s\n" "${MKIMAGE_SHA256}" "$(command -v mkimage)" |
    sha256sum --check --status ||
    err "mkimage checksum mismatch"
  verify_host_tool "${MAKE_SHA256}" "$(command -v make)" "make"
  verify_host_tool "${HOSTCC_SHA256}" "$(command -v gcc)" "HOSTCC driver"
  verify_host_tool "${HOSTCC1_SHA256}" "$(gcc -print-prog-name=cc1)" "HOSTCC cc1"
  verify_host_tool "${HOST_AS_SHA256}" "$(command -v as)" "host assembler"
  verify_host_tool "${HOST_LD_SHA256}" "$(command -v ld)" "host linker"

  if ((mtd_cleanup_fix == 1)); then
    mtd_cleanup_patch="${repo_root}/patches/invoke-kernel/0005-fix-mtdblock-removal-lifetime.patch"
    printf "%s  %s\n" "${MTD_CLEANUP_PATCH_SHA256}" "${mtd_cleanup_patch}" |
      sha256sum --check --status ||
      err "MTD cleanup patch checksum mismatch"
    mkdir -p "$(dirname "${source_work_dir}")"
    mkdir "${source_work_dir}"
    cp -a "${source_dir}/." "${source_work_dir}/"
    source_dir="${source_work_dir}"
  fi

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

  actual_source_manifest="$(tree_manifest_sha256 "${source_dir}")"
  [[ "${actual_source_manifest}" == "${SOURCE_TREE_MANIFEST_SHA256}" ]] ||
    err "kernel source-tree manifest mismatch"

  if ((mtd_cleanup_fix == 1)); then
    mtd_cleanup_source="${source_dir}/drivers/mtd/mtdblock_ro.c"
    printf "%s  %s\n" "${MTD_CLEANUP_SOURCE_SHA256}" "${mtd_cleanup_source}" |
      sha256sum --check --status ||
      err "MTD cleanup source checksum mismatch"
    patch --batch --forward --fuzz=0 --directory="${source_dir}" --strip=1 \
      < "${mtd_cleanup_patch}"
    printf "%s  %s\n" "${MTD_CLEANUP_PATCHED_SOURCE_SHA256}" "${mtd_cleanup_source}" |
      sha256sum --check --status ||
      err "failed to apply the MTD cleanup fix"
    actual_source_manifest="$(tree_manifest_sha256 "${source_dir}")"
    [[ "${actual_source_manifest}" == "${MTD_CLEANUP_TREE_MANIFEST_SHA256}" ]] ||
      err "MTD cleanup source-tree manifest mismatch"
  fi

  actual_dtb_sha256="$(sha256sum "${dtb_path}" | cut -d " " -f 1)"
  [[ "${actual_dtb_sha256}" == "${dtb_sha256}" ]] ||
    err "device-tree checksum mismatch"

  # An incremental kernel rebuild can silently produce a different image than a
  # clean one. A stale shared build directory once yielded 150275c6... where a
  # clean tree reproduced the gated d29a0075..., so always start from scratch.
  if [[ -e "${build_dir}" ]]; then
    ((mtd_cleanup_fix == 0)) ||
      err "fixed variant refuses to clean an existing build directory: ${build_dir}"
    printf "Removing existing kernel build directory: %s\n" "${build_dir}"
    rm -rf -- "${build_dir}"
  fi
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
  lzop_version="$(lzop --version | sed -n '1p')"
  mkimage_version="$(mkimage -V)"
  if ((mtd_cleanup_fix == 1)); then
    install -m 0644 "${build_dir}/include/generated/compile.h" \
      "${partial_output}/kernel-compile.h"
    kernel_uts_version="$(sed -n 's/^#define UTS_VERSION "\(.*\)"$/\1/p' \
      "${partial_output}/kernel-compile.h")"
    [[ "${kernel_uts_version}" == "#${MTD_CLEANUP_BUILD_VERSION} "* ]] ||
      err "compiled kernel is missing the MTD cleanup build identifier"
    kernel_proc_version="$(strings "${build_dir}/vmlinux" |
      grep -F "Linux version ${kernel_release} (")"
    [[ "${kernel_proc_version}" == *" ${kernel_uts_version}" ]] ||
      err "compiled kernel banner does not match UTS_VERSION"
  fi
  {
    printf "purpose=native RAM kernel profile %s\n" "${profile}"
    printf "source_archive_sha256=%s\n" "${SOURCE_ARCHIVE_SHA256}"
    printf "source_tree_manifest_sha256=%s\n" \
      "${actual_source_manifest}"
    if ((mtd_cleanup_fix == 1)); then
      printf "variant=mtd-cleanup\n"
      printf "source_directory=%s\n" "${source_dir}"
      printf "kbuild_build_version=%s\n" "${MTD_CLEANUP_BUILD_VERSION}"
      printf "kernel_uts_version=%s\n" "${kernel_uts_version}"
      printf "expected_proc_version=%s\n" "${kernel_proc_version}"
      printf "baseline_source_tree_manifest_sha256=%s\n" "${SOURCE_TREE_MANIFEST_SHA256}"
      printf "mtd_cleanup_patch_sha256=%s\n" "${MTD_CLEANUP_PATCH_SHA256}"
      printf "mtd_cleanup_source_sha256=%s\n" "${MTD_CLEANUP_SOURCE_SHA256}"
      printf "mtd_cleanup_patched_source_sha256=%s\n" "${MTD_CLEANUP_PATCHED_SOURCE_SHA256}"
      printf "kernel_image_sha256=%s\n" \
        "$(sha256sum "${partial_output}/81_IMAGE.reinvoke-${image_suffix}" | cut -d ' ' -f 1)"
      printf "kernel_image_size=%s\n" \
        "$(wc -c < "${partial_output}/81_IMAGE.reinvoke-${image_suffix}")"
    fi
    printf "ndk_archive_sha256=%s\n" "${NDK_ARCHIVE_SHA256}"
    printf "device_tree_sha256=%s\n" "${actual_dtb_sha256}"
    printf "kernel_release=%s\n" "${kernel_release}"
    printf "kernel_load_address=%s\n" "${LOAD_ADDRESS}"
    printf "compiler=%s\n" "$("${compiler}" --version | sed -n '1p')"
    printf "compiler_sha256=%s\n" "${COMPILER_SHA256}"
    printf "linker_sha256=%s\n" "${LINKER_SHA256}"
    printf "spi_timeout_patch_sha256=%s\n" "${SPI_PATCH_SHA256}"
    printf "yaffs_reproducibility_patch_sha256=%s\n" "${YAFFS_PATCH_SHA256}"
    printf "lzo_reproducibility_patch_sha256=%s\n" "${LZO_PATCH_SHA256}"
    printf "linker=%s\n" "$("${linker}" --version | sed -n '1p')"
    printf "lzop=%s\n" "${lzop_version}"
    printf "lzop_sha256=%s\n" "${LZOP_SHA256}"
    printf "mkimage=%s\n" "${mkimage_version}"
    printf "host_make=%s\n" "$(make --version | head -1)"
    printf "host_make_sha256=%s\n" "${MAKE_SHA256}"
    printf "host_cc=%s\n" "$(gcc --version | head -1)"
    printf "host_cc_sha256=%s\n" "${HOSTCC_SHA256}"
    printf "host_cc1_sha256=%s\n" "${HOSTCC1_SHA256}"
    printf "host_as_sha256=%s\n" "${HOST_AS_SHA256}"
    printf "host_ld_sha256=%s\n" "${HOST_LD_SHA256}"
    printf "mkimage_sha256=%s\n" "${MKIMAGE_SHA256}"
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
