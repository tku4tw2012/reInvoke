#!/bin/busybox sh
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT

pilot_adb_network_failed() {
  echo failed >"${PILOT_STATE}/adb-network-state"
  pilot_failure adb-network "$1"
}

pilot_adb_network_iptables() {
  ${BB} timeout -t 5 -s KILL ${BB} env XTABLES_LIBDIR=/opt/reinvoke/lib/xtables \
    /opt/reinvoke/lib/ld-linux-armhf.so.3 --library-path /opt/reinvoke/lib \
    /opt/reinvoke/bin/iptables "$@" >/dev/null 2>&1
}

pilot_adb_network_valid() {
  adb_config="${PILOT_ADB_NETWORK_CONFIG:-/etc/native-adb}"
  [ "$(${BB} stat -c '%a:%u:%g' "${adb_config}" 2>/dev/null)" = 700:0:0 ] &&
    ${BB} test ! -L "${adb_config}" || return 1
  for adb_file in enabled window-seconds allow-peers properties; do
    ${BB} test -f "${adb_config}/${adb_file}" &&
      ${BB} test ! -L "${adb_config}/${adb_file}" &&
      [ "$(${BB} stat -c '%a:%u:%g' "${adb_config}/${adb_file}" 2>/dev/null)" = 600:0:0 ] || return 1
  done
  [ "$(${BB} cat "${adb_config}/enabled")" = 1 ] || return 1
  [ "$(${BB} stat -c %s "${adb_config}/window-seconds")" -le 4 ] &&
    [ "$(${BB} stat -c %s "${adb_config}/allow-peers")" -le 19 ] || return 1
  adb_seconds="$(${BB} cat "${adb_config}/window-seconds")"
  case "${adb_seconds}" in ''|0*|*[!0-9]*) return 1 ;; esac
  [ "${adb_seconds}" -ge 1 ] && [ "${adb_seconds}" -le 300 ] || return 1
  ${BB} awk '
    NF != 1 || $0 !~ /^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+\/32$/ { exit 1 }
    {
      ip=$0; sub(/\/32$/, "", ip); split(ip, octet, ".");
      for (i=1;i<=4;i++)
        if (octet[i]+0>255 || (length(octet[i])>1 && substr(octet[i],1,1)=="0")) exit 1;
      privateIP=(octet[1]+0==10 || (octet[1]+0==172 && octet[2]+0>=16 && octet[2]+0<=31) ||
                 (octet[1]+0==192 && octet[2]+0==168));
      if (!privateIP || seen[ip]++) exit 1;
      count++;
    }
    END { if (count!=1) exit 1 }
  ' "${adb_config}/allow-peers" || return 1
  pilot_verify_payload "${adb_config}/properties" \
    71fda6da529f065e6716653ff505ca21074e218fc0567d65dce3dcb1fde1658c 32768 &&
    pilot_verify_payload /sbin/adbd-root \
    62593dfe9580443dca064e28c38cb647f1b719fe275666d5fb4b615781864ed6 \
    79776 || return 1
}

pilot_adb_network_run() {
  exec 9<"${adb_config}/properties" || exit 1
  export ANDROID_PROPERTY_WORKSPACE=9,32768
  exec /sbin/adbd-root
}

pilot_adb_network_usb_ready() {
  [ "$(${BB} cat "${PILOT_ADBD_GADGET:-/sys/class/android_usb/android0}/state" 2>/dev/null)" = CONFIGURED ] ||
    return 1
  adb_usb_pid="$(${BB} cat "${PILOT_STATE}/adbd.pid" 2>/dev/null)"
  case "${adb_usb_pid}" in ''|0|*[!0-9]*) return 1 ;; esac
  [ "${#adb_usb_pid}" -le 10 ] || return 1
  pilot_adbd_usb_open "${adb_usb_pid}"
}

