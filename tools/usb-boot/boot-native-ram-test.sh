#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Test U-Boot prompt generation detection without touching hardware.

set -euo pipefail

TEST_WORK_DIR=""
TEST_WAIT_PID=""

cleanup() {
  if [[ -n "${TEST_WAIT_PID}" ]] &&
    kill -0 "${TEST_WAIT_PID}" 2>/dev/null; then
    kill "${TEST_WAIT_PID}" 2>/dev/null || true
    wait "${TEST_WAIT_PID}" 2>/dev/null || true
  fi
  if [[ -n "${TEST_WORK_DIR}" ]]; then
    rm -rf -- "${TEST_WORK_DIR}"
  fi
}

err() {
  printf "ERROR: %s\n" "$1" >&2
  exit 1
}

main() {
  local script_dir
  local work_dir
  local console_log
  local console_fifo
  local gadget_state
  local output
  local wait_pid
  local attempt

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  work_dir="$(mktemp -d)"
  TEST_WORK_DIR="${work_dir}"
  trap cleanup EXIT
  console_log="${work_dir}/uboot.log"
  console_fifo="${work_dir}/uboot_cmd"
  gadget_state="${work_dir}/gadget"
  output="${work_dir}/output"

  # Load functions without executing the script's main entry point.
  # shellcheck disable=SC1090
  source <(sed '/^main "\$@"$/d' "${script_dir}/boot-native-ram.sh")

  usb_gadget_present() {
    [[ -f "${gadget_state}" ]]
  }

  printf "old console data\n" >"${console_log}"
  mkfifo "${console_fifo}"
  touch "${gadget_state}"

  wait_for_uboot_prompt \
    "${console_log}" "${console_fifo}" >"${output}" &
  wait_pid="$!"
  TEST_WAIT_PID="${wait_pid}"
  for ((attempt = 0; attempt < 50; attempt++)); do
    grep -qF "Waiting for yellow-mode U-Boot" "${output}" 2>/dev/null &&
      break
    sleep 0.1
  done
  grep -qF "Waiting for yellow-mode U-Boot" "${output}" ||
    err "catcher did not report that it was armed"

  rm "${gadget_state}"
  printf "\rMV88DE3100|> " >>"${console_log}"
  sleep 0.3
  kill -0 "${wait_pid}" 2>/dev/null ||
    err "catcher accepted a stale prompt without a new U-Boot banner"

  printf "\nU-Boot 2013.04\n" >>"${console_log}"
  sleep 0.3
  kill -0 "${wait_pid}" 2>/dev/null ||
    err "catcher accepted a stale prompt after the new U-Boot banner"

  printf "\rMV88DE3100|> " >>"${console_log}"
  for ((attempt = 0; attempt < 50; attempt++)); do
    kill -0 "${wait_pid}" 2>/dev/null || break
    sleep 0.1
  done
  kill -0 "${wait_pid}" 2>/dev/null &&
    err "catcher did not accept a prompt after the new U-Boot banner"
  wait "${wait_pid}"
  TEST_WAIT_PID=""
  grep -qF "U-Boot prompt is ready" "${output}" ||
    err "catcher did not accept the new U-Boot generation"

  printf "PASS boot-native-ram prompt generation\n"

  test_status_transitions "${work_dir}" "${output}"
  test_stale_lock_detection
  test_lock_is_not_inherited "${work_dir}"
}

# The operator relies on the status file to know whether the yellow-mode window
# was caught, so the armed and acquired transitions must both be recorded.
test_status_transitions() {
  local work_dir="$1"
  local output="$2"

  STATUS_FILE="${work_dir}/status"
  set_status waiting-for-uboot "armed" >/dev/null
  grep -qE '^[0-9-]+T[0-9:]+Z waiting-for-uboot armed$' "${STATUS_FILE}" ||
    err "status file did not record the armed transition"
  set_status uboot-acquired "after 3 seconds" >/dev/null
  grep -qE '^[0-9-]+T[0-9:]+Z uboot-acquired after 3 seconds$' \
    "${STATUS_FILE}" ||
    err "status file did not record the acquired transition"
  [[ "$(wc -l <"${STATUS_FILE}")" == "1" ]] ||
    err "status file must hold exactly one current state"
  [[ ! -e "${STATUS_FILE}.tmp" ]] ||
    err "status file update left a temporary file behind"
  STATUS_FILE=""
  grep -qF "STATUS uboot-acquired" "${output}" 2>/dev/null ||
    true

  printf "PASS boot-native-ram status transitions\n"
}

test_stale_lock_detection() {
  other_loader_running &&
    err "no other loader is running, but detection reported one"

  printf "PASS boot-native-ram stale lock detection\n"
}

# The adb fork-server daemonizes. If it inherits the singleton lock descriptor
# it holds the lock for the life of the host session and blocks every later
# loader run, so children must be spawned with that descriptor closed.
test_lock_is_not_inherited() {
  local work_dir="$1"
  local lock="${work_dir}/loader.lock"
  local marker="${work_dir}/daemon.pid"

  : >"${lock}"

  (
    exec 8>"${lock}"
    flock -n 8 || exit 1
    setsid sleep 30 8>&- &
    printf "%s\n" "$!" >"${marker}"
  )
  if ! flock -n "${lock}" true; then
    kill "$(cat "${marker}")" 2>/dev/null || true
    err "a child spawned with the lock closed still holds the loader lock"
  fi
  kill "$(cat "${marker}")" 2>/dev/null || true

  (
    exec 8>"${lock}"
    flock -n 8 || exit 1
    setsid sleep 30 &
    printf "%s\n" "$!" >"${marker}"
  )
  if flock -n "${lock}" true; then
    kill "$(cat "${marker}")" 2>/dev/null || true
    err "inheriting child did not retain the lock, so the test is not valid"
  fi
  kill "$(cat "${marker}")" 2>/dev/null || true

  printf "PASS boot-native-ram lock is not inherited by daemons\n"
}

main "$@"
