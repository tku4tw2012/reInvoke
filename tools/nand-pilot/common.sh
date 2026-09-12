#!/bin/busybox sh
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT

BB=${BB:-/bin/busybox}
PILOT_STATE=${PILOT_STATE:-/run/nand-pilot}

pilot_log() {
  printf 'reInvoke NAND pilot: %s\n' "$*" >/dev/kmsg 2>/dev/null || true
  printf 'reInvoke NAND pilot: %s\n' "$*" >/dev/console 2>/dev/null || true
  if ${BB} test -d "${PILOT_STATE}"; then
    if ${BB} test -f "${PILOT_STATE}/boot.log" &&
       ${BB} test "$(${BB} stat -c %s "${PILOT_STATE}/boot.log")" -gt 32768; then
      ${BB} mv "${PILOT_STATE}/boot.log" "${PILOT_STATE}/boot.log.1"
    fi
    printf '%s %s\n' "$(${BB} cut -d' ' -f1 /proc/uptime)" "$*" \
      >>"${PILOT_STATE}/boot.log"
  fi
}

pilot_phase() {
  printf '%s\n' "$1" >"${PILOT_STATE}/phase"
  pilot_log "phase=$1"
}

pilot_failure() {
  # One bounded record per named subsystem; callers use fixed subsystem names.
  printf '%.1024s\n' "$2" >"${PILOT_STATE}/failure-$1"
  pilot_log "FAIL $1: $2"
}

pilot_check_pty_node() {
  ${BB} test -c "$1" &&
    ${BB} test "$(${BB} stat -L -c '%t:%T:%a' "$1" 2>/dev/null)" = "5:2:666"
}

configure_adb() {
  gadget="${PILOT_ADBD_GADGET:-/sys/class/android_usb/android0}"
  ${BB} test -d "${gadget}" || return 1
  echo 0 >"${gadget}/enable" || return 1
  usb_functions=adb
  if ${BB} test -d "${gadget}/f_acm"; then
    usb_functions=acm,adb
    echo 1 >"${gadget}/f_acm/instances" || return 1
  fi
  echo 0d02 >"${gadget}/idProduct" &&
    echo "$1" >"${gadget}/iProduct" &&
    echo "${usb_functions}" >"${gadget}/functions" &&
    echo 1 >"${gadget}/enable" || return 1
  ${BB} test "$(${BB} cat "${gadget}/enable")" = 1 &&
    ${BB} test "$(${BB} cat "${gadget}/iProduct")" = "$1"
}

pilot_adbd_run() {
  if [ "$1" = runtime ]; then
    exec ${BB} chroot "${PILOT_ADBD_RUNTIME_ROOT}" /sbin/adbd-root
  fi
  exec /sbin/adbd-root
}

adb_loop() {
  while ! ${BB} test -e "${PILOT_STATE}/stop-adb"; do
    adb_root=early
    if [ -n "${PILOT_ADBD_RUNTIME_ROOT:-}" ] &&
       ${BB} test -f "${PILOT_STATE}/runtime-ready"; then
      adb_root=runtime
    fi
    pilot_adbd_run "${adb_root}" >"${PILOT_ADBD_LOG:-/dev/kmsg}" 2>&1 &
    adb_pid=$!
    echo "${adb_pid}" >"${PILOT_STATE}/adbd.pid"
    echo "${adb_root}" >"${PILOT_STATE}/adbd-root"
    while ${BB} kill -0 "${adb_pid}" 2>/dev/null; do
      if ${BB} test -e "${PILOT_STATE}/stop-adb" ||
         { [ "${adb_root}" = early ] &&
           [ -n "${PILOT_ADBD_RUNTIME_ROOT:-}" ] &&
           ${BB} test -f "${PILOT_STATE}/runtime-ready"; }; then
        ${BB} kill "${adb_pid}" ||
          pilot_failure adb "daemon exited while switching diagnostic roots"
        break
      fi
      ${BB} sleep 1
    done
    wait "${adb_pid}"
    echo "adbd exited $?" >"${PILOT_STATE}/adbd-exit"
    ${BB} rm -f "${PILOT_STATE}/adbd.pid"
    ${BB} sleep 1
  done
}

pilot_node_from_sysfs() {
  dev_file="$1"
  node_path="$2"
  ${BB} test -r "${dev_file}" || return 1
  node_dev="$(${BB} cat "${dev_file}")" || return 1
  node_major="${node_dev%:*}"
  node_minor="${node_dev#*:}"
  ${BB} test -c "${node_path}" ||
    ${BB} mknod -m 0600 "${node_path}" c "${node_major}" "${node_minor}"
}

