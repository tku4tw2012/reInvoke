#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Build a RAM-only diagnostic initramfs from the preserved recovery image.

set -euo pipefail

readonly EXPECTED_SOURCE_SHA256="08a8f96a5c476a08ba19441d83637e606f27f442d56c2689dd6b56d2fc72b7a8"
readonly EXPECTED_PROVISIOND_SHA256="a4024dac2b178e1060eda8a240f644b0981c66ee82e8c32c7da34d59184a07b1"

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: build-native-initramfs.sh \
  --source-initramfs PATH \
  --donor-rootfs PATH \
  [--kernel-modules PATH] \
  [--provisiond PATH] \
  --output PATH

Builds a RAM-only 82_IMAGE derivative. The donor rootfs supplies the Invoke's
SD8887 Wi-Fi and Bluetooth firmware, calibration, and transmit-power data.
An optional kernel module installation root can supply replacement-kernel
modules. No proprietary input or generated image is copied into the repository.
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
  local source_initramfs=""
  local donor_rootfs=""
  local kernel_modules=""
  local provisiond=""
  local output_path=""
  local script_dir
  local work_dir
  local rootfs_dir
  local archive_listing
  local cleanup_command
  local candidate
  local donor_version
  local firmware_dir
  local module_release=""
  local module_tree=""
  local module_flavor=""
  local bluetooth_module=""
  local -a module_trees=()

  while (( $# > 0 )); do
    case "$1" in
      --source-initramfs)
        [[ -n "${2:-}" ]] || err "--source-initramfs requires a path"
        source_initramfs="$2"
        shift 2
        ;;
      --donor-rootfs)
        [[ -n "${2:-}" ]] || err "--donor-rootfs requires a path"
        donor_rootfs="$2"
        shift 2
        ;;
      --kernel-modules)
        [[ -n "${2:-}" ]] || err "--kernel-modules requires a path"
        kernel_modules="$2"
        shift 2
        ;;
      --provisiond)
        [[ -n "${2:-}" ]] || err "--provisiond requires a path"
        provisiond="$2"
        shift 2
        ;;
      --output)
        [[ -n "${2:-}" ]] || err "--output requires a path"
        output_path="$2"
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

  [[ -f "${source_initramfs}" ]] ||
    err "source initramfs not found: ${source_initramfs}"
  [[ -d "${donor_rootfs}" ]] ||
    err "donor rootfs not found: ${donor_rootfs}"
  [[ -n "${output_path}" ]] || err "--output is required"
  [[ ! -e "${output_path}" ]] ||
    err "refusing to overwrite existing output: ${output_path}"

  for command_name in cp cpio find gzip install sha256sum sort; do
    require_command "${command_name}"
  done

  printf "%s  %s\n" \
    "${EXPECTED_SOURCE_SHA256}" \
    "${source_initramfs}" |
    sha256sum --check --status ||
    err "source initramfs checksum is not the reviewed OTA2 82_IMAGE"

  firmware_dir="${donor_rootfs}/lib/firmware/mrvl"
  for relative_path in \
    "lib/firmware/mrvl/sd8887_wlan_a2_p78.bin" \
    "lib/firmware/mrvl/sd8887_bt_a2_new.bin" \
    "lib/firmware/mrvl/WlanCalData_ext-LS9AD-20160725.conf" \
    "lib/firmware/mrvl/txpwrlimit_cfg_8887.bin" \
    "etc/version.txt"; do
    [[ -f "${donor_rootfs}/${relative_path}" ]] ||
      err "donor file not found: ${relative_path}"
  done

  if [[ -n "${kernel_modules}" ]]; then
    [[ -d "${kernel_modules}/lib/modules" ]] ||
      err "kernel module root has no lib/modules directory"
    mapfile -t module_trees < <(
      find "${kernel_modules}/lib/modules" \
        -mindepth 1 -maxdepth 1 -type d -print
    )
    (( ${#module_trees[@]} == 1 )) ||
      err "kernel module root must contain exactly one release"
    module_tree="${module_trees[0]}"
    module_release="${module_tree##*/}"
    if [[ -f "${module_tree}/kernel/arch/arm/mach-berlin/modules/wlan_sd8887/mlan.ko" &&
          -f "${module_tree}/kernel/arch/arm/mach-berlin/modules/wlan_sd8887/sd8xxx.ko" ]]; then
      module_flavor="wlan_sd8887"
    elif [[ -f "${module_tree}/kernel/arch/arm/mach-berlin/modules/wlan_sd8801/88mlan.ko" &&
            -f "${module_tree}/kernel/arch/arm/mach-berlin/modules/wlan_sd8801/sd8801.ko" ]]; then
      module_flavor="wlan_sd8801"
    else
      err "kernel module root has no reviewed SD8887-compatible module set"
    fi
    for candidate in \
      "${module_tree}/kernel/arch/arm/mach-berlin/modules/bt_sd8887/bt8xxx.ko" \
      "${module_tree}/extra/bt8xxx.ko"; do
      if [[ -f "${candidate}" ]]; then
        bluetooth_module="${candidate#"${module_tree}/"}"
        break
      fi
    done
    [[ -n "${bluetooth_module}" ]] ||
      err "kernel module root has no native SD8887 Bluetooth module"

  fi
  if [[ -n "${provisiond}" ]]; then
    [[ -f "${provisiond}" ]] ||
      err "provisioning daemon not found: ${provisiond}"
    printf "%s  %s\n" "${EXPECTED_PROVISIOND_SHA256}" "${provisiond}" |
      sha256sum --check --status ||
      err "provisioning daemon checksum mismatch"
  fi

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  work_dir="$(mktemp -d "${TMPDIR:-/tmp}/reinvoke-initramfs.XXXXXX")"
  printf -v cleanup_command 'rm -rf -- %q' "${work_dir}"
  trap "${cleanup_command}" EXIT
  rootfs_dir="${work_dir}/rootfs"
  archive_listing="${work_dir}/archive.list"
  mkdir -p "${rootfs_dir}" "$(dirname "${output_path}")"

  gzip --decompress --stdout "${source_initramfs}" |
    cpio --list > "${archive_listing}"
  if grep -Eq '(^/|(^|/)\.\.(/|$))' "${archive_listing}"; then
    err "source initramfs contains an unsafe path"
  fi

  (
    cd "${rootfs_dir}"
    gzip --decompress --stdout "${source_initramfs}" |
      cpio --extract --make-directories --no-absolute-filenames
  )

  install -m 0755 "${script_dir}/native-ram-init" "${rootfs_dir}/init"
  install -m 0644 \
    "${firmware_dir}/sd8887_wlan_a2_p78.bin" \
    "${firmware_dir}/sd8887_bt_a2_new.bin" \
    "${firmware_dir}/WlanCalData_ext-LS9AD-20160725.conf" \
    "${firmware_dir}/txpwrlimit_cfg_8887.bin" \
    "${rootfs_dir}/lib/firmware/mrvl/"

  if [[ -n "${module_tree}" ]]; then
    cp -a "${module_tree}" "${rootfs_dir}/lib/modules/"
  fi
  if [[ -n "${provisiond}" ]]; then
    install -m 0755 "${provisiond}" \
      "${rootfs_dir}/usr/sbin/reinvoke-provisiond"
  fi

  rm -f \
    "${rootfs_dir}/bin/flash_custk" \
    "${rootfs_dir}/home/galois/run.sh"

  donor_version="$(tr -d '\r\n' < "${donor_rootfs}/etc/version.txt")"
  {
    printf "reInvoke native RAM diagnostics 0.1\n"
    printf "source: reviewed OTA2 recovery initramfs\n"
    printf "hardware donor: %s\n" "${donor_version}"
    if [[ -n "${module_release}" ]]; then
      printf "replacement kernel modules: %s\n" "${module_release}"
      printf "replacement module flavor: %s\n" "${module_flavor}"
      printf "replacement bluetooth module: %s\n" "${bluetooth_module}"
    fi
    if [[ -n "${provisiond}" ]]; then
      printf "provisioning daemon: included, manual start only\n"
    fi
    printf "storage policy: no NAND partitions mounted\n"
  } > "${rootfs_dir}/etc/reinvoke-release"

  umask 077
  (
    cd "${rootfs_dir}"
    find . -print0 |
      LC_ALL=C sort --zero-terminated |
      cpio --null --create --format=newc --owner=0:0 |
      gzip --no-name --best
  ) > "${output_path}"

  gzip --test "${output_path}"
  stat --format="%n %s bytes" "${output_path}"
  sha256sum "${output_path}"

  trap - EXIT
  rm -rf -- "${work_dir}"
}

main "$@"
