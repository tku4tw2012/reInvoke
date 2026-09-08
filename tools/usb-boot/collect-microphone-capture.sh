#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Collect attended microphone capture, privacy, and restart evidence.

set -euo pipefail

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: collect-microphone-capture.sh \
  --client PATH --output-dir PATH [options]

Options:
  --adb-serial SERIAL       ADB serial (default: 0123456789ABCDEF)
  --adb-server-port PORT    ADB server port (default: 5037)
  --state-timeout SECONDS   Wait for each Mic-Mute transition (default: 120)
  --client PATH             ARMv7 reInvoke microphone test client
  --output-dir PATH         New evidence directory
  --help                    Show this help

The unit must start unmuted. When prompted, tap Mic-Mute once to mute and once
to unmute. The script then restarts the DSP service automatically.
EOF
  exit "${exit_code}"
}

err() {
  printf "ERROR: %s\n" "$1" >&2
  exit 1
}

remote() {
  adb -P "${ADB_SERVER_PORT}" -s "${ADB_SERIAL}" shell "$@"
}

remote_status() {
  local output_path="$1"
  local command="$2"
  local marker
  local response
  local status_line
  local status

  marker="__REINVOKE_REMOTE_STATUS_${BASHPID}_${RANDOM}__"
  response="$(
    remote "
      (
        ${command}
      )
      status=\$?
      /bin/busybox echo '${marker}'\${status}
    "
  )" || {
    printf "%s\n" "${response:-}" >"${output_path}"
    return 255
  }
  status_line="$(
    printf "%s\n" "${response}" |
      awk -v marker="${marker}" 'index($0, marker) == 1 { value=$0 } END { print value }'
  )"
  status_line="${status_line%$'\r'}"
  [[ "${status_line}" =~ ^${marker}([0-9]+)$ ]] || {
    printf "%s\n" "${response}" >"${output_path}"
    return 255
  }
  status="${BASH_REMATCH[1]}"
  printf "%s\n" "${response}" |
    awk -v marker="${marker}" 'index($0, marker) != 1' >"${output_path}"
  ((status <= 255)) || return 255
  return "${status}"
}

remote_line() {
  remote "$1" |
    tr -d '\r' |
    awk '$0 != "/"' |
    tail -1
}

remote_size() {
  remote_line "/bin/busybox stat -c %s '$1' 2>/dev/null || echo 0"
}

wait_for_state() {
  local expected="$1"
  local timeout_seconds="$2"
  local elapsed=0

  while ((elapsed < timeout_seconds)); do
    if [[ "$(remote_line \
      '/bin/busybox cat /run/reinvoke/microphone-state 2>/dev/null')" \
      == "${expected}" ]]; then
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  return 1
}

wait_for_confirmation() {
  local expected="$1"
  local start_line="$2"
  local timeout_seconds="$3"
  local elapsed=0

  while ((elapsed < timeout_seconds)); do
    if [[ "$(remote_line "
      /bin/busybox tail -n +${start_line} \
        /run/reinvoke/logs/runtime.log |
      /bin/busybox grep -q \
        'confirmed DSP microphone muted=${expected}' &&
      /bin/busybox echo MATCH
    ")" == "MATCH" ]]; then
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  return 1
}

wait_for_size_change() {
  local path="$1"
  local initial="$2"
  local timeout_seconds="$3"
  local elapsed=0
  local current

  while ((elapsed < timeout_seconds)); do
    current="$(remote_size "${path}")"
    [[ "${current}" =~ ^[0-9]+$ ]] || return 1
    ((current > initial)) && return 0
    sleep 1
    elapsed=$((elapsed + 1))
  done
  return 1
}

