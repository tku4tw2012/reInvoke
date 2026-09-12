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
  usb_functions=adb
  if ${BB} test -d "${gadget}/f_acm"; then
    usb_functions=acm,adb
  fi
  if [ "$(${BB} cat "${gadget}/enable" 2>/dev/null)" = 1 ] &&
     [ "$(${BB} cat "${gadget}/functions" 2>/dev/null)" = "${usb_functions}" ] &&
     [ "$(${BB} cat "${gadget}/iProduct" 2>/dev/null)" = "$1" ] &&
     [ "$(${BB} cat "${gadget}/idProduct" 2>/dev/null)" = 0d02 ]; then
    return 0
  fi
  echo 0 >"${gadget}/enable" || return 1
  if [ "${usb_functions}" = acm,adb ]; then
    echo 1 >"${gadget}/f_acm/instances" || return 1
  fi
  echo 0d02 >"${gadget}/idProduct" &&
    echo "$1" >"${gadget}/iProduct" &&
    echo "${usb_functions}" >"${gadget}/functions" &&
    echo 1 >"${gadget}/enable" || return 1
  ${BB} test "$(${BB} cat "${gadget}/enable")" = 1 &&
    ${BB} test "$(${BB} cat "${gadget}/iProduct")" = "$1" &&
    ${BB} test "$(${BB} cat "${gadget}/functions")" = "${usb_functions}"
}

pilot_adbd_run() {
  if [ "$1" = runtime ]; then
    exec ${BB} chroot "${PILOT_ADBD_RUNTIME_ROOT}" /sbin/adbd-root
  fi
  exec /sbin/adbd-root
}

adb_loop() {
  adb_pid=""
  adb_attempt=0
  trap '
    if [ -n "${adb_pid}" ]; then
      ${BB} kill "${adb_pid}" 2>/dev/null || true
      wait "${adb_pid}" 2>/dev/null || true
    fi
    ${BB} rm -f "${PILOT_STATE}/adbd.pid" "${PILOT_STATE}/adbd-supervisor.pid"
    ${BB} rmdir "${PILOT_STATE}/adb-owner" 2>/dev/null || true
  ' EXIT
  trap 'exit 0' TERM INT
  while ! ${BB} test -e "${PILOT_STATE}/stop-adb"; do
    if ! pilot_usb_prerequisites; then
      adb_attempt=$((adb_attempt + 1))
      [ "${adb_attempt}" -lt 30 ] || break
      ${BB} sleep 1
      continue
    fi
    adb_root=early
    if [ -n "${PILOT_ADBD_RUNTIME_ROOT:-}" ] &&
       ${BB} test -f "${PILOT_STATE}/runtime-ready"; then
      adb_root=runtime
    fi
    pilot_adbd_run "${adb_root}" >"${PILOT_ADBD_LOG:-/dev/kmsg}" 2>&1 &
    adb_pid=$!
    echo "${adb_pid}" >"${PILOT_STATE}/adbd.pid"
    echo "${adb_root}" >"${PILOT_STATE}/adbd-root"
    adb_unready=0
    while ${BB} kill -0 "${adb_pid}" 2>/dev/null; do
      if ${BB} test -e "${PILOT_STATE}/stop-adb" ||
         { [ "${adb_root}" = early ] &&
           [ -n "${PILOT_ADBD_RUNTIME_ROOT:-}" ] &&
           ${BB} test -f "${PILOT_STATE}/runtime-ready"; }; then
        ${BB} kill "${adb_pid}" ||
          pilot_failure adb "daemon exited while switching diagnostic roots"
        break
      fi
      if pilot_adbd_usb_open "${adb_pid}"; then
        echo open >"${PILOT_STATE}/adb-transport"
        adb_unready=0
        adb_attempt=0
      else
        adb_fd_status=$?
        if [ "${adb_fd_status}" = 2 ]; then
          pilot_usb_failure fd-unreadable
          # Missing evidence is not proof that an existing transport is bad.
          # Keep this child; never reset a potentially healthy opener merely
          # because procfs could not be inspected.
          adb_unready=0
        else
          pilot_usb_failure usb-fd-not-open
          adb_unready=$((adb_unready + 1))
        fi
        if [ "${adb_unready}" -ge 3 ]; then
          # The retained daemon selects USB just once. Re-evaluate only an
          # unready child; never toggle the gadget merely because a PID exists.
          ${BB} kill "${adb_pid}" 2>/dev/null || true
          break
        fi
      fi
      ${BB} sleep 1
    done
    wait "${adb_pid}"
    echo "adbd exited $?" >"${PILOT_STATE}/adbd-exit"
    ${BB} rm -f "${PILOT_STATE}/adbd.pid"
    adb_pid=""
    adb_attempt=$((adb_attempt + 1))
    [ "${adb_attempt}" -lt 30 ] || break
    ${BB} sleep 1
  done
  echo stopped >"${PILOT_STATE}/adb-transport"
  if ! ${BB} test -e "${PILOT_STATE}/stop-adb"; then
    pilot_usb_failure retry-budget-exhausted
  fi
}