pilot_usb_adbd_launch() {
  : "${PILOT_ADBD_PRODUCT:?}"
  : "${PILOT_ADBD_STARTED_PHASE:=early-adb-started}"
  : "${PILOT_ADBD_DEGRADED_PHASE:=early-adb-degraded}"
  : "${PILOT_ADBD_GADGET:=/sys/class/android_usb/android0}"
  : "${PILOT_ADBD_PTMX:=/dev/ptmx}"
  : "${PILOT_ADBD_NODE:=/dev/android_adb}"
  : "${PILOT_ADBD_DEV_FILE:=/sys/class/misc/android_adb/dev}"

  if [ "${PILOT_ADBD_PTY_READY:-1}" != 1 ] ||
     ! pilot_check_pty_node "${PILOT_ADBD_PTMX}"; then
    pilot_failure pty "early ADB requires /dev/ptmx character device 5:2 mode 0666"
    pilot_phase "${PILOT_ADBD_DEGRADED_PHASE}"
    return 1
  fi

  if ! ${BB} test -d "${PILOT_ADBD_GADGET}"; then
    pilot_failure usb-gadget "android_usb gadget absent for actual kernel; continuing without early adbd"
    pilot_phase "${PILOT_ADBD_DEGRADED_PHASE}"
    return 1
  fi

  configure_adb "${PILOT_ADBD_PRODUCT}" || {
    pilot_failure usb-gadget "USB gadget configuration failed"
    pilot_phase "${PILOT_ADBD_DEGRADED_PHASE}"
    return 1
  }

  pilot_node_from_sysfs "${PILOT_ADBD_DEV_FILE}" "${PILOT_ADBD_NODE}" || {
    pilot_failure android-adb-node "kernel has no legacy /dev/android_adb ABI required by retained adbd"
    pilot_phase "${PILOT_ADBD_DEGRADED_PHASE}"
    return 1
  }

  if ${BB} test -n "${PILOT_ADBD_ENABLE_DEV_FILE:-}" &&
     ${BB} test -n "${PILOT_ADBD_ENABLE_NODE:-}"; then
    pilot_node_from_sysfs "${PILOT_ADBD_ENABLE_DEV_FILE}" "${PILOT_ADBD_ENABLE_NODE}" ||
      pilot_failure android-adb-enable "optional android_adb_enable misc node missing"
  fi
  if ${BB} test -n "${PILOT_ADBD_TTYGS0_DEV_FILE:-}" &&
     ${BB} test -n "${PILOT_ADBD_TTYGS0_NODE:-}"; then
    pilot_node_from_sysfs "${PILOT_ADBD_TTYGS0_DEV_FILE}" "${PILOT_ADBD_TTYGS0_NODE}" ||
      pilot_failure ttyGS0 "optional ttyGS0 misc node missing"
  fi

  adb_loop &
  echo "$!" >"${PILOT_STATE}/adbd-supervisor.pid"
  adb_wait=0
  while ! ${BB} test -s "${PILOT_STATE}/adbd.pid" && [ "${adb_wait}" -lt 5 ]; do
    ${BB} sleep 1
    adb_wait=$((adb_wait + 1))
  done
  ${BB} sleep 1
  if ${BB} test -s "${PILOT_STATE}/adbd.pid" &&
    ${BB} kill -0 "$(${BB} cat "${PILOT_STATE}/adbd.pid")" 2>/dev/null; then
    pilot_phase "${PILOT_ADBD_STARTED_PHASE}"
    return 0
  fi
  pilot_failure adb "early adbd did not remain alive; runtime continuing"
  pilot_phase "${PILOT_ADBD_DEGRADED_PHASE}"
  return 1
}

pilot_verify_payload() {
  ${BB} test "$(${BB} stat -c %s "$1")" = "$3" || return 1
  printf '%s  %s\n' "$2" "$1" | ${BB} sha256sum -c >/dev/null 2>&1
}

pilot_extract_payload() {
  ${BB} gzip -t "$1" || return 1
  (
    set -o pipefail
    cd "$2" || exit 1
    ${BB} gzip -dc "$1" | ${BB} cpio -idm
  )
}

pilot_check_writable() {
  for writable in "$@"; do
    ${BB} mkdir -p "${writable}" &&
      ${BB} touch "${writable}/.nand-pilot-write-check" &&
      ${BB} rm "${writable}/.nand-pilot-write-check" || return 1
  done
}

pilot_fatal() {
  pilot_failure bootstrap "$*"
  pilot_phase failed
  while true; do
    pilot_log "bootstrap stopped: $*; diagnostics remain available if USB initialized"
    ${BB} sleep 30
  done
}