main() {
  local ADB_SERIAL="0123456789ABCDEF"
  local ADB_SERVER_PORT=5037
  local state_timeout=120
  local client=""
  local output_dir=""
  local remote_client="/tmp/reinvoke-mic-capture-client"
  local remote_raw="/tmp/reinvoke-mic-acceptance.s32le"
  local remote_events="/tmp/reinvoke-mic-acceptance.jsonl"
  local remote_errors="/tmp/reinvoke-mic-acceptance.err"
  local remote_pid="/tmp/reinvoke-mic-acceptance.pid"
  local initial_state
  local client_pid
  local baseline_size
  local muted_size
  local muted_size_after
  local resumed_size
  local final_size
  local generation_count
  local generation_count_after
  local old_dsp_pid
  local old_dsp_start
  local new_dsp_pid=""
  local new_dsp_start=""
  local log_start
  local direct_status=0
  local direct_size=0
  local cleanup_needed=yes

  while (( $# > 0 )); do
    case "$1" in
      --adb-serial)
        [[ -n "${2:-}" ]] || err "--adb-serial requires a value"
        ADB_SERIAL="$2"
        shift 2
        ;;
      --adb-server-port)
        [[ "${2:-}" =~ ^[1-9][0-9]*$ ]] ||
          err "--adb-server-port requires a positive integer"
        ADB_SERVER_PORT="$2"
        shift 2
        ;;
      --state-timeout)
        [[ "${2:-}" =~ ^[1-9][0-9]*$ ]] ||
          err "--state-timeout requires a positive integer"
        state_timeout="$2"
        shift 2
        ;;
      --client)
        [[ -n "${2:-}" ]] || err "--client requires a path"
        client="$2"
        shift 2
        ;;
      --output-dir)
        [[ -n "${2:-}" ]] || err "--output-dir requires a path"
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

  [[ -x "${client}" ]] || err "--client must be an executable ARM binary"
  [[ -n "${output_dir}" ]] || err "--output-dir is required"
  [[ ! -e "${output_dir}" ]] || err "output directory already exists"
  mkdir -p "${output_dir}"
  export ADB_SERIAL ADB_SERVER_PORT
  adb -P "${ADB_SERVER_PORT}" -s "${ADB_SERIAL}" wait-for-device

  initial_state="$(
    remote_line '/bin/busybox cat /run/reinvoke/microphone-state 2>/dev/null'
  )"
  [[ "${initial_state}" == "unmuted" ]] ||
    err "microphone must start unmuted; current state is ${initial_state:-unknown}"
  remote '
    BB=/bin/busybox
    ready=yes
    $BB test -S /run/reinvoke/mic-capture/audio.sock || ready=no
    $BB test -s /run/reinvoke/mic-capture.pid || ready=no
    $BB test -s /run/reinvoke/mcu-interface.pid || ready=no
    $BB test -s /run/reinvoke/dsp-interface.pid || ready=no
    echo "ready=$ready"
  ' >"${output_dir}/readiness.txt"
  grep -q '^ready=yes$' "${output_dir}/readiness.txt" ||
    err "microphone capture owner is not ready"

  adb -P "${ADB_SERVER_PORT}" -s "${ADB_SERIAL}" push \
    "${client}" "${remote_client}" >/dev/null
  remote "
    /bin/busybox chmod 0700 '${remote_client}'
    /bin/busybox rm -f \
      '${remote_raw}' '${remote_events}' '${remote_errors}' '${remote_pid}'
    /bin/busybox setsid '${remote_client}' \
      -duration 600s -reconnect -output '${remote_raw}' \
      >'${remote_events}' 2>'${remote_errors}' </dev/null &
    echo \$! >'${remote_pid}'
  "
  client_pid="$(remote_line "/bin/busybox cat '${remote_pid}'")"
  [[ "${client_pid}" =~ ^[1-9][0-9]*$ ]] ||
    err "test client did not start"
  trap '
    if [[ "${cleanup_needed}" == "yes" ]]; then
      remote "
        pid=${client_pid}
        if [ \"\$(/bin/busybox readlink /proc/\${pid}/exe 2>/dev/null)\" = \
          \"${remote_client}\" ]; then
          /bin/busybox kill \${pid} 2>/dev/null || true
        fi
      " >/dev/null
    fi
  ' EXIT

  printf "READY: speak or tap near the microphone for three seconds.\n"
  wait_for_size_change "${remote_raw}" 0 15 ||
    err "unmuted capture did not produce audio"
  sleep 3
  baseline_size="$(remote_size "${remote_raw}")"
  printf "%s\n" "${baseline_size}" >"${output_dir}/size-before-mute"
  log_start="$(
    remote_line '/bin/busybox wc -l < /run/reinvoke/logs/runtime.log'
  )"

  printf "READY: tap Mic-Mute once now; wait for the red privacy indication.\n"
  wait_for_state muted "${state_timeout}" ||
    err "physical Mic-Mute did not reach muted state"
  wait_for_confirmation true "${log_start}" "${state_timeout}" ||
    err "DSP did not confirm physical microphone mute"
  muted_size="$(remote_size "${remote_raw}")"
  sleep 5
  muted_size_after="$(remote_size "${remote_raw}")"
  printf "%s\n%s\n" "${muted_size}" "${muted_size_after}" \
    >"${output_dir}/size-during-mute"
  [[ "${muted_size_after}" == "${muted_size}" ]] ||
    err "consumer received bytes while muted"

  if remote_status "${output_dir}/direct-open.txt" '
    /bin/busybox rm -f /tmp/direct-capture.raw
    /bin/busybox timeout -t 5 \
      /opt/reinvoke/lib/ld-linux-armhf.so.3 \
      --library-path /opt/reinvoke/lib \
      /opt/reinvoke/bin/arecord \
      -D hw:1,0 -t raw -f S32_LE -r 48000 -c 2 \
      --period-size=256 --buffer-size=4096 -d 1 /tmp/direct-capture.raw
  '; then
    direct_status=0
  else
    direct_status=$?
  fi
  printf "%s\n" "${direct_status}" >"${output_dir}/direct-open.status"
  [[ "${direct_status}" == "1" ]] ||
    err "direct PCM probe returned ${direct_status}, not the expected busy error"
  grep -qiE 'busy|resource unavailable' "${output_dir}/direct-open.txt" ||
    err "direct PCM probe did not report the expected busy error"
  direct_size="$(
    remote_size /tmp/direct-capture.raw
  )"
  [[ "${direct_size}" == "0" ]] ||
    err "direct PCM probe unexpectedly captured ${direct_size} bytes"

  log_start="$(
    remote_line '/bin/busybox wc -l < /run/reinvoke/logs/runtime.log'
  )"
  printf "READY: tap Mic-Mute once now to clear privacy.\n"
  wait_for_state unmuted "${state_timeout}" ||
    err "physical Mic-Mute did not return to unmuted state"
  wait_for_confirmation false "${log_start}" "${state_timeout}" ||
    err "DSP did not confirm physical microphone unmute"
  printf "READY: speak or tap near the microphone for three seconds.\n"
  wait_for_size_change "${remote_raw}" "${muted_size_after}" 20 ||
    err "capture did not resume after physical unmute"
  sleep 3
  resumed_size="$(remote_size "${remote_raw}")"
  printf "%s\n" "${resumed_size}" >"${output_dir}/size-after-unmute"

  generation_count="$(
    remote_line "
      /bin/busybox grep -c '\"type\":\"generation\"' \
        '${remote_events}' 2>/dev/null || /bin/busybox true
    "
  )"
  old_dsp_pid="$(
    remote_line '/bin/busybox cat /run/reinvoke/dsp-interface.pid'
  )"
  old_dsp_start="$(
    remote_line "
      /bin/busybox awk '{print \$22}' /proc/${old_dsp_pid}/stat 2>/dev/null
    "
  )"
  [[ "${old_dsp_pid}" =~ ^[1-9][0-9]*$ ]] ||
    err "DSP PID is invalid"
  [[ "${old_dsp_start}" =~ ^[1-9][0-9]*$ ]] ||
    err "DSP process start time is invalid"
  remote_status "${output_dir}/dsp-kill.txt" "
    pid='${old_dsp_pid}'
    expected_start='${old_dsp_start}'
    [ \"\$(/bin/busybox readlink /proc/\${pid}/exe 2>/dev/null)\" = \
      /opt/reinvoke/bin/reinvoke-dsp-interface ] &&
    [ \"\$(/bin/busybox awk '{print \$22}' /proc/\${pid}/stat 2>/dev/null)\" = \
      \"\${expected_start}\" ] &&
    /bin/busybox kill \${pid}
  " || err "DSP process identity changed before restart request"
  for _ in $(seq 1 45); do
    new_dsp_pid="$(
      remote_line \
        '/bin/busybox cat /run/reinvoke/dsp-interface.pid 2>/dev/null'
    )"
    if [[ "${new_dsp_pid}" =~ ^[1-9][0-9]*$ ]]; then
      new_dsp_start="$(
        remote_line "
          /bin/busybox awk '{print \$22}' \
            /proc/${new_dsp_pid}/stat 2>/dev/null
        "
      )"
    fi
    if [[ "${new_dsp_start}" =~ ^[1-9][0-9]*$ &&
          ("${new_dsp_pid}" != "${old_dsp_pid}" ||
           "${new_dsp_start}" != "${old_dsp_start}") ]]; then
      break
    fi
    sleep 1
  done
  [[ "${new_dsp_start}" =~ ^[1-9][0-9]*$ &&
     ("${new_dsp_pid}" != "${old_dsp_pid}" ||
      "${new_dsp_start}" != "${old_dsp_start}") ]] ||
    err "DSP service did not restart"
  wait_for_size_change "${remote_raw}" "${resumed_size}" 45 ||
    err "capture did not resume after DSP restart"
  final_size="$(remote_size "${remote_raw}")"
  generation_count_after="$(
    remote_line "
      /bin/busybox grep -c '\"type\":\"generation\"' \
        '${remote_events}' 2>/dev/null || /bin/busybox true
    "
  )"
  ((generation_count_after > generation_count)) ||
    err "DSP restart did not create a new delivery generation"

  remote "
    pid='${client_pid}'
    if [ \"\$(/bin/busybox readlink /proc/\${pid}/exe 2>/dev/null)\" = \
      '${remote_client}' ]; then
      /bin/busybox kill \${pid} 2>/dev/null || true
    fi
  "
  cleanup_needed=no
  trap - EXIT
  sleep 2
  adb -P "${ADB_SERVER_PORT}" -s "${ADB_SERIAL}" pull \
    "${remote_events}" "${output_dir}/client.jsonl" >/dev/null
  adb -P "${ADB_SERVER_PORT}" -s "${ADB_SERIAL}" pull \
    "${remote_errors}" "${output_dir}/client.stderr" >/dev/null
  adb -P "${ADB_SERVER_PORT}" -s "${ADB_SERIAL}" pull \
    "${remote_raw}" "${output_dir}/microphone.s32le" >/dev/null
  python3 - \
    "${output_dir}/microphone.s32le" \
    "${baseline_size}" \
    "${muted_size_after}" \
    "${resumed_size}" >"${output_dir}/sample-analysis.txt" <<'PY'
import array
import sys

path, baseline_text, resumed_start_text, resumed_end_text = sys.argv[1:]
content = open(path, "rb").read()
if sys.byteorder != "little" or array.array("i").itemsize != 4:
    raise SystemExit("sample analysis requires a little-endian 32-bit int host")


def analyze(name, start, end):
    segment = content[start:end]
    samples = array.array("i")
    samples.frombytes(segment[: len(segment) // 4 * 4])
    nonzero = sum(value != 0 for value in samples)
    peak = max((abs(value) for value in samples), default=0)
    print(f"{name}_samples={len(samples)}")
    print(f"{name}_nonzero={nonzero}")
    print(f"{name}_peak={peak}")
    if not samples or not nonzero:
        raise SystemExit(f"{name} did not contain positive audio")


analyze("before_mute", 0, int(baseline_text))
analyze("after_unmute", int(resumed_start_text), int(resumed_end_text))
PY
  remote "
    echo 'initial_state=${initial_state}'
    echo 'baseline_size=${baseline_size}'
    echo 'muted_size=${muted_size}'
    echo 'muted_size_after=${muted_size_after}'
    echo 'resumed_size=${resumed_size}'
    echo 'final_size=${final_size}'
    echo 'old_dsp_pid=${old_dsp_pid}'
    echo 'old_dsp_start=${old_dsp_start}'
    echo 'new_dsp_pid=${new_dsp_pid}'
    echo 'new_dsp_start=${new_dsp_start}'
    echo 'generations_before_restart=${generation_count}'
    echo 'generations_after_restart=${generation_count_after}'
    echo 'final_state='\$(/bin/busybox cat /run/reinvoke/microphone-state)
    echo 'nand_mounts='\$(/bin/busybox mount |
      /bin/busybox grep -ciE 'mtd|ubi|nand')
  " >"${output_dir}/SUMMARY"
  (
    cd "${output_dir}"
    find . -type f ! -name SHA256SUMS -print0 |
      sort -z |
      xargs -0 sha256sum >SHA256SUMS
  )
  cat "${output_dir}/SUMMARY"
  printf "Microphone capture evidence: %s\n" "${output_dir}"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