pilot_node_from_sysfs() {
  dev_file="$1"
  node_path="$2"
  ${BB} test -r "${dev_file}" || return 1
  node_dev="$(${BB} cat "${dev_file}")" || return 1
  node_major="${node_dev%:*}"
  node_minor="${node_dev#*:}"
  case "${node_major}:${node_minor}" in
    *[!0-9:]*|:*|*:) return 1 ;;
  esac
  [ "${node_dev}" = "${node_major}:${node_minor}" ] || return 1
  [ "${node_major}" -gt 0 ] && [ "${node_major}" -le 4095 ] &&
    [ "${node_minor}" -le 1048575 ] || return 1
  expected_dev="$(printf '%x:%x' "${node_major}" "${node_minor}")"
  if ! ${BB} test -e "${node_path}" && ! ${BB} test -L "${node_path}"; then
    ${BB} mknod -m 0600 "${node_path}" c "${node_major}" "${node_minor}" || return 1
  fi
  ${BB} test ! -L "${node_path}" &&
    ${BB} test -c "${node_path}" &&
    [ "$(${BB} stat -c '%t:%T' "${node_path}" 2>/dev/null)" = "${expected_dev}" ] || return 1
  ${BB} chown 0:0 "${node_path}" && ${BB} chmod 0600 "${node_path}" || return 1
  [ "$(${BB} stat -c '%t:%T:%a:%u:%g' "${node_path}" 2>/dev/null)" = "${expected_dev}:600:0:0" ]
}

pilot_adbd_usb_open() {
  ${BB} test -c "${PILOT_ADBD_NODE:-/dev/android_adb}" || return 2
  adb_node_dev="$(${BB} stat -L -c '%t:%T' "${PILOT_ADBD_NODE:-/dev/android_adb}" 2>/dev/null)" || return 2
  adb_fd_dir="${PILOT_ADBD_PROC:-/proc}/$1/fd"
  ${BB} test -r "${adb_fd_dir}" && ${BB} test -x "${adb_fd_dir}" || return 2
  adb_fd_seen=0
  for adb_fd in "${adb_fd_dir}"/*; do
    ${BB} test -L "${adb_fd}" || continue
    adb_fd_seen=$((adb_fd_seen + 1))
    [ "${adb_fd_seen}" -le 128 ] || return 2
    adb_fd_target="$(${BB} readlink "${adb_fd}" 2>/dev/null)" || return 2
    # A runtime chroot opens through a bind mount; procfs may show its outer
    # /runtime/dev path. Device identity, not that spelling, identifies the FD.
    adb_fd_dev="$(${BB} stat -L -c '%F:%t:%T' "${adb_fd}" 2>/dev/null)" || return 2
    if [ "${adb_fd_dev}" = "character special file:${adb_node_dev}" ]; then
      return 0
    fi
  done
  [ "${adb_fd_seen}" -gt 0 ] || return 2
  return 1
}

pilot_usb_failure() {
  echo "$1" >"${PILOT_STATE}/adb-transport"
  if [ "$(${BB} cat "${PILOT_STATE}/usb-last-failure" 2>/dev/null)" != "$1" ]; then
    echo "$1" >"${PILOT_STATE}/usb-last-failure"
    ${BB} cut -d' ' -f1 /proc/uptime >"${PILOT_STATE}/usb-failure-uptime"
    pilot_failure usb "$1"
  fi
}

pilot_usb_prerequisites() {
  if ! ${BB} test -d "${PILOT_ADBD_GADGET}"; then
    pilot_usb_failure gadget-absent
    return 1
  fi
  configure_adb "${PILOT_ADBD_PRODUCT}" || {
    pilot_usb_failure gadget-config-failed
    return 1
  }
  pilot_node_from_sysfs "${PILOT_ADBD_DEV_FILE}" "${PILOT_ADBD_NODE}" || {
    pilot_usb_failure node-invalid-or-absent
    return 1
  }
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
    pilot_failure pty "PTY degraded; USB transport startup remains independent"
  fi

  # The sole supervisor owns prerequisite retry and the early/runtime handoff.
  # Startup returns immediately; neither missing PTYs nor late drivers gate PID 1.
  if ! ${BB} mkdir "${PILOT_STATE}/adb-owner" 2>/dev/null; then
    pilot_failure usb-owner "supervisor already owns transport or owner state is stale"
    return 1
  fi
  echo pending >"${PILOT_STATE}/adb-transport"
  adb_loop &
  echo "$!" >"${PILOT_STATE}/adbd-supervisor.pid"
  return 0
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
