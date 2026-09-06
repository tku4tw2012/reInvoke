#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Capture one evidence bundle while an operator exercises the physical controls.
#
# Button presses reach the services as MCU publications and cannot be injected
# over WAMP, so the indicator and Bluetooth control gates need a person at the
# speaker. This harness records everything those gates need while that happens,
# so the operator only has to press buttons and never has to read logs.

set -euo pipefail

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: collect-physical-controls.sh --output-dir PATH [options]

Options:
  --adb-serial SERIAL       ADB serial (default: 0123456789ABCDEF)
  --adb-server-port PORT    ADB server port (default: 5037)
  --duration SECONDS        Capture window (default: 180)
  --output-dir PATH         New evidence directory
  --help                    Show this help

Records button publications, Bluetooth state transitions, indicator commands,
and the runtime log for the capture window. It never presses anything itself
and never calls a state-changing procedure.
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
  local duration=180
  local output_dir=""
  local monitor_pid=""
  local state_pid=""
  local log_start

  while (($# > 0)); do
    case "$1" in
      --adb-serial)
        [[ -n "${2:-}" ]] || err "--adb-serial requires a value"
        adb_serial="$2"
        shift 2
        ;;
      --adb-server-port)
        [[ "${2:-}" =~ ^[1-9][0-9]*$ ]] ||
          err "--adb-server-port requires a positive integer"
        adb_server_port="$2"
        shift 2
        ;;
      --duration)
        [[ "${2:-}" =~ ^[1-9][0-9]*$ ]] ||
          err "--duration requires a positive integer"
        duration="$2"
        shift 2
        ;;
      --output-dir)
        [[ -n "${2:-}" ]] || err "--output-dir requires a value"
        output_dir="$2"
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
  [[ ! -e "${output_dir}" ]] || err "output directory already exists"

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  repo_root="$(cd "${script_dir}/../.." && pwd)"
  mkdir -p "${output_dir}"

  export ANDROID_SERIAL="${adb_serial}"

  # Record where the log already ended so the bundle holds only this window.
  log_start="$(
    adb -P "${adb_server_port}" shell \
      '/bin/busybox wc -l < /run/reinvoke/logs/runtime.log' 2>/dev/null |
      tr -d '\r' | tr -d ' /' | tail -1
  )"
  [[ "${log_start}" =~ ^[0-9]+$ ]] || log_start=1
  printf "%s\n" "${log_start}" >"${output_dir}/log-start-line"

  node "${repo_root}/tools/control/wamp-monitor.mjs" \
    --topic com.harman.test.inputEvent \
    --topic com.harman.vui.keypress \
    --topic com.harman.volumeChanged \
    --topic com.harman.musicMuteChanged \
    --topic com.harman.stateChanged \
    --duration "${duration}" \
    >"${output_dir}/button-events.jsonl" 2>&1 &
  monitor_pid="$!"

  sample_bluetooth_state "${adb_server_port}" "${duration}" \
    >"${output_dir}/bluetooth-state.log" 2>&1 &
  state_pid="$!"

  printf "Capturing for %s seconds. Exercise the controls now.\n" "${duration}"
  printf "Evidence: %s\n" "${output_dir}"

  wait "${monitor_pid}" 2>/dev/null || true
  wait "${state_pid}" 2>/dev/null || true

  adb -P "${adb_server_port}" shell \
    "/bin/busybox tail -n +${log_start} /run/reinvoke/logs/runtime.log" \
    2>/dev/null | tr -d '\r' >"${output_dir}/runtime-window.log" || true

  grep -aE 'pairing|ledSet|ledAnimate|ledOff|bluetooth|Pairable|Discoverable' \
    "${output_dir}/runtime-window.log" \
    >"${output_dir}/indicator-and-pairing.log" 2>/dev/null || true

  summarize "${output_dir}"
}

# Poll the authoritative state file so a transition is recorded even when no
# WAMP publication accompanies it.
sample_bluetooth_state() {
  local adb_server_port="$1"
  local duration="$2"
  local elapsed=0
  local previous=""
  local current

  while ((elapsed < duration)); do
    current="$(
      adb -P "${adb_server_port}" shell \
        '/bin/busybox cat /run/reinvoke/bluetooth-state' 2>/dev/null |
        tr -d '\r' | tr -d ' /' | tail -1
    )"
    if [[ -n "${current}" && "${current}" != "${previous}" ]]; then
      printf "%s bluetooth-state=%s\n" \
        "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "${current}"
      previous="${current}"
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
}

summarize() {
  local output_dir="$1"
  local buttons

  # Count only published events. The subscription confirmation carries the same
  # topic and would otherwise be reported as a button press that never happened.
  buttons="$(
    grep -cE \
      '"type":"event".*"topic":"com\.harman\.(test\.inputEvent|vui\.keypress)"' \
      "${output_dir}/button-events.jsonl" 2>/dev/null || true
  )"
  [[ -n "${buttons}" ]] || buttons=0

  {
    printf "button_publications=%s\n" "${buttons}"
    # The first sample is the state on entry, not a transition.
    printf "bluetooth_states_recorded=%s\n" \
      "$(grep -c 'bluetooth-state=' "${output_dir}/bluetooth-state.log" \
        2>/dev/null || printf 0)"
    printf "indicator_or_pairing_lines=%s\n" \
      "$(wc -l <"${output_dir}/indicator-and-pairing.log" 2>/dev/null ||
        printf 0)"
  } >"${output_dir}/SUMMARY"

  cat "${output_dir}/SUMMARY"
}

main "$@"
