#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Record one Invoke USB attempt as a timestamped evidence bundle.

set -euo pipefail

declare -a CHILD_PIDS=()
CAPTURE_PID=""
ADB_SERVER_PORT=""
ATTEMPT_DIR=""

err() {
  printf "ERROR: %s\n" "$1" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: capture-attempt.sh LABEL MODE

Modes:
  passive          Capture USB, kernel, descriptor, and ADB observations only
  original-stock   Run the original Marvell tool with stock 08_IMAGE
  original-absent  Run the original Marvell tool without 08_IMAGE
  arm-stock        Run the pinned open-source tool with stock 08_IMAGE
  arm-absent       Run the pinned open-source tool without 08_IMAGE
EOF
}

register_pid() {
  CHILD_PIDS+=("$1")
}

stop_pid() {
  local pid="$1"
  local index

  if [[ -n "${pid}" && -d "/proc/${pid}" ]]; then
    kill "${pid}" 2>/dev/null || true
    for ((index = 0; index < 20; index++)); do
      [[ ! -d "/proc/${pid}" ]] && break
      sleep 0.1
    done
    if [[ -d "/proc/${pid}" ]]; then
      kill -KILL "${pid}" 2>/dev/null || true
    fi
    wait "${pid}" 2>/dev/null || true
  fi
}

cleanup() {
  local pid

  trap - EXIT INT TERM
  stop_pid "${CAPTURE_PID}"
  for pid in "${CHILD_PIDS[@]}"; do
    stop_pid "${pid}"
  done

  if [[ -n "${ADB_SERVER_PORT}" ]]; then
    adb -P "${ADB_SERVER_PORT}" kill-server >/dev/null 2>&1 || true
  fi

  if [[ -n "${ATTEMPT_DIR}" && -d "${ATTEMPT_DIR}" ]]; then
    printf "ended_utc=%s\n" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      >> "${ATTEMPT_DIR}/manifest.txt"
    printf "\nCapture saved to %s\n" "${ATTEMPT_DIR}"
  fi
}

monitor_required_children() {
  local parent_pid="$1"
  local output="$2"
  shift 2
  local pid

  while true; do
    for pid in "$@"; do
      if [[ ! -d "/proc/${pid}" ]]; then
        printf "%s required process %s exited\n" \
          "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${pid}" |
          tee -a "${output}" >&2
        kill -TERM "${parent_pid}"
        return
      fi
    done
    sleep 0.5
  done
}

