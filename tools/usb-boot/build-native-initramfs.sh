#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Build a RAM-only diagnostic initramfs from the preserved recovery image.

set -euo pipefail

readonly EXPECTED_SOURCE_SHA256="08a8f96a5c476a08ba19441d83637e606f27f442d56c2689dd6b56d2fc72b7a8"
readonly EXPECTED_PROVISIOND_SHA256="5bde5aefdb21a9caf605fb57e9a62cf9597b8ebddd1fc9d65938441d04678b07"
readonly EXPECTED_WIFI_APPLYD_SHA256="6697df000d130a6461d1e3f57b6ebe8b1ad1742984a94250bc1e243dca097610"
readonly EXPECTED_NETWORKD_SHA256="cb61bcdd0b9f4b145619514b9acb41d74d98042f8698419ea37e0c4864340a66"
readonly EXPECTED_MODULE_TREE_MANIFEST_SHA256="06d7a5f5bc43c3b3d869b9b962e1ef70d7f3c3fc15d934c8dc020332b57b940a"
readonly MAX_NATIVE_INITRAMFS_BYTES=$((60 * 1024 * 1024))

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: build-native-initramfs.sh \
  --source-initramfs PATH \
  --donor-rootfs PATH \
  [--kernel-modules PATH] \
  [--provisiond PATH] \
  [--wifi-applyd PATH] \
  [--networkd PATH] \
  [--runtime-bundle PATH --runtime-manifest-sha256 SHA256] \
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
  local wifi_applyd=""
  local networkd=""
  local runtime_bundle=""
  local runtime_manifest_sha256=""
  local output_path=""
  local output_partial
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
  local module_tree_manifest_sha256
  local output_size
  local -a module_trees=()
  local -a module_symlinks=()

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
      --wifi-applyd)
        [[ -n "${2:-}" ]] || err "--wifi-applyd requires a path"
        wifi_applyd="$2"
        shift 2
        ;;
      --networkd)
        [[ -n "${2:-}" ]] || err "--networkd requires a path"
        networkd="$2"
        shift 2
        ;;
      --runtime-bundle)
        [[ -n "${2:-}" ]] || err "--runtime-bundle requires a path"
        runtime_bundle="$2"
        shift 2
        ;;
      --runtime-manifest-sha256)
        [[ "${2:-}" =~ ^[0-9a-fA-F]{64}$ ]] ||
          err "--runtime-manifest-sha256 requires 64 hexadecimal characters"
        runtime_manifest_sha256="${2,,}"
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

  for command_name in awk cp cpio find gzip install realpath sha256sum sort stat touch xargs; do
    require_command "${command_name}"
  done
  source_initramfs="$(realpath "${source_initramfs}")"
  donor_rootfs="$(realpath "${donor_rootfs}")"
  output_path="$(realpath --canonicalize-missing "${output_path}")"
  output_partial="${output_path}.partial"
  [[ ! -e "${output_path}" ]] ||
    err "refusing to overwrite existing output: ${output_path}"
  [[ ! -e "${output_partial}" ]] ||
    err "stale partial output exists: ${output_partial}"
  if [[ -n "${kernel_modules}" ]]; then
    kernel_modules="$(realpath "${kernel_modules}")"
  fi
  if [[ -n "${provisiond}" ]]; then
    provisiond="$(realpath "${provisiond}")"
  fi
  if [[ -n "${wifi_applyd}" ]]; then
    wifi_applyd="$(realpath "${wifi_applyd}")"
  fi
  if [[ -n "${networkd}" ]]; then
    networkd="$(realpath "${networkd}")"
  fi
  if [[ -n "${runtime_bundle}" ]]; then
    runtime_bundle="$(realpath "${runtime_bundle}")"
  fi

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
    mapfile -t module_symlinks < <(
      find "${module_tree}" -type l -print |
        LC_ALL=C sort
    )
    (( ${#module_symlinks[@]} == 2 )) &&
      [[ "${module_symlinks[0]}" == "${module_tree}/build" ]] &&
      [[ "${module_symlinks[1]}" == "${module_tree}/source" ]] ||
      err "kernel module tree has unexpected symlinks"
    module_tree_manifest_sha256="$(
      cd "${kernel_modules}"
      find . -type f -print0 |
        LC_ALL=C sort --zero-terminated |
        xargs --null sha256sum |
        sha256sum |
        awk '{print $1}'
    )"
    [[ "${module_tree_manifest_sha256}" == \
      "${EXPECTED_MODULE_TREE_MANIFEST_SHA256}" ]] ||
      err "kernel module tree checksum mismatch"

  fi
  if [[ -n "${provisiond}" ]]; then
    [[ -f "${provisiond}" ]] ||
      err "provisioning daemon not found: ${provisiond}"
    printf "%s  %s\n" "${EXPECTED_PROVISIOND_SHA256}" "${provisiond}" |
      sha256sum --check --status ||
      err "provisioning daemon checksum mismatch"
  fi
  if [[ -n "${wifi_applyd}" ]]; then
    [[ -f "${wifi_applyd}" ]] ||
      err "Wi-Fi apply daemon not found: ${wifi_applyd}"
    printf "%s  %s\n" "${EXPECTED_WIFI_APPLYD_SHA256}" "${wifi_applyd}" |
      sha256sum --check --status ||
      err "Wi-Fi apply daemon checksum mismatch"
  fi
  if [[ -n "${networkd}" ]]; then
    [[ -f "${networkd}" ]] ||
      err "network lifecycle service not found: ${networkd}"
    printf "%s  %s\n" "${EXPECTED_NETWORKD_SHA256}" "${networkd}" |
      sha256sum --check --status ||
      err "network lifecycle service checksum mismatch"
  fi
  if [[ -n "${runtime_bundle}" || -n "${runtime_manifest_sha256}" ]]; then
    [[ -n "${runtime_bundle}" && -n "${runtime_manifest_sha256}" ]] ||
      err "runtime bundle and manifest checksum must be supplied together"
    [[ -d "${runtime_bundle}" ]] ||
      err "runtime bundle not found: ${runtime_bundle}"
    [[ -f "${runtime_bundle}/SHA256SUMS" ]] ||
      err "runtime bundle has no SHA256SUMS"
    printf "%s  %s\n" \
      "${runtime_manifest_sha256}" "${runtime_bundle}/SHA256SUMS" |
      sha256sum --check --status ||
      err "runtime bundle manifest checksum mismatch"
    if find "${runtime_bundle}" -type l -print -quit | grep -q .; then
      err "runtime bundle contains a symbolic link"
    fi
    (
      cd "${runtime_bundle}"
      sha256sum --check --strict SHA256SUMS
    ) >/dev/null || err "runtime bundle file checksum mismatch"
  fi

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  work_dir="$(mktemp -d "${TMPDIR:-/tmp}/reinvoke-initramfs.XXXXXX")"
  printf -v cleanup_command \
    'rm -rf -- %q; rm -f -- %q' \
    "${work_dir}" "${output_partial}"
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
  install -m 0755 "${script_dir}/native-acceptance.sh" \
    "${rootfs_dir}/usr/sbin/reinvoke-acceptance"
  install -m 0644 \
    "${firmware_dir}/sd8887_wlan_a2_p78.bin" \
    "${firmware_dir}/sd8887_bt_a2_new.bin" \
    "${firmware_dir}/WlanCalData_ext-LS9AD-20160725.conf" \
    "${firmware_dir}/txpwrlimit_cfg_8887.bin" \
    "${rootfs_dir}/lib/firmware/mrvl/"

  if [[ -n "${module_tree}" ]]; then
    cp -a "${module_tree}" "${rootfs_dir}/lib/modules/"
  fi
  find "${rootfs_dir}/lib/modules" \
    -mindepth 2 -maxdepth 2 -type l \
    \( -name build -o -name source \) -delete
  if find "${rootfs_dir}/lib/modules" \
    -mindepth 2 -maxdepth 2 -type l \
    \( -name build -o -name source \) -print -quit |
    grep -q .; then
    err "failed to remove host-only kernel module symlinks"
  fi
  if [[ -n "${provisiond}" ]]; then
    install -m 0755 "${provisiond}" \
      "${rootfs_dir}/usr/sbin/reinvoke-provisiond"
  fi
  if [[ -n "${wifi_applyd}" ]]; then
    install -m 0755 "${wifi_applyd}" \
      "${rootfs_dir}/usr/sbin/reinvoke-wifi-applyd"
  fi
  if [[ -n "${networkd}" ]]; then
    install -m 0755 "${networkd}" \
      "${rootfs_dir}/usr/sbin/reinvoke-networkd"
  fi
  if [[ -n "${runtime_bundle}" ]]; then
    mkdir -p "${rootfs_dir}/opt/reinvoke"
    cp -a "${runtime_bundle}/." "${rootfs_dir}/opt/reinvoke/"
    rm -rf "${rootfs_dir}/home/galois"
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
    if [[ -n "${wifi_applyd}" ]]; then
      printf "Wi-Fi apply daemon: included, manual start only\n"
    fi
    if [[ -n "${networkd}" ]]; then
      printf "network lifecycle service: included, auto-started\n"
    fi
    if [[ -n "${runtime_bundle}" ]]; then
      printf "autonomous runtime: included, auto-started\n"
      printf "autonomous runtime manifest: %s\n" \
        "${runtime_manifest_sha256}"
    fi
    printf "storage policy: no NAND partitions mounted\n"
  } > "${rootfs_dir}/etc/reinvoke-release"

  umask 077
  find "${rootfs_dir}" -exec touch --no-dereference --date="@0" {} +
  (
    cd "${rootfs_dir}"
    find . -print0 |
      LC_ALL=C sort --zero-terminated |
      cpio --null --create --format=newc --owner=0:0 --reproducible |
      gzip --no-name --best
  ) > "${output_partial}"

  gzip --test "${output_partial}"
  output_size="$(stat --format="%s" "${output_partial}")"
  ((output_size <= MAX_NATIVE_INITRAMFS_BYTES)) ||
    err "initramfs exceeds the 60 MiB autonomous-runtime budget"
  mv "${output_partial}" "${output_path}"
  stat --format="%n %s bytes" "${output_path}"
  sha256sum "${output_path}"

  trap - EXIT
  rm -rf -- "${work_dir}"
}

main "$@"
