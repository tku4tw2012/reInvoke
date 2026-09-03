#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Stage and start the RAM-only classic Bluetooth audio replacement stack.

set -euo pipefail

readonly ROOTFS_SHA256="2b373249af7ceb6793216617185e31ac30bb75cef2cf05d9af43fc5e434aa54a"
readonly ROOTFS_SIZE=48889856
readonly BLUETOOTHD_SHA256="22a68c5d1ff5a20f15a081dd5744e831236c9f3d38ef1796a805f60c8e1147a5"
readonly BLUEALSA_SHA256="66b43a54c1abfaaf45f3ef433466c8ecb69453795a655c89af48d2652e15e35e"
readonly BLUEALSA_APLAY_SHA256="2237cce8da108ed3239d54e8077a20fcefe9464ccd2d0c4041a4d650b887b94a"
readonly DEFAULT_SERIAL="0123456789ABCDEF"
readonly MOUNT_POINT="/mnt/installed"
readonly DEVICE_ROOTFS="/tmp/installed-rootfs-region.bin"

usage() {
  cat <<'EOF'
Usage: start-bluez-audio.sh --rootfs PATH --bluetoothd PATH \
  --bluealsa PATH --bluealsa-aplay PATH --hci-init PATH \
  --pairing-agent PATH --peer-address ADDRESS [--pair-seconds 0-300] \
  [--adb-serial SERIAL]

All target executables must be static ARM ELF files. Third-party service
artifacts are checksum-gated. The reviewed donor rootfs is mounted read-only
only to provide dbus-daemon. Bluetooth state remains in volatile RAM under
/tmp and /usr/var/lib/bluetooth.
EOF
}

err() {
  printf "ERROR: %s\n" "$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null || err "'$1' is required"
}

verify_sha256() {
  local path="$1"
  local expected="$2"

  printf "%s  %s\n" "${expected}" "${path}" |
    sha256sum --check --status ||
    err "checksum mismatch: ${path}"
}

verify_static_arm() {
  local path="$1"

  [[ -f "${path}" ]] || err "artifact not found: ${path}"
  file "${path}" | grep -q "ELF 32-bit.*ARM.*statically linked" ||
    err "artifact is not a static 32-bit ARM ELF: ${path}"
  if readelf -l "${path}" | grep -q "INTERP"; then
    err "artifact has a dynamic interpreter: ${path}"
  fi
  if readelf -d "${path}" 2>/dev/null | grep -q "NEEDED"; then
    err "artifact has a shared-library dependency: ${path}"
  fi
}

adb_shell() {
  adb -s "${ADB_SERIAL}" shell "$@"
}

ensure_rootfs_mount() {
  local mounted_release

  mounted_release="$(
    adb_shell "busybox cat '${MOUNT_POINT}/etc/version.txt' 2>/dev/null" |
      grep -F "Barracuda_libre-12.2050.3" |
      tail -1 || true
  )"
  if [[ "${mounted_release}" == "Barracuda_libre-12.2050.3" ]]; then
    return
  fi

  adb -s "${ADB_SERIAL}" push "${ROOTFS_PATH}" "${DEVICE_ROOTFS}" >/dev/null
  adb_shell "
    actual=\$(busybox sha256sum '${DEVICE_ROOTFS}' | busybox cut -d' ' -f1)
    busybox test \"\${actual}\" = '${ROOTFS_SHA256}' || exit 41
    busybox mkdir -p '${MOUNT_POINT}'
    busybox mknod /dev/loop0 b 7 0 2>/dev/null || true
    busybox losetup -d /dev/loop0 2>/dev/null || true
    busybox losetup -r /dev/loop0 '${DEVICE_ROOTFS}' || exit 42
    busybox mount -t squashfs -r /dev/loop0 '${MOUNT_POINT}' || exit 43
    for path in dev proc sys tmp; do
      busybox mount --bind \"/\${path}\" '${MOUNT_POINT}'/\"\${path}\" ||
        exit 44
    done
  "
}

stop_device_process() {
  local name="$1"
  local pid

  for pid in $(adb_shell "busybox pidof '${name}'" 2>/dev/null |
    grep -Eo '[0-9]+'); do
    adb_shell "kill '${pid}'"
  done
}

