#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Bring up the Marvell usb_boot session for the Harman Kardon Invoke.
#
# Starts usb_boot and attaches the console client that it requires before it
# will watch USB. Logs:
#   usbboot.log       - USB boot protocol output
#   uboot-console.log - U-Boot console transcript
#
# Send commands to the prompt with:  echo 'help' > /tmp/uboot_cmd
set -euo pipefail

BOOT_PID=""
CONSOLE_PID=""
KEEP_RUNNING=0

err() {
  printf "FATAL: %s\n" "$1" >&2
  exit 1
}

cleanup_failed_start() {
  local pid

  ((KEEP_RUNNING == 0)) || return
  for pid in "${CONSOLE_PID}" "${BOOT_PID}"; do
    if [[ -n "${pid}" && -d "/proc/${pid}" ]]; then
      kill "${pid}" 2>/dev/null || true
      wait "${pid}" 2>/dev/null || true
    fi
  done
}

port_is_listening() {
  local port="$1"

  ss -ltn | grep -qE \
    "127\\.0\\.0\\.1:${port}|0\\.0\\.0\\.0:${port}|\\[::\\]:${port}"
}

main() {
  local script_dir
  local port=8141
  local variant="${1:-stock}"
  local firmware_dir
  local boot_bin
  local boot_kind="${INVOKE_USB_BOOT_KIND:-original}"
  local auto_stop_seconds="${INVOKE_AUTO_STOP_SECONDS:-0}"
  local attempt_dir
  local lock_root
  local lock_file
  local file
  local index

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  firmware_dir="${INVOKE_FIRMWARE_DIR:-${script_dir}}"
  [[ -d "${firmware_dir}" ]] ||
    err "Firmware staging directory not found: ${firmware_dir}"
  firmware_dir="$(cd "${firmware_dir}" && pwd)"
  boot_bin="${INVOKE_USB_BOOT_BIN:-${firmware_dir}/usb_boot}"
  attempt_dir="${INVOKE_ATTEMPT_DIR:-${firmware_dir}}"
  lock_root="${XDG_RUNTIME_DIR:-/tmp}"
  lock_file="${lock_root}/reinvoke-usb-boot-$(id -u).lock"

  mkdir -p "${attempt_dir}"
  attempt_dir="$(cd "${attempt_dir}" && pwd)"
  cd "${firmware_dir}"
  trap cleanup_failed_start EXIT
  umask 077
  exec 9> "${lock_file}"
  flock -n 9 ||
    err "Another Invoke USB boot session is already running"

  [[ -x "${boot_bin}" ]] ||
    err "USB boot tool is not executable: ${boot_bin}"
  [[ "${boot_kind}" == "original" || "${boot_kind}" == "arm" ]] ||
    err "INVOKE_USB_BOOT_KIND must be original or arm"
  [[ "${auto_stop_seconds}" =~ ^[0-9]+$ ]] ||
    err "INVOKE_AUTO_STOP_SECONDS must be an integer"

  for file in bcm_erom.bin.usb bootloader.img sysinit.img drm_erom.img; do
    [[ -f "${file}" ]] || err "Missing required file: ${file}"
  done

  for file in 83_IMAGE 99_IMAGE; do
    if [[ -f "${file}" ]]; then
      err "${file} is present. Remove it before running against a working unit."
    fi
  done

  if [[ ! -f 08_IMAGE.stock ]]; then
    [[ -f 08_IMAGE ]] || err "08_IMAGE is missing; cannot preserve its stock copy"
    cp 08_IMAGE 08_IMAGE.stock
  fi

  case "${variant}" in
    stock)
      cp 08_IMAGE.stock 08_IMAGE
      ;;
    absent)
      rm -f 08_IMAGE
      ;;
    *)
      err "Unknown variant '${variant}' (stock|absent)"
      ;;
  esac
  printf "variant: %s\n" "${variant}"
  printf "tool: %s (%s)\n" "${boot_bin}" "${boot_kind}"

  [[ -f 79_IMAGE ]] || cp 79_IMAGE.uboot_cmdline 79_IMAGE
  if grep -qvE '^#|^$' 79_IMAGE 2>/dev/null; then
    printf "FATAL: 79_IMAGE contains commands that would run automatically:\n" >&2
    grep -vE '^#|^$' 79_IMAGE >&2
    exit 1
  fi

  if port_is_listening "${port}"; then
    err "TCP port ${port} is already in use; stop that known process first."
  fi

  rm -f "${attempt_dir}/usbboot.log" \
    "${attempt_dir}/uboot-console.log" \
    "${attempt_dir}/uboot-console-errors.log" \
    /tmp/uboot.log \
    /tmp/uboot_cmd
  touch "${attempt_dir}/uboot-console.log"
  ln -s "${attempt_dir}/uboot-console.log" /tmp/uboot.log

  nohup stdbuf -oL -eL "${boot_bin}" 1286 8174 "${firmware_dir}/" "${port}" \
    > "${attempt_dir}/usbboot.log" 2>&1 &
  BOOT_PID="$!"
  printf "%s\n" "${BOOT_PID}" > "${attempt_dir}/usb-boot.pid"

  nohup "${script_dir}/attach-console.sh" "${port}" \
    "${script_dir}/uboot-console.py" 180 \
    > "${attempt_dir}/uboot-console-errors.log" 2>&1 &
  CONSOLE_PID="$!"
  printf "%s\n" "${CONSOLE_PID}" > "${attempt_dir}/console-client.pid"

  for ((index = 0; index < 100; index++)); do
    if [[ "${boot_kind}" == "original" ]] &&
      grep -q "polling_for_hotplug_event" "${attempt_dir}/usbboot.log"; then
      break
    fi
    if [[ "${boot_kind}" == "arm" ]] &&
      grep -q "Device not found. Waiting" "${attempt_dir}/usbboot.log"; then
      break
    fi
    if [[ ! -d "/proc/${BOOT_PID}" ]]; then
      printf "FATAL: USB boot tool exited before becoming ready.\n" >&2
      tail -20 "${attempt_dir}/usbboot.log" >&2
      exit 1
    fi
    sleep 0.1
  done

  if [[ "${boot_kind}" == "original" ]] &&
    grep -q "polling_for_hotplug_event" "${attempt_dir}/usbboot.log"; then
    KEEP_RUNNING=1
    printf "READY: original usb_boot is polling for the device.\n"
  elif [[ "${boot_kind}" == "arm" ]] &&
    grep -q "Device not found. Waiting" "${attempt_dir}/usbboot.log"; then
    KEEP_RUNNING=1
    printf "READY: pinned open-source tool is polling for the device.\n"
  else
    printf "FATAL: USB boot tool never entered its device polling loop.\n" >&2
    tail -20 "${attempt_dir}/usbboot.log" >&2
    exit 1
  fi
  printf "Arm download mode: hold Reset, reconnect power, press MicOff 4x within 5s.\n"

  if ((auto_stop_seconds > 0)); then
    printf "Smoke-test auto-stop in %s seconds.\n" "${auto_stop_seconds}"
    sleep "${auto_stop_seconds}"
    KEEP_RUNNING=0
    cleanup_failed_start
  fi
}

main "$@"
