#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Collect one machine-readable native RAM acceptance bundle over ADB.

set -euo pipefail

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: collect-native-acceptance.sh --output-dir PATH [options]

Options:
  --adb-serial SERIAL       ADB serial (default: 0123456789ABCDEF)
  --adb-server-port PORT    ADB server port (default: 5037)
  --output-dir PATH         New evidence directory
  --help                    Show this help
EOF
  exit "${exit_code}"
}

err() {
  printf "ERROR: %s\n" "$1" >&2
  exit 1
}

main() {
  local script_dir
  local repo_root
  local adb_serial="0123456789ABCDEF"
  local adb_server_port=5037
  local output_dir=""
  local acceptance_command_status=0
  local acceptance_status=0
  local mcu_status=0
  local dsp_status=0
  local dsp_version_status=0
  local mic_mute_status=0
  local mic_restore_status=0
  local initial_mic_state
  local confirmed_mic_state
  local dsp_monitor_pid

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  repo_root="$(cd "${script_dir}/../.." && pwd)"
  while (( $# > 0 )); do
    case "$1" in
      --adb-serial)
        adb_serial="${2:-}"
        shift 2
        ;;
      --adb-server-port)
        adb_server_port="${2:-}"
        shift 2
        ;;
      --output-dir)
        output_dir="${2:-}"
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
  [[ -n "${output_dir}" ]] || err "--output-dir is required"
  [[ "${adb_server_port}" =~ ^[1-9][0-9]*$ ]] ||
    err "--adb-server-port must be a positive integer"
  [[ ! -e "${output_dir}" ]] ||
    err "refusing to overwrite output directory: ${output_dir}"
  for command_name in \
    adb find grep mkdir node sha256sum sleep sort tail tr xargs; do
    command -v "${command_name}" >/dev/null ||
      err "'${command_name}' is required"
  done

  mkdir -p "${output_dir}"
  adb -P "${adb_server_port}" -s "${adb_serial}" get-state |
    grep -qx device || err "ADB device is not ready"

  if adb -P "${adb_server_port}" -s "${adb_serial}" shell \
    /usr/sbin/reinvoke-acceptance >"${output_dir}/acceptance.txt" 2>&1; then
    acceptance_command_status=0
  else
    acceptance_command_status=$?
  fi
  printf "%s\n" "${acceptance_command_status}" \
    >"${output_dir}/acceptance-command.status"
  if [[ "$(
    grep -Eo 'SUMMARY failures=[0-9]+' \
      "${output_dir}/acceptance.txt" | tail -1
  )" =~ ^SUMMARY\ failures=([0-9]+)$ ]]; then
    acceptance_status="${BASH_REMATCH[1]}"
  else
    acceptance_status=255
  fi
  printf "%s\n" "${acceptance_status}" >"${output_dir}/acceptance.status"

  adb -P "${adb_server_port}" -s "${adb_serial}" shell \
    'busybox cat /etc/reinvoke-release' >"${output_dir}/release.txt"
  adb -P "${adb_server_port}" -s "${adb_serial}" shell \
    'busybox cat /proc/mounts' >"${output_dir}/mounts.txt"
  adb -P "${adb_server_port}" -s "${adb_serial}" shell \
    'busybox ps' >"${output_dir}/processes.txt"
  adb -P "${adb_server_port}" -s "${adb_serial}" shell \
    'busybox dmesg' >"${output_dir}/dmesg.txt"
  adb -P "${adb_server_port}" -s "${adb_serial}" shell \
    'busybox cat /proc/asound/cards; busybox cat /proc/asound/pcm' \
    >"${output_dir}/alsa.txt"
  adb -P "${adb_server_port}" -s "${adb_serial}" shell \
    'busybox ifconfig -a' >"${output_dir}/interfaces.txt"
  adb -P "${adb_server_port}" -s "${adb_serial}" forward \
    tcp:19999 tcp:9999 >/dev/null
  if node "${repo_root}/tools/control/wamp-call.mjs" \
    com.harman.vui.getmcustatus --timeout 8000 \
    >"${output_dir}/mcu-status.json" 2>&1; then
    mcu_status=0
  else
    mcu_status=$?
  fi
  printf "%s\n" "${mcu_status}" >"${output_dir}/mcu-status.status"
  if ((mcu_status == 0)) &&
    ! grep -q '"type": "result"' "${output_dir}/mcu-status.json"; then
    mcu_status=1
    printf "%s\n" "${mcu_status}" >"${output_dir}/mcu-status.status"
  fi

  node "${repo_root}/tools/control/wamp-monitor.mjs" \
    --topic com.harman.dsp.version --duration 10 --max-events 1 \
    >"${output_dir}/dsp-version.jsonl" 2>&1 &
  dsp_monitor_pid=$!
  sleep 1
  if node "${repo_root}/tools/control/wamp-call.mjs" \
    com.harman.dsp.getVer --timeout 8000 \
    >"${output_dir}/dsp-status.json" 2>&1; then
    dsp_status=0
  else
    dsp_status=$?
  fi
  if ((dsp_status == 0)) &&
    ! grep -q '"type": "result"' "${output_dir}/dsp-status.json"; then
    dsp_status=1
  fi
  printf "%s\n" "${dsp_status}" >"${output_dir}/dsp-status.status"
  if wait "${dsp_monitor_pid}" &&
    grep -q '"type":"event"' "${output_dir}/dsp-version.jsonl" &&
    grep -Fq '"args":[25688]' "${output_dir}/dsp-version.jsonl"; then
    dsp_version_status=0
  else
    dsp_version_status=1
  fi
  printf "%s\n" "${dsp_version_status}" \
    >"${output_dir}/dsp-version.status"

  initial_mic_state="$(
    adb -P "${adb_server_port}" -s "${adb_serial}" shell \
      '/bin/busybox cat /run/reinvoke/microphone-state' |
      tr -d '\r' |
      tail -1
  )"
  printf "%s\n" "${initial_mic_state}" \
    >"${output_dir}/microphone-state.initial"
  # Always drive the microphone away from its initial state and back again.
  # Asserting the state we already had would pass without the DSP doing
  # anything, so each step must observe an actual transition.
  if [[ "${initial_mic_state}" == "muted" ]]; then
    toggled_mic_state="unmuted"
    toggle_argument='[0]'
    restore_argument='[1]'
  else
    toggled_mic_state="muted"
    toggle_argument='[1]'
    restore_argument='[0]'
  fi

  if [[ "${initial_mic_state}" != "muted" &&
        "${initial_mic_state}" != "unmuted" ]]; then
    mic_mute_status=1
  elif node "${repo_root}/tools/control/wamp-call.mjs" \
    com.harman.dsp.micMute --args "${toggle_argument}" --timeout 8000 \
    >"${output_dir}/microphone-mute.json" 2>&1 &&
    grep -q '"type": "result"' "${output_dir}/microphone-mute.json"; then
    confirmed_mic_state="$(
      adb -P "${adb_server_port}" -s "${adb_serial}" shell \
        '/bin/busybox cat /run/reinvoke/microphone-state' |
        tr -d '\r' |
        tail -1
    )"
    [[ "${confirmed_mic_state}" == "${toggled_mic_state}" ]] || mic_mute_status=1
  else
    mic_mute_status=1
  fi
  printf "%s\n" "${mic_mute_status}" \
    >"${output_dir}/microphone-mute.status"

  mic_restore_status="${mic_mute_status}"
  if ((mic_mute_status == 0)); then
    if node "${repo_root}/tools/control/wamp-call.mjs" \
      com.harman.dsp.micMute --args "${restore_argument}" --timeout 8000 \
      >"${output_dir}/microphone-restore.json" 2>&1 &&
      grep -q '"type": "result"' \
        "${output_dir}/microphone-restore.json"; then
      confirmed_mic_state="$(
        adb -P "${adb_server_port}" -s "${adb_serial}" shell \
          '/bin/busybox cat /run/reinvoke/microphone-state' |
          tr -d '\r' |
          tail -1
      )"
      [[ "${confirmed_mic_state}" == "${initial_mic_state}" ]] ||
        mic_restore_status=1
    else
      mic_restore_status=1
    fi
  fi
  printf "%s\n" "${mic_restore_status}" \
    >"${output_dir}/microphone-restore.status"

  adb -P "${adb_server_port}" -s "${adb_serial}" pull \
    /run/reinvoke/logs "${output_dir}/logs" >/dev/null

  (
    cd "${output_dir}"
    find . -type f ! -name SHA256SUMS -print0 |
      LC_ALL=C sort --zero-terminated |
      xargs --null sha256sum >SHA256SUMS
  )
  printf "Acceptance evidence: %s\n" "${output_dir}"
  ((acceptance_command_status == 0)) ||
    err "on-device acceptance command exited ${acceptance_command_status}"
  ((acceptance_status == 0)) ||
    err "on-device acceptance reported ${acceptance_status} failures"
  ((mcu_status == 0)) || err "MCU WAMP acceptance failed"
  ((dsp_status == 0)) || err "DSP WAMP acceptance failed"
  ((dsp_version_status == 0)) || err "DSP version event acceptance failed"
  ((mic_mute_status == 0)) || err "microphone mute acceptance failed"
  ((mic_restore_status == 0)) ||
    err "microphone state restoration acceptance failed"
}

main "$@"
