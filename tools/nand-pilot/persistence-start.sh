#!/bin/busybox sh
# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT

pilot_persistence_start() {
  if ! ${BB} timeout -t 12 -s KILL /bin/reinvoke-persist prepare; then
    pilot_failure persistence "prepare-failed; settings remain volatile"
    return 1
  fi
  supervise persistence /bin/reinvoke-persist serve
  persist_wait=0
  while ! ${BB} test -S /run/reinvoke/persistence.sock &&
        [ "${persist_wait}" -lt 3 ]; do
    persist_wait=$((persist_wait + 1))
    ${BB} sleep 1
  done
  if ! ${BB} test -S /run/reinvoke/persistence.sock; then
    pilot_failure persistence "service-not-ready; settings remain volatile"
    return 1
  fi
}

pilot_resume_then_exec() {
  if ${BB} mkdir -m 0700 "${PILOT_STATE}/wifi-resume-once" 2>/dev/null; then
    ${BB} timeout -t 30 -s KILL /usr/sbin/reinvoke-wifi-applyd \
      --resume --connect-timeout 20s &
    resume_pid=$!
    trap '${BB} kill "${resume_pid}" 2>/dev/null || true
      wait "${resume_pid}" 2>/dev/null || true
      exit 0' TERM INT HUP
    wait "${resume_pid}"
    resume_status=$?
    trap - TERM INT HUP
    if [ "${resume_status}" -ne 0 ]; then
      pilot_failure wifi "saved-profile-resume-unavailable; physical setup remains available"
    fi
  elif ! ${BB} test -d "${PILOT_STATE}/wifi-resume-once"; then
    pilot_failure wifi "resume-guard-unavailable; physical setup remains available"
  fi
  ${BB} test -e /run/reinvoke/shutdown && return 0
  exec "$@"
}
