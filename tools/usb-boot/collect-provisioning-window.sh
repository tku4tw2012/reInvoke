#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Collect one physical Mic-Mute-long provisioning-window evidence bundle.

set -euo pipefail

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: collect-provisioning-window.sh --output-dir PATH [options]

Options:
  --adb-serial SERIAL       ADB serial (default: 0123456789ABCDEF)
  --adb-server-port PORT    ADB server port (default: 5037)
  --start-timeout SECONDS   Wait for physical long press (default: 90)
  --cleanup-timeout SECONDS Wait for bounded window cleanup (default: 360)
  --output-dir PATH         New evidence directory
  --help                    Show this help

Start this after a sta-uAP boot, then hold Mic-Mute until the long-press event.
The collector records active isolation and waits for the five-minute window to
clean itself up. It never reads AP credentials or submits station credentials.
EOF
  exit "${exit_code}"
}

err() {
  printf "ERROR: %s\n" "$1" >&2
  exit 1
}

remote() {
  adb -P "${ADB_SERVER_PORT}" -s "${ADB_SERIAL}" shell "$1" |
    tr -d '\r' |
    grep -v '^/$'
}

snapshot() {
  local phase="$1"
  local output_dir="$2"

  remote '
    BB=/bin/busybox
    echo "=== cmdline ==="
    $BB cat /proc/cmdline
    echo "=== interfaces ==="
    $BB ifconfig p2p0 2>&1 || true
    echo "=== forwarding ==="
    $BB cat /proc/sys/net/ipv4/ip_forward
    $BB cat /proc/sys/net/ipv6/conf/all/forwarding
    echo "=== processes ==="
    $BB ps -o pid,ppid,args |
      $BB grep -E "reinvoke-provision|reinvoke-wifi-applyd|hostapd|udhcpd" |
      $BB grep -v grep || true
    echo "=== runtime files ==="
    $BB find /run/reinvoke/provision-window -maxdepth 3 -print 2>/dev/null ||
      true
    echo "=== sockets ==="
    $BB netstat -ltn 2>/dev/null |
      $BB grep -E ":8080|:8443" || true
    echo "=== storage ==="
    $BB mount | $BB grep -Ei "mtd|ubi|yaffs|nand" || true
    echo "=== wamp firewall ==="
    /opt/reinvoke/lib/ld-linux-armhf.so.3 \
      --library-path /opt/reinvoke/lib \
      /opt/reinvoke/bin/iptables -S INPUT 2>/dev/null || true
  ' >"${output_dir}/${phase}.txt"
}

# Prints one "=== name ===" section of a snapshot file.
snapshot_section() {
  local file="$1"
  local name="$2"

  awk -v want="=== ${name} ===" '
    $0 == want { capture = 1; next }
    /^=== / { capture = 0 }
    capture { print }
  ' "${file}" 2>/dev/null
}

# The provisioning window must never widen the WAMP control-plane firewall.
# Access-point clients live in 192.168.43.0/24, which no ACCEPT rule covers, so
# each WAMP port must keep an unconditional DROP behind the loopback and
# operator ACCEPT rules.
wamp_firewall_isolated() {
  local file="$1"
  local rules
  local port

  rules="$(snapshot_section "${file}" "wamp firewall")"
  [[ -n "${rules}" ]] || return 1
  for port in 9998 9999; do
    printf "%s\n" "${rules}" |
      grep -qE -- "-A INPUT -p tcp -m tcp --dport ${port} -j DROP$" ||
      return 1
    printf "%s\n" "${rules}" |
      grep -E -- "--dport ${port} -j ACCEPT$" |
      grep -qvE -- "(-i lo|-s 192\.168\.4\.27/32) " &&
      return 1
  done
  return 0
}

wamp_firewall_unchanged() {
  local baseline="$1"
  local other="$2"

  diff <(snapshot_section "${baseline}" "wamp firewall") \
    <(snapshot_section "${other}" "wamp firewall") >/dev/null
}