start_device_process() {
  local name="$1"
  local log_path="$2"
  local command="$3"
  local attempt
  local launch_output

  launch_output="$(
    adb_shell \
      "busybox rm -f '${log_path}'; busybox nohup busybox setsid ${command} </dev/null >'${log_path}' 2>&1 & busybox sleep 1; echo REINVOKE_LAUNCHED"
  )"
  grep -qF REINVOKE_LAUNCHED <<<"${launch_output}" ||
    err "failed to launch ${name}"
  for ((attempt = 0; attempt < 20; attempt++)); do
    if adb_shell "busybox pidof '${name}'" 2>/dev/null | grep -q '[0-9]'; then
      return
    fi
    sleep 0.25
  done
  adb_shell "busybox cat '${log_path}'" >&2 || true
  err "${name} did not remain running"
}

stage_artifacts() {
  adb -s "${ADB_SERIAL}" push "${BLUETOOTHD_PATH}" \
    /tmp/reinvoke-bluetoothd-5.55 >/dev/null
  adb -s "${ADB_SERIAL}" push "${BLUEALSA_PATH}" \
    /tmp/reinvoke-bluealsa >/dev/null
  adb -s "${ADB_SERIAL}" push "${BLUEALSA_APLAY_PATH}" \
    /tmp/reinvoke-bluealsa-aplay >/dev/null
  adb -s "${ADB_SERIAL}" push "${HCI_INIT_PATH}" \
    /tmp/reinvoke-hci-init >/dev/null
  adb -s "${ADB_SERIAL}" push "${PAIRING_AGENT_PATH}" \
    /tmp/reinvoke-bluez-pairing-agent >/dev/null
  adb -s "${ADB_SERIAL}" push "${CONFIG_PATH}" \
    /tmp/reinvoke-bluez-classic.conf >/dev/null
  adb_shell "chmod 700 /tmp/reinvoke-bluetoothd-5.55 \
    /tmp/reinvoke-bluealsa /tmp/reinvoke-bluealsa-aplay \
    /tmp/reinvoke-hci-init /tmp/reinvoke-bluez-pairing-agent"
}

start_stack() {
  local bus_address="unix:path=/tmp/reinvoke-dbus/system_bus_socket"

  stop_device_process "reinvoke-bluez-pairing-agent"
  stop_device_process "reinvoke-bluealsa-aplay"
  stop_device_process "reinvoke-bluealsa"
  stop_device_process "reinvoke-bluetoothd-5.55"
  stop_device_process "dbus-daemon"

  adb_shell "busybox rm -rf /tmp/reinvoke-dbus /tmp/reinvoke-bt-state;
    busybox rm -rf /usr/var/lib/bluetooth;
    busybox mkdir -p /tmp/reinvoke-dbus /tmp/reinvoke-bt-state \
      /usr/var/lib/bluetooth"
  start_device_process \
    "dbus-daemon" \
    "/tmp/reinvoke-dbus/dbus.log" \
    "busybox chroot '${MOUNT_POINT}' /usr/bin/dbus-daemon --session \
      --nofork --nopidfile --address='${bus_address}'"
  adb_shell "/tmp/reinvoke-hci-init --reset --unpair '${PEER_ADDRESS}'"

  start_device_process \
    "reinvoke-bluetoothd-5.55" \
    "/tmp/reinvoke-bt-state/bluetoothd.log" \
    "busybox env DBUS_SYSTEM_BUS_ADDRESS='${bus_address}' \
      /tmp/reinvoke-bluetoothd-5.55 -n -p a2dp,avrcp \
      -f /tmp/reinvoke-bluez-classic.conf"
  start_device_process \
    "reinvoke-bluealsa" \
    "/tmp/reinvoke-bt-state/bluealsa.log" \
    "busybox env DBUS_SYSTEM_BUS_ADDRESS='${bus_address}' \
      /tmp/reinvoke-bluealsa -p a2dp-sink -i hci0 --initial-volume=0"
  start_device_process \
    "reinvoke-bluealsa-aplay" \
    "/tmp/reinvoke-bt-state/bluealsa-aplay.log" \
    "busybox env DBUS_SYSTEM_BUS_ADDRESS='${bus_address}' \
      /tmp/reinvoke-bluealsa-aplay -v -D plughw:1,0 '${PEER_ADDRESS}'"
  start_device_process \
    "reinvoke-bluez-pairing-agent" \
    "/tmp/reinvoke-bt-state/pairing-agent.log" \
    "busybox env DBUS_SYSTEM_BUS_ADDRESS='${bus_address}' \
      /tmp/reinvoke-bluez-pairing-agent '${PEER_ADDRESS}' '${PAIR_SECONDS}'"
}

