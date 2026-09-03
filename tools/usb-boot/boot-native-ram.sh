#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Stage and load one checksum-gated native kernel/initramfs pair from U-Boot.

set -euo pipefail

readonly KERNEL_STAGING_ADDRESS="0x0c400000"
readonly INITRAMFS_ADDRESS="0x08000000"
readonly MAX_INITRAMFS_BYTES=$((0x04400000))
# The open-source console relay can split the prompt suffix after reconnecting.
readonly UBOOT_PROMPT_PREFIX=$'\rMV88D'

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: boot-native-ram.sh --kernel PATH --kernel-sha256 SHA256 \
  --initramfs PATH --initramfs-sha256 SHA256 [options]

Options:
  --firmware-dir PATH       usb_boot firmware staging directory
  --console-log PATH        U-Boot console log (default: /tmp/uboot.log)
  --console-fifo PATH       U-Boot command FIFO (default: /tmp/uboot_cmd)
  --load-timeout SECONDS    Kernel usbload timeout (default: 120)
  --usb-timeout SECONDS     USB gadget criterion (default: 30)
  --adb-server-port PORT    ADB server port (default: 5037)
  --adb-serial SERIAL       Expected ADB serial
  --wifi-mode MODE          sta or sta-uap (default: sta)
  --wait-for-prompt         Wait indefinitely for yellow-mode U-Boot
  --prepare-only            Validate and stage without sending commands
  --help                    Show this help

The script sends only usbload, set bootargs, and bootm. It never invokes a
persistent U-Boot write command.
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

validate_sha256() {
  local expected="$1"
  local path="$2"
  local label="$3"

  printf "%s  %s\n" "${expected}" "${path}" |
    sha256sum --check --status ||
    err "${label} checksum mismatch"
}

stage_file() {
  local source_path="$1"
  local destination_path="$2"
  local partial_path="${destination_path}.partial"

  [[ ! -e "${partial_path}" ]] ||
    err "stale staging file exists: ${partial_path}"
  install -m 0644 "${source_path}" "${partial_path}"
  mv "${partial_path}" "${destination_path}"
}

console_contains_since() {
  local log_path="$1"
  local offset="$2"
  local pattern="$3"

  tail -c "+$((offset + 1))" "${log_path}" 2>/dev/null |
    grep -a -qF "${pattern}"
}

wait_for_console_text() {
  local log_path="$1"
  local offset="$2"
  local pattern="$3"
  local label="$4"
  local timeout_seconds="$5"
  local start_seconds="${SECONDS}"
  local elapsed
  local last_report=-1

  while true; do
    if console_contains_since "${log_path}" "${offset}" "${pattern}"; then
      elapsed=$((SECONDS - start_seconds))
      printf "%s complete after %d seconds\n" "${label}" "${elapsed}"
      return
    fi

    elapsed=$((SECONDS - start_seconds))
    ((elapsed < timeout_seconds)) ||
      err "${label} did not complete within ${timeout_seconds} seconds"
    if ((elapsed > 0 && elapsed % 5 == 0 && elapsed != last_report)); then
      printf "%s pending at %d seconds\n" "${label}" "${elapsed}"
      last_report="${elapsed}"
    fi
    sleep 0.2
  done
}

usb_gadget_present() {
  lsusb -d 18d1:0d02 2>/dev/null | grep -q "18d1:0d02"
}

wait_for_uboot_prompt() {
  local console_log="$1"
  local console_fifo="$2"
  local log_offset=0

  if [[ -f "${console_log}" ]]; then
    log_offset="$(stat --format="%s" "${console_log}")"
    if [[ -p "${console_fifo}" ]] &&
      grep -a -qF "${UBOOT_PROMPT_PREFIX}" "${console_log}" &&
      ! usb_gadget_present; then
      printf "\r" >"${console_fifo}"
    fi
  fi

  printf "Waiting for yellow-mode U-Boot; no timeout is applied\n"
  while true; do
    if [[ -f "${console_log}" &&
          -p "${console_fifo}" ]] &&
      console_contains_since \
        "${console_log}" "${log_offset}" "${UBOOT_PROMPT_PREFIX}" &&
      ! usb_gadget_present; then
      printf "U-Boot prompt is ready\n"
      return
    fi
    sleep 0.2
  done
}