main() {
  local label="${1:-}"
  local mode="${2:-}"
  local repo_root
  local archive_root
  local firmware_dir
  local hardware_root
  local attempt_root
  local timestamp
  local usbmon_interface
  local capture_limit_seconds
  local adb_server_port
  local script_dir
  local arm_binary
  local tool_binary=""
  local tool_kind=""
  local variant=""
  local pid_file
  local pid
  local index
  local capture_status
  local -a required_pids=()

  if [[ -z "${label}" || -z "${mode}" ]]; then
    usage
    exit 2
  fi
  [[ "${label}" =~ ^[a-z0-9][a-z0-9-]*$ ]] ||
    err "LABEL must use lowercase letters, digits, and hyphens"

  case "${mode}" in
    passive) ;;
    original-stock)
      tool_kind="original"
      variant="stock"
      ;;
    original-absent)
      tool_kind="original"
      variant="absent"
      ;;
    arm-stock)
      tool_kind="arm"
      variant="stock"
      ;;
    arm-absent)
      tool_kind="arm"
      variant="absent"
      ;;
    *)
      usage
      err "Unknown mode: ${mode}"
      ;;
  esac

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  repo_root="$(cd "${script_dir}/../.." && pwd)"
  archive_root="${REINVOKE_ARCHIVE:-${repo_root}/../reinvoke-archive}"
  firmware_dir="${INVOKE_FIRMWARE_DIR:-${repo_root}/../invoke-boot}"
  repo_root="$(realpath "${repo_root}")"
  archive_root="$(realpath "${archive_root}")"
  firmware_dir="$(realpath "${firmware_dir}")"
  [[ -d "${archive_root}" ]] || err "Archive root not found: ${archive_root}"
  [[ -d "${firmware_dir}" ]] || err "Firmware staging directory not found: ${firmware_dir}"
  [[ "${archive_root}" != "${repo_root}" &&
    "${archive_root}" != "${repo_root}/"* ]] ||
    err "Attempt captures must remain outside Git"

  usbmon_interface="${INVOKE_USBMON_INTERFACE:-usbmon3}"
  [[ "${usbmon_interface}" =~ ^usbmon[1-9][0-9]*$ ]] ||
    err "Use one bus-specific usbmon interface, not usbmon0"
  capture_limit_seconds="${INVOKE_CAPTURE_LIMIT_SECONDS:-600}"
  [[ "${capture_limit_seconds}" =~ ^[0-9]+$ ]] ||
    err "INVOKE_CAPTURE_LIMIT_SECONDS must be an integer"
  ((capture_limit_seconds >= 60 && capture_limit_seconds <= 3600)) ||
    err "Capture limit must be between 60 and 3600 seconds"
  adb_server_port="${INVOKE_ADB_SERVER_PORT:-5038}"
  [[ "${adb_server_port}" =~ ^[0-9]+$ ]] ||
    err "INVOKE_ADB_SERVER_PORT must be an integer"
  ((adb_server_port >= 1024 && adb_server_port <= 65535)) ||
    err "ADB server port must be between 1024 and 65535"
  if ss -ltn | grep -qE "127\\.0\\.0\\.1:${adb_server_port}|0\\.0\\.0\\.0:${adb_server_port}"; then
    err "ADB server port ${adb_server_port} is already in use"
  fi
  ADB_SERVER_PORT="${adb_server_port}"

  timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
  hardware_root="${archive_root}/hardware"
  attempt_root="${hardware_root}/usb-attempts"
  [[ ! -L "${hardware_root}" ]] ||
    err "Archive hardware path must not be a symlink"
  [[ ! -L "${attempt_root}" ]] ||
    err "USB attempt path must not be a symlink"
  if [[ -e "${hardware_root}" && ! -d "${hardware_root}" ]]; then
    err "Archive hardware path is not a directory"
  fi
  if [[ -e "${attempt_root}" && ! -d "${attempt_root}" ]]; then
    err "USB attempt path is not a directory"
  fi
  [[ -d "${hardware_root}" ]] || mkdir -m 0700 "${hardware_root}"
  [[ -d "${attempt_root}" ]] || mkdir -m 0700 "${attempt_root}"
  hardware_root="$(realpath "${hardware_root}")"
  attempt_root="$(realpath "${attempt_root}")"
  [[ "${hardware_root}" == "${archive_root}/"* ]] ||
    err "Archive hardware path resolves outside the archive"
  [[ "${attempt_root}" == "${hardware_root}/"* ]] ||
    err "USB attempt path resolves outside the hardware directory"

  ATTEMPT_DIR="${attempt_root}/${timestamp}-${label}-${mode}"
  umask 077
  [[ ! -e "${ATTEMPT_DIR}" && ! -L "${ATTEMPT_DIR}" ]] ||
    err "Attempt directory already exists: ${ATTEMPT_DIR}"
  mkdir -m 0700 "${ATTEMPT_DIR}"
  ATTEMPT_DIR="$(realpath "${ATTEMPT_DIR}")"
  [[ "${ATTEMPT_DIR}" == "${attempt_root}/"* ]] ||
    err "Attempt directory resolves outside the attempt root"

  arm_binary="${archive_root}/tools/hk-invoke-arm-flasher/63444e82/usb_boot_arm"
  case "${tool_kind}" in
    original)
      tool_binary="${firmware_dir}/usb_boot"
      ;;
    arm)
      tool_binary="${arm_binary}"
      [[ -x "${tool_binary}" ]] ||
        err "Build the pinned tool first with ${script_dir}/build-arm-flasher.sh"
      ;;
  esac

  {
    printf "label=%s\n" "${label}"
    printf "mode=%s\n" "${mode}"
    printf "started_utc=%s\n" "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf "hostname=%s\n" "$(hostname)"
    printf "kernel=%s\n" "$(uname -srmo)"
    printf "repo_commit=%s\n" "$(git -C "${repo_root}" rev-parse HEAD)"
    printf "usbmon_interface=%s\n" "${usbmon_interface}"
    printf "capture_limit_seconds=%s\n" "${capture_limit_seconds}"
    printf "adb_server_port=%s\n" "${adb_server_port}"
    if [[ -n "${tool_binary}" ]]; then
      printf "tool=%s\n" "${tool_binary}"
      sha256sum "${tool_binary}"
    fi
    printf "\n[usb-topology]\n"
    lsusb -t
    printf "\n[usb-devices]\n"
    lsusb
    printf "\n[staged-file-hashes]\n"
    find "${firmware_dir}" -maxdepth 1 -type f \
      \( -name '*_IMAGE' -o -name '*.img' -o -name '*.usb' \) \
      -print0 | sort -z | xargs -0 -r sha256sum
  } > "${ATTEMPT_DIR}/manifest.txt"

  trap cleanup EXIT
  trap 'exit 130' INT TERM

  dumpcap -D | grep -qE "^[0-9]+\. ${usbmon_interface}$" ||
    err "dumpcap cannot see ${usbmon_interface}; load the usbmon kernel module first."

  "${script_dir}/monitor-descriptors.sh" \
    "${ATTEMPT_DIR}/descriptors.log" \
    > "${ATTEMPT_DIR}/descriptor-monitor.log" 2>&1 &
  register_pid "$!"

  journalctl -k -f --since now -o short-precise --no-pager \
    > "${ATTEMPT_DIR}/kernel.log" 2>&1 &
  register_pid "$!"

  adb -P "${adb_server_port}" start-server \
    > "${ATTEMPT_DIR}/adb-track.log" 2>&1
  stdbuf -oL adb -P "${adb_server_port}" track-devices \
    >> "${ATTEMPT_DIR}/adb-track.log" 2>&1 &
  register_pid "$!"

  if [[ -n "${tool_kind}" ]]; then
    INVOKE_ATTEMPT_DIR="${ATTEMPT_DIR}" \
      INVOKE_FIRMWARE_DIR="${firmware_dir}" \
      INVOKE_USB_BOOT_BIN="${tool_binary}" \
      INVOKE_USB_BOOT_KIND="${tool_kind}" \
      "${script_dir}/start-session.sh" "${variant}"
    for pid_file in "${ATTEMPT_DIR}/usb-boot.pid" \
      "${ATTEMPT_DIR}/console-client.pid"; do
      pid="$(< "${pid_file}")"
      register_pid "${pid}"
      required_pids+=("${pid}")
    done
    monitor_required_children "${BASHPID}" \
      "${ATTEMPT_DIR}/session-health.log" \
      "${required_pids[@]}" &
    register_pid "$!"
  fi

  printf "Capture is running: %s\n" "${ATTEMPT_DIR}"
  printf "Perform the '%s' physical attempt, then press Ctrl-C.\n" "${label}"
  printf "dumpcap will print its listening message when packet capture is ready.\n"

  set +e
  timeout --signal=INT --kill-after=5s "${capture_limit_seconds}" \
    dumpcap -i "${usbmon_interface}" -s 0 -w \
      "${ATTEMPT_DIR}/usbmon.pcap" \
      > /dev/null \
      2> >(tee "${ATTEMPT_DIR}/dumpcap.log" >&2) &
  CAPTURE_PID="$!"
  wait "${CAPTURE_PID}"
  capture_status="$?"
  CAPTURE_PID=""
  set -e

  case "${capture_status}" in
    0 | 124 | 130) ;;
    *)
      err "dumpcap capture failed with status ${capture_status}"
      ;;
  esac
}

main "$@"