pilot_adb_network_listening() {
  adb_inodes=""
  adb_fd_count=0
  for adb_fd in /proc/"$1"/fd/*; do
    adb_fd_count=$((adb_fd_count + 1))
    [ "${adb_fd_count}" -le 128 ] || return 1
    adb_link="$(${BB} readlink "${adb_fd}" 2>/dev/null)" || continue
    case "${adb_link}" in
      socket:\[*\]) adb_inode="${adb_link#socket:\[}"; adb_inode="${adb_inode%\]}"
        adb_inodes="${adb_inodes} ${adb_inode}" ;;
    esac
  done
  [ -n "${adb_inodes}" ] || return 1
  ${BB} awk -v owned="${adb_inodes} " \
    '$2=="00000000:15B3" && $4=="0A" && index(owned, " "$10" ") {found=1}
     END {exit !found}' /proc/net/tcp
}

pilot_adb_network_uptime() {
  adb_time="$(${BB} cut -d. -f1 /proc/uptime)" || return 1
  case "${adb_time}" in ''|*[!0-9]*) return 1 ;; esac
  printf '%s\n' "${adb_time}"
}

pilot_adb_network_close() {
  # Retain the INPUT jump and a terminal DROP. Removing the jump would reopen
  # the port under a permissive parent policy. Never flush a referenced chain.
  if [ -n "${adb_pid}" ]; then
    ${BB} kill "${adb_pid}" 2>/dev/null || true
    adb_stop=0
    while ${BB} kill -0 "${adb_pid}" 2>/dev/null && [ "${adb_stop}" -lt 3 ]; do
      adb_stop=$((adb_stop + 1))
      ${BB} sleep 1
    done
    if ${BB} kill -0 "${adb_pid}" 2>/dev/null; then
      ${BB} kill -KILL "${adb_pid}" 2>/dev/null || true
    fi
    wait "${adb_pid}" 2>/dev/null || true
  fi
  if [ "${adb_chain}" = 1 ]; then
    pilot_adb_network_iptables -I NATIVE_ADB 1 -j DROP ||
      pilot_adb_network_failed "firewall-close-failed"
    for adb_peer in ${adb_allowed}; do
      pilot_adb_network_iptables -D NATIVE_ADB -s "${adb_peer}" -j ACCEPT ||
        pilot_adb_network_failed "firewall-peer-cleanup-failed"
    done
  fi
  if [ "$(${BB} cat "${PILOT_STATE}/adb-network-state" 2>/dev/null)" != failed ]; then
    echo closed >"${PILOT_STATE}/adb-network-state"
  fi
  ${BB} rm -f "${PILOT_STATE}/adbd.pid" "${PILOT_STATE}/adbd-supervisor.pid"
  ${BB} rmdir "${PILOT_STATE}/adb-owner" 2>/dev/null || true
}

pilot_adb_network_loop() {
  adb_pid=""
  adb_chain=0
  adb_allowed=""
  trap 'pilot_adb_network_close' EXIT
  trap 'exit 0' TERM INT HUP
  pilot_adb_network_iptables -N NATIVE_ADB || {
    pilot_adb_network_failed "firewall-chain-unavailable"
    return 1
  }
  adb_chain=1
  pilot_adb_network_iptables -A NATIVE_ADB -j DROP &&
    pilot_adb_network_iptables -I INPUT 1 -p tcp --dport 5555 -j NATIVE_ADB || {
      pilot_adb_network_failed "firewall-install-failed"
      return 1
    }
  while IFS= read -r adb_peer; do
    pilot_adb_network_iptables -I NATIVE_ADB 1 -s "${adb_peer}" -j ACCEPT || {
      pilot_adb_network_failed "firewall-peer-rule-failed"
      return 1
    }
    adb_allowed="${adb_allowed} ${adb_peer}"
  done <"${adb_config}/allow-peers"
  echo installed >"${PILOT_STATE}/adb-network-firewall"
  adb_start="$(pilot_adb_network_uptime)" || {
    pilot_adb_network_failed "monotonic-clock-unavailable"
    return 1
  }
  adb_deadline=$((adb_start + adb_seconds))
  echo "${adb_deadline}" >"${PILOT_STATE}/adb-network-deadline"
  pilot_adb_network_run >"${PILOT_ADBD_LOG:-/dev/null}" 2>&1 &
  adb_pid=$!
  echo "${adb_pid}" >"${PILOT_STATE}/adbd.pid"
  echo tcp-runtime >"${PILOT_STATE}/adbd-root"
  adb_ready=0
  adb_checks=0
  while ! ${BB} test -e "${PILOT_STATE}/stop-adb-network" &&
        ! ${BB} test -e "${PILOT_RUNTIME_SHUTDOWN:-/run/reinvoke/shutdown}"; do
    adb_now="$(pilot_adb_network_uptime)" || {
      pilot_adb_network_failed "monotonic-clock-unavailable"
      return 1
    }
    if [ "${adb_now}" -ge "${adb_deadline}" ]; then
      echo expired >"${PILOT_STATE}/adb-network-result"
      return 0
    fi
    if ! ${BB} kill -0 "${adb_pid}" 2>/dev/null; then
      pilot_adb_network_failed "daemon-exited-no-retry"
      return 1
    fi
    if pilot_adb_network_listening "${adb_pid}"; then
      adb_ready=1
      echo listening >"${PILOT_STATE}/adb-network-state"
    elif [ "${adb_ready}" = 1 ] || [ "${adb_checks}" -ge 5 ]; then
      pilot_adb_network_failed "listener-unavailable-no-retry"
      return 1
    fi
    adb_checks=$((adb_checks + 1))
    ${BB} sleep 1
  done
  echo shutdown >"${PILOT_STATE}/adb-network-result"
}

pilot_adb_network_start() {
  adb_config="${PILOT_ADB_NETWORK_CONFIG:-/etc/native-adb}"
  if ! ${BB} test -e "${adb_config}/enabled"; then
    echo disabled >"${PILOT_STATE}/adb-network-state"
    return 0
  fi
  case "$(${BB} cat "${adb_config}/enabled" 2>/dev/null)" in
    0) echo disabled >"${PILOT_STATE}/adb-network-state"; return 0 ;;
    1) ;;
    *) pilot_adb_network_failed "invalid-enable-setting"; return 1 ;;
  esac
  pilot_adb_network_valid || {
    pilot_adb_network_failed "invalid-private-configuration"
    return 1
  }
  if pilot_adb_network_usb_ready; then
    echo usb-preserved >"${PILOT_STATE}/adb-network-state"
    return 0
  fi
  # One bounded window per boot, recorded only in RAM. Persistence failure
  # must not disable recovery access. Never kill a saved PID or steal an owner.
  ${BB} mkdir "${PILOT_STATE}/adb-network-once" 2>/dev/null || {
    pilot_failure adb-network "window-already-consumed"
    return 1
  }
  ${BB} touch "${PILOT_STATE}/stop-adb"
  adb_wait=0
  while ${BB} test -d "${PILOT_STATE}/adb-owner" && [ "${adb_wait}" -lt 6 ]; do
    adb_wait=$((adb_wait + 1))
    ${BB} sleep 1
  done
  ${BB} mkdir "${PILOT_STATE}/adb-owner" 2>/dev/null || {
    pilot_adb_network_failed "transport-owner-did-not-release"
    return 1
  }
  echo starting >"${PILOT_STATE}/adb-network-state"
  pilot_adb_network_loop &
  echo "$!" >"${PILOT_STATE}/adbd-supervisor.pid"
  return 0
}