wait_for_usb_gadget() {
  local timeout_seconds="$1"
  local start_seconds="${SECONDS}"
  local elapsed
  local last_report=-1

  while true; do
    elapsed=$((SECONDS - start_seconds))
    if usb_gadget_present; then
      printf "USB gadget 18d1:0d02 returned after %d seconds\n" "${elapsed}"
      return
    fi

    ((elapsed < timeout_seconds)) ||
      err "USB gadget did not return within ${timeout_seconds} seconds"
    if ((elapsed % 2 == 0 && elapsed != last_report)); then
      printf "USB gadget pending at %d/%d seconds\n" \
        "${elapsed}" "${timeout_seconds}"
      last_report="${elapsed}"
    fi
    sleep 0.2
  done
}

main() {
  local repo_root
  local firmware_dir=""
  local kernel_path=""
  local kernel_sha256=""
  local initramfs_path=""
  local initramfs_sha256=""
  local console_log="/tmp/uboot.log"
  local console_fifo="/tmp/uboot_cmd"
  local load_timeout=120
  local usb_timeout=30
  local adb_server_port="${INVOKE_ADB_SERVER_PORT:-5037}"
  local adb_serial="0123456789ABCDEF"
  local adb_state
  local wifi_mode="sta"
  local wait_for_prompt=0
  local prepare_only=0
  local initramfs_size
  local console_offset
  local bootargs
  local header

  repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
  firmware_dir="${INVOKE_FIRMWARE_DIR:-${repo_root}/../invoke-boot}"

  while (( $# > 0 )); do
    case "$1" in
      --kernel)
        [[ -n "${2:-}" ]] || err "--kernel requires a path"
        kernel_path="$2"
        shift 2
        ;;
      --kernel-sha256)
        [[ "${2:-}" =~ ^[0-9a-fA-F]{64}$ ]] ||
          err "--kernel-sha256 requires 64 hexadecimal characters"
        kernel_sha256="${2,,}"
        shift 2
        ;;
      --initramfs)
        [[ -n "${2:-}" ]] || err "--initramfs requires a path"
        initramfs_path="$2"
        shift 2
        ;;
      --initramfs-sha256)
        [[ "${2:-}" =~ ^[0-9a-fA-F]{64}$ ]] ||
          err "--initramfs-sha256 requires 64 hexadecimal characters"
        initramfs_sha256="${2,,}"
        shift 2
        ;;
      --firmware-dir)
        [[ -n "${2:-}" ]] || err "--firmware-dir requires a path"
        firmware_dir="$2"
        shift 2
        ;;
      --console-log)
        [[ -n "${2:-}" ]] || err "--console-log requires a path"
        console_log="$2"
        shift 2
        ;;
      --console-fifo)
        [[ -n "${2:-}" ]] || err "--console-fifo requires a path"
        console_fifo="$2"
        shift 2
        ;;
      --load-timeout)
        [[ "${2:-}" =~ ^[1-9][0-9]*$ ]] ||
          err "--load-timeout requires a positive integer"
        load_timeout="$2"
        shift 2
        ;;
      --usb-timeout)
        [[ "${2:-}" =~ ^[1-9][0-9]*$ ]] ||
          err "--usb-timeout requires a positive integer"
        usb_timeout="$2"
        shift 2
        ;;
      --adb-server-port)
        [[ "${2:-}" =~ ^[1-9][0-9]*$ ]] ||
          err "--adb-server-port requires a positive integer"
        adb_server_port="$2"
        shift 2
        ;;
      --adb-serial)
        [[ -n "${2:-}" ]] || err "--adb-serial requires a value"
        adb_serial="$2"
        shift 2
        ;;
      --wifi-mode)
        [[ "${2:-}" == "sta" || "${2:-}" == "sta-uap" ]] ||
          err "--wifi-mode must be sta or sta-uap"
        wifi_mode="$2"
        shift 2
        ;;
      --wait-for-prompt)
        wait_for_prompt=1
        shift
        ;;
      --prepare-only)
        prepare_only=1
        shift
        ;;
      --help|-h)
        usage
        ;;
      *)
        err "unknown argument: $1"
        ;;
    esac
  done

  [[ -f "${kernel_path}" ]] || err "kernel not found: ${kernel_path}"
  [[ -n "${kernel_sha256}" ]] || err "--kernel-sha256 is required"
  [[ -f "${initramfs_path}" ]] ||
    err "initramfs not found: ${initramfs_path}"
  [[ -n "${initramfs_sha256}" ]] || err "--initramfs-sha256 is required"
  [[ "${adb_server_port}" =~ ^[1-9][0-9]*$ ]] ||
    err "ADB server port must be a positive integer"
  ((adb_server_port >= 1024 && adb_server_port <= 65535)) ||
    err "ADB server port must be between 1024 and 65535"
  [[ -d "${firmware_dir}" ]] ||
    err "firmware staging directory not found: ${firmware_dir}"
  [[ ! -e "${firmware_dir}/83_IMAGE" ]] ||
    err "unsafe staging file is present: ${firmware_dir}/83_IMAGE"
  [[ ! -e "${firmware_dir}/99_IMAGE" ]] ||
    err "unsafe staging file is present: ${firmware_dir}/99_IMAGE"

  for command_name in \
    adb grep install lsusb mkimage mv sha256sum stat tail timeout; do
    require_command "${command_name}"
  done

  validate_sha256 "${kernel_sha256}" "${kernel_path}" "kernel"
  validate_sha256 "${initramfs_sha256}" "${initramfs_path}" "initramfs"

  header="$(mkimage -l "${kernel_path}")"
  grep -qF "Load Address: 02008000" <<<"${header}" ||
    err "kernel U-Boot load address is not 0x02008000"
  grep -qF "Entry Point:  02008000" <<<"${header}" ||
    err "kernel U-Boot entry point is not 0x02008000"

  initramfs_size="$(stat --format="%s" "${initramfs_path}")"
  ((initramfs_size > 0 && initramfs_size < MAX_INITRAMFS_BYTES)) ||
    err "initramfs size would overlap the kernel staging address"

  stage_file "${kernel_path}" "${firmware_dir}/81_IMAGE"
  stage_file "${initramfs_path}" "${firmware_dir}/82_IMAGE"
  printf "Staged kernel (%s bytes) and initramfs (%s bytes)\n" \
    "$(stat --format="%s" "${kernel_path}")" "${initramfs_size}"
  printf "Kernel SHA-256: %s\n" "${kernel_sha256}"
  printf "Initramfs SHA-256: %s\n" "${initramfs_sha256}"

  if ((prepare_only == 1)); then
    return 0
  fi

  if ((wait_for_prompt == 1)); then
    wait_for_uboot_prompt "${console_log}" "${console_fifo}"
  fi
  [[ -f "${console_log}" ]] ||
    err "U-Boot console log not found: ${console_log}"
  [[ -p "${console_fifo}" ]] ||
    err "U-Boot command FIFO not found: ${console_fifo}"
  grep -a -qF "${UBOOT_PROMPT_PREFIX}" "${console_log}" ||
    err "U-Boot prompt is not present in the console log"
  ! usb_gadget_present ||
    err "18d1:0d02 is already present; enter U-Boot before loading"

  console_offset="$(stat --format="%s" "${console_log}")"
  printf "usbload 0x81 %s\r" "${KERNEL_STAGING_ADDRESS}" >"${console_fifo}"
  wait_for_console_text \
    "${console_log}" "${console_offset}" "do_usbload, loading image 81" \
    "Kernel request" "${load_timeout}"
  wait_for_console_text \
    "${console_log}" "${console_offset}" "all done." \
    "Kernel transfer" "${load_timeout}"
  wait_for_console_text \
    "${console_log}" "${console_offset}" "${UBOOT_PROMPT_PREFIX}" \
    "U-Boot prompt return" "${load_timeout}"

  bootargs="console=ttyS0,115200 earlyprintk loglevel=8 debug root=/dev/ram rdinit=/init init=/init initrd=${INITRAMFS_ADDRESS},${initramfs_size}"
  if [[ "${wifi_mode}" == "sta-uap" ]]; then
    bootargs="${bootargs} reinvoke.wifi_mode=sta-uap"
  fi
  printf "Sending initramfs load and volatile boot command batch\n"
  printf "usbload 0x82 %s\rset bootargs %s\rbootm %s\r" \
    "${INITRAMFS_ADDRESS}" "${bootargs}" "${KERNEL_STAGING_ADDRESS}" \
    >"${console_fifo}"

  wait_for_usb_gadget "${usb_timeout}"
  timeout "${usb_timeout}" \
    adb -P "${adb_server_port}" -s "${adb_serial}" wait-for-device
  adb_state="$(
    adb -P "${adb_server_port}" -s "${adb_serial}" get-state
  )"
  [[ "${adb_state}" == "device" ]] ||
    err "expected ADB device did not become ready"
  printf "ADB %s is ready on server port %s\n" \
    "${adb_serial}" "${adb_server_port}"
}

main "$@"