main() {
  local rootfs_size
  local script_dir

  ROOTFS_PATH=""
  BLUETOOTHD_PATH=""
  BLUEALSA_PATH=""
  BLUEALSA_APLAY_PATH=""
  HCI_INIT_PATH=""
  PAIRING_AGENT_PATH=""
  PEER_ADDRESS=""
  PAIR_SECONDS=0
  ADB_SERIAL="${DEFAULT_SERIAL}"

  while (( $# > 0 )); do
    case "$1" in
      --rootfs|--bluetoothd|--bluealsa|--bluealsa-aplay|--hci-init|--pairing-agent|--peer-address|--pair-seconds|--adb-serial)
        [[ -n "${2:-}" ]] || err "$1 requires a value"
        case "$1" in
          --rootfs) ROOTFS_PATH="$2" ;;
          --bluetoothd) BLUETOOTHD_PATH="$2" ;;
          --bluealsa) BLUEALSA_PATH="$2" ;;
          --bluealsa-aplay) BLUEALSA_APLAY_PATH="$2" ;;
          --hci-init) HCI_INIT_PATH="$2" ;;
          --pairing-agent) PAIRING_AGENT_PATH="$2" ;;
          --peer-address) PEER_ADDRESS="${2^^}" ;;
          --pair-seconds) PAIR_SECONDS="$2" ;;
          --adb-serial) ADB_SERIAL="$2" ;;
        esac
        shift 2
        ;;
      --help|-h)
        usage
        exit 0
        ;;
      *)
        err "unknown argument: $1"
        ;;
    esac
  done

  [[ "${PEER_ADDRESS}" =~ ^([0-9A-F]{2}:){5}[0-9A-F]{2}$ ]] ||
    err "--peer-address requires AA:BB:CC:DD:EE:FF"
  [[ "${PAIR_SECONDS}" =~ ^[0-9]+$ ]] ||
    err "--pair-seconds requires an integer"
  ((PAIR_SECONDS <= 300)) || err "--pair-seconds must not exceed 300"
  [[ -f "${ROOTFS_PATH}" ]] || err "rootfs not found: ${ROOTFS_PATH}"
  rootfs_size="$(stat --format="%s" "${ROOTFS_PATH}")"
  [[ "${rootfs_size}" == "${ROOTFS_SIZE}" ]] ||
    err "rootfs size does not match the reviewed carve"

  for command_name in adb file grep readelf sha256sum stat tail; do
    require_command "${command_name}"
  done
  verify_sha256 "${ROOTFS_PATH}" "${ROOTFS_SHA256}"
  verify_sha256 "${BLUETOOTHD_PATH}" "${BLUETOOTHD_SHA256}"
  verify_sha256 "${BLUEALSA_PATH}" "${BLUEALSA_SHA256}"
  verify_sha256 "${BLUEALSA_APLAY_PATH}" "${BLUEALSA_APLAY_SHA256}"
  for path in "${BLUETOOTHD_PATH}" "${BLUEALSA_PATH}" \
    "${BLUEALSA_APLAY_PATH}" "${HCI_INIT_PATH}" "${PAIRING_AGENT_PATH}"; do
    verify_static_arm "${path}"
  done

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  CONFIG_PATH="${script_dir}/bluez-classic.conf"
  [[ -f "${CONFIG_PATH}" ]] || err "BlueZ configuration is missing"
  [[ "$(adb -s "${ADB_SERIAL}" get-state 2>/dev/null)" == "device" ]] ||
    err "ADB device ${ADB_SERIAL} is not ready"

  ensure_rootfs_mount
  stage_artifacts
  start_stack
  if ((PAIR_SECONDS > 0)); then
    printf "RAM-only BlueZ audio stack is ready with a %s-second pairing window\n" \
      "${PAIR_SECONDS}"
  else
    printf "RAM-only BlueZ audio stack is ready and closed to new pairing\n"
  fi
}

main "$@"