wait_for_token() {
  local command="$1"
  local token="$2"
  local timeout="$3"
  local elapsed=0

  while ((elapsed < timeout)); do
    if remote "${command}" 2>/dev/null | grep -qF "${token}"; then
      return 0
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  return 1
}

forwarding_disabled() {
  local path="$1"

  awk '
    /=== forwarding ===/ {
      getline ipv4
      getline ipv6
      found = 1
    }
    END {
      exit !(found && ipv4 == "0" && ipv6 == "0")
    }
  ' "${path}"
}

main() {
  local output_dir=""
  local start_timeout=90
  local cleanup_timeout=360
  local log_start
  local status=0

  ADB_SERIAL="0123456789ABCDEF"
  ADB_SERVER_PORT=5037

  while (($# > 0)); do
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
      --start-timeout)
        [[ "${2:-}" =~ ^[1-9][0-9]*$ ]] ||
          err "--start-timeout requires a positive integer"
        start_timeout="$2"
        shift 2
        ;;
      --cleanup-timeout)
        [[ "${2:-}" =~ ^[1-9][0-9]*$ ]] ||
          err "--cleanup-timeout requires a positive integer"
        cleanup_timeout="$2"
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
  command -v adb >/dev/null || err "'adb' is required"
  mkdir -p "${output_dir}"

  adb -P "${ADB_SERVER_PORT}" -s "${ADB_SERIAL}" wait-for-device
  log_start="$(
    remote '/bin/busybox wc -l < /run/reinvoke/logs/runtime.log' |
      tr -d ' '
  )"
  [[ "${log_start}" =~ ^[0-9]+$ ]] || err "cannot determine runtime log offset"
  printf "%s\n" "${log_start}" >"${output_dir}/log-start-line"

  snapshot baseline "${output_dir}"
  remote '
    BB=/bin/busybox
    ready=yes
    $BB grep -q "reinvoke.wifi_mode=sta-uap" /proc/cmdline || ready=no
    $BB test -d /sys/class/net/p2p0 || ready=no
    $BB test -S /run/reinvoke/provision-window.sock || ready=no
    $BB test -s /run/reinvoke/provision-windowd.pid || ready=no
    echo "ready=$ready"
  ' >"${output_dir}/readiness.txt"
  grep -q '^ready=yes$' "${output_dir}/readiness.txt" ||
    err "sta-uAP provisioning control is not ready"

  printf "READY: hold Mic-Mute until the long-press event now.\n"
  if ! wait_for_token \
    '/bin/busybox test -s /run/reinvoke/provision-window/bootstrap/provisioning.json && echo ACTIVE' \
    ACTIVE \
    "${start_timeout}"; then
    status=1
  else
    snapshot active "${output_dir}"
    remote '
      /bin/busybox cat \
        /run/reinvoke/provision-window/bootstrap/provisioning.json 2>/dev/null ||
        true
    ' >"${output_dir}/descriptor.json"
    if ! wait_for_token \
      '/bin/busybox test ! -e /run/reinvoke/provision-window && echo CLEAN' \
      CLEAN \
      "${cleanup_timeout}"; then
      status=1
    fi
  fi

  snapshot final "${output_dir}"
  remote "
    /bin/busybox tail -n +${log_start} /run/reinvoke/logs/runtime.log
  " >"${output_dir}/runtime-window.log"

  {
    grep -q 'micmute-long' "${output_dir}/runtime-window.log" &&
      echo "PASS physical.micmute_long" ||
      echo "FAIL physical.micmute_long"
    grep -q 'state=ENABLED' "${output_dir}/runtime-window.log" &&
      echo "PASS access_point.enabled" ||
      echo "INFO access_point.enabled_not_logged"
    forwarding_disabled "${output_dir}/active.txt" 2>/dev/null &&
      echo "PASS isolation.forwarding_off" ||
      echo "FAIL isolation.forwarding_off"
    wamp_firewall_isolated "${output_dir}/active.txt" &&
      echo "PASS isolation.wamp_blocked_from_ap" ||
      echo "FAIL isolation.wamp_blocked_from_ap"
    wamp_firewall_unchanged \
      "${output_dir}/baseline.txt" "${output_dir}/active.txt" &&
      echo "PASS isolation.wamp_rules_unchanged" ||
      echo "FAIL isolation.wamp_rules_unchanged"
    grep -q 'inet addr:192.168.43.1' "${output_dir}/active.txt" \
      2>/dev/null &&
      echo "PASS access_point.address" ||
      echo "FAIL access_point.address"
    grep -q ':8443' "${output_dir}/active.txt" 2>/dev/null &&
      echo "PASS access_point.https_listener" ||
      echo "FAIL access_point.https_listener"
    grep -qE \
      'reinvoke-wifi-applyd|/reinvoke-provisiond|/hostapd|udhcpd' \
      "${output_dir}/active.txt" 2>/dev/null &&
      echo "PASS access_point.children_running" ||
      echo "FAIL access_point.children_running"
    [[ -s "${output_dir}/descriptor.json" ]] &&
      echo "PASS access_point.descriptor" ||
      echo "FAIL access_point.descriptor"
    grep -q '=== storage ===' "${output_dir}/active.txt" 2>/dev/null &&
      [[ -z "$(snapshot_section "${output_dir}/active.txt" storage)" ]] &&
      echo "PASS storage.no_nand_mount" ||
      echo "FAIL storage.no_nand_mount"
    grep -q '/run/reinvoke/provision-window' "${output_dir}/final.txt" &&
      echo "FAIL cleanup.runtime_removed" ||
      echo "PASS cleanup.runtime_removed"
    grep -qE \
      'reinvoke-wifi-applyd|/reinvoke-provisiond|/hostapd|udhcpd' \
      "${output_dir}/final.txt" 2>/dev/null &&
      echo "FAIL cleanup.children_stopped" ||
      echo "PASS cleanup.children_stopped"
    grep -q 'inet addr:192.168.43.1' "${output_dir}/final.txt" 2>/dev/null &&
      echo "FAIL cleanup.address_removed" ||
      echo "PASS cleanup.address_removed"
    forwarding_disabled "${output_dir}/final.txt" 2>/dev/null &&
      echo "PASS cleanup.forwarding_off" ||
      echo "FAIL cleanup.forwarding_off"
    wamp_firewall_unchanged \
      "${output_dir}/baseline.txt" "${output_dir}/final.txt" &&
      echo "PASS cleanup.wamp_rules_restored" ||
      echo "FAIL cleanup.wamp_rules_restored"
  } >"${output_dir}/SUMMARY"

  # A check that never prints would otherwise pass silently, so require the
  # full expected set to be present before trusting the result.
  local required_check
  for required_check in \
    physical.micmute_long \
    isolation.forwarding_off \
    isolation.wamp_blocked_from_ap \
    isolation.wamp_rules_unchanged \
    access_point.address \
    access_point.https_listener \
    access_point.children_running \
    access_point.descriptor \
    storage.no_nand_mount \
    cleanup.runtime_removed \
    cleanup.children_stopped \
    cleanup.address_removed \
    cleanup.forwarding_off \
    cleanup.wamp_rules_restored; do
    grep -qE "^(PASS|FAIL) ${required_check}$" "${output_dir}/SUMMARY" || {
      printf "FAIL missing.%s\n" "${required_check}" >>"${output_dir}/SUMMARY"
    }
  done

  if grep -q '^FAIL ' "${output_dir}/SUMMARY"; then
    status=1
  fi
  (
    cd "${output_dir}"
    find . -type f ! -name SHA256SUMS -print0 |
      sort -z |
      xargs -0 sha256sum >SHA256SUMS
  )
  cat "${output_dir}/SUMMARY"
  ((status == 0)) || err "provisioning-window acceptance failed"
  printf "Provisioning evidence: %s\n" "${output_dir}"
}

main "$@"
