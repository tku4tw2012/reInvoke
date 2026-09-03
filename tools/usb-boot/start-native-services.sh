#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Start the minimum RAM-only donor services needed for hardware diagnostics.

set -euo pipefail

readonly ROOTFS_SHA256="2b373249af7ceb6793216617185e31ac30bb75cef2cf05d9af43fc5e434aa54a"
readonly ROOTFS_SIZE=48889856
readonly DEVICE_ROOTFS="/tmp/installed-rootfs-region.bin"
readonly MOUNT_POINT="/mnt/installed"
readonly DEFAULT_SERIAL="0123456789ABCDEF"
readonly DEFAULT_IDENTITY_HEX="0252494e5601"

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: start-native-services.sh --rootfs PATH [options]

Options:
  --adb-serial SERIAL    ADB serial (default: 0123456789ABCDEF)
  --identity-hex HEX     RAM-only identity used by Bluedroid
  --music-volume VALUE  Initial muted software volume, 0-100 (default: 20)
  --start-dsp           Start the donor DSP adapter
  --pair                Open the bounded Bluetooth pairing window
  --no-bluetooth        Do not start the Bluetooth userspace service
  --help                Show this help

The reviewed rootfs image is checksum-gated and mounted from RAM read-only.
DSP startup is opt-in because its normal boot event transiently unmutes the
outputs before the launcher can reassert mute. The script never starts
system-manager, Podium, an updater, or a persistent-storage service.
EOF
  exit "${exit_code}"
}

err() {
  printf "ERROR: %s\n" "$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null || err "'$1' is required"
}

adb_shell() {
  adb -s "${ADB_SERIAL}" shell "$@"
}

device_process_exists() {
  local name="$1"

  adb_shell "busybox pidof ${name}" 2>/dev/null |
    grep -Eq '[0-9]'
}

wait_for_wamp_call() {
  local procedure="$1"
  local label="$2"
  local timeout_seconds="${3:-20}"
  local elapsed

  for ((elapsed = 0; elapsed < timeout_seconds * 2; elapsed++)); do
    if node "${WAMP_CALL}" "${procedure}" --timeout 1500 \
      >/dev/null 2>&1; then
      printf "%s ready after %d seconds\n" "${label}" "$((elapsed / 2))"
      return
    fi
    sleep 0.5
  done
  err "${label} did not answer within ${timeout_seconds} seconds"
}

wait_for_bluetooth_registration() {
  local elapsed

  for ((elapsed = 0; elapsed < 60; elapsed++)); do
    if node "${WAMP_CALL}" com.harman.source.get-registered --timeout 1500 \
      2>/dev/null | grep -qF com.harman.bluetooth; then
      printf "Bluetooth service registered after %d seconds\n" \
        "$((elapsed / 2))"
      return
    fi
    sleep 0.5
  done
  err "Bluetooth service did not register within 30 seconds"
}

start_detached_service() {
  local name="$1"
  local log_path="$2"
  local attempt
  local launch_attempt
  local launch_output
  shift 2
  local command="$*"

  if device_process_exists "${name}"; then
    printf "%s is already running\n" "${name}"
    return
  fi

  for ((launch_attempt = 1; launch_attempt <= 3; launch_attempt++)); do
    launch_output="$(
      adb_shell \
        "busybox rm -f '${log_path}'; busybox nohup busybox setsid busybox chroot '${MOUNT_POINT}' ${command} </dev/null >'${log_path}' 2>&1 & busybox sleep 1; echo REINVOKE_LAUNCHED"
    )"
    grep -qF REINVOKE_LAUNCHED <<<"${launch_output}" ||
      err "failed to launch ${name}"

    for ((attempt = 0; attempt < 20; attempt++)); do
      if device_process_exists "${name}"; then
        printf "%s started on attempt %d\n" "${name}" "${launch_attempt}"
        return
      fi
      sleep 0.25
    done

    if ((launch_attempt < 3)); then
      printf "%s launch attempt %d did not survive; retrying\n" \
        "${name}" "${launch_attempt}"
      sleep 2
    fi
  done

  adb_shell "busybox cat '${log_path}'" >&2 || true
  err "${name} did not remain running after launch"
}

ensure_rootfs_mount() {
  local mounted_release

  mounted_release="$(
    adb_shell \
      "busybox cat '${MOUNT_POINT}/etc/version.txt' 2>/dev/null" |
      grep -F "Barracuda_libre-12.2050.3" |
      tail -1
  )"
  if [[ "${mounted_release}" == "Barracuda_libre-12.2050.3" ]]; then
    printf "Reviewed donor rootfs is already mounted\n"
  else
    adb -s "${ADB_SERIAL}" push "${ROOTFS_PATH}" "${DEVICE_ROOTFS}" >/dev/null
    adb_shell "
      actual=\$(busybox sha256sum '${DEVICE_ROOTFS}' | busybox cut -d' ' -f1)
      busybox test \"\${actual}\" = '${ROOTFS_SHA256}' || exit 41
      busybox mkdir -p '${MOUNT_POINT}'
      busybox mknod /dev/loop0 b 7 0 2>/dev/null || true
      busybox losetup -d /dev/loop0 2>/dev/null || true
      busybox losetup -r /dev/loop0 '${DEVICE_ROOTFS}' || exit 42
      busybox mount -t squashfs -r /dev/loop0 '${MOUNT_POINT}' || exit 43
    " || err "failed to mount the reviewed donor rootfs from RAM"
  fi

  adb_shell "
    for path in dev proc sys tmp; do
      if ! busybox mount | busybox grep -q \" on ${MOUNT_POINT}/\${path} \"; then
        busybox mount --bind \"/\${path}\" '${MOUNT_POINT}'/\"\${path}\" ||
          exit 44
      fi
    done
    busybox mkdir -p \
      /tmp/reinvoke-lsync/data1/bluetooth \
      /tmp/reinvoke-lsync/data1/crash \
      /tmp/reinvoke-lsync/data1/misc/bluedroid
    if ! busybox mount | busybox grep -q \" on ${MOUNT_POINT}/lsync \"; then
      busybox mount --bind /tmp/reinvoke-lsync '${MOUNT_POINT}/lsync' ||
        exit 45
    fi
    echo REINVOKE_ROOTFS_READY
  " | grep -qF REINVOKE_ROOTFS_READY ||
    err "failed to mount the reviewed donor rootfs from RAM"
}

ensure_router() {
  local attempt

  start_detached_service \
    bonefish \
    /tmp/reinvoke-bonefish.log \
    /usr/bin/bonefish -r default -t 9999 -w 9998 -d

  adb -s "${ADB_SERIAL}" forward tcp:19999 tcp:9999 >/dev/null
  for ((attempt = 0; attempt < 20; attempt++)); do
    if timeout 1 bash -c "</dev/tcp/127.0.0.1/19999" 2>/dev/null; then
      printf "Bonefish router is ready\n"
      return
    fi
    sleep 0.25
  done
  err "Bonefish did not open the forwarded control port"
}

ensure_identity_service() {
  local pid_file="/tmp/reinvoke-identifiers-$(id -u).pid"
  local log_file="/tmp/reinvoke-identifiers-$(id -u).log"

  if node "${WAMP_CALL}" com.harman.identifiersGet --timeout 1500 \
    >/dev/null 2>&1; then
    printf "Identity compatibility service is already registered\n"
    return
  fi

  nohup node "${WAMP_SERVICE}" com.harman.identifiersGet \
    --kwargs \
    "{\"mac-hex\":\"${IDENTITY_HEX}\",\"unique-hex\":\"${IDENTITY_HEX}\"}" \
    >"${log_file}" 2>&1 &
  printf "%s\n" "$!" >"${pid_file}"
  wait_for_wamp_call com.harman.identifiersGet "Identity service"
}

ensure_mcu() {
  start_detached_service \
    mcu-interface \
    /tmp/reinvoke-mcu.log \
    /usr/bin/mcu-interface 127.0.0.1 9999
  wait_for_wamp_call com.harman.vui.getmcustatus "MCU service"
}

ensure_dsp() {
  start_detached_service \
    dsp-client \
    /tmp/reinvoke-dsp.log \
    /usr/bin/dsp-client
  wait_for_wamp_call com.harman.dsp.getVer "DSP service" 30

  node "${WAMP_CALL}" com.harman.vui.muteampcontrol \
    --args '["mute"]' --timeout 8000 >/dev/null
  node "${WAMP_CALL}" com.harman.vui.mutedaccontrol \
    --args '["mute"]' --timeout 8000 >/dev/null
  printf "Amplifier and DAC are muted\n"
}

ensure_audio_ui() {
  start_detached_service \
    audio-ui \
    /tmp/reinvoke-audio-ui.log \
    /usr/bin/audio-ui 127.0.0.1 9999 silent normal
  wait_for_wamp_call com.harman.volumeGet "Audio UI"

  adb_shell \
    "busybox chroot '${MOUNT_POINT}' /bin/sh -c 'for softvol in system music timer voice call; do echo -n | aplay -D \"\${softvol}\" >/dev/null 2>&1 || true; done'"
  node "${WAMP_CALL}" com.harman.volumeSet \
    --args "[${MUSIC_VOLUME},\"music\"]" --timeout 8000 >/dev/null
  node "${WAMP_CALL}" com.harman.extStateUpdate \
    --args '["system"]' \
    --kwargs '{"state":"normal"}' \
    --timeout 8000 >/dev/null
  printf "Audio UI is initialized at %s percent and remains muted\n" \
    "${MUSIC_VOLUME}"
}

ensure_source_manager() {
  start_detached_service \
    music-source-manager \
    /tmp/reinvoke-music-source-manager.log \
    /usr/bin/music-source-manager
  wait_for_wamp_call \
    com.harman.source.get-registered \
    "Music source manager"
}

ensure_bluetooth() {
  if ! adb_shell \
    "busybox test -L /sys/class/bluetooth/hci0 && echo HCI_READY" |
    grep -qF HCI_READY; then
    err "hci0 is absent; verify that the native bt8xxx module loaded"
  fi

  ensure_identity_service
  start_detached_service \
    bluetooth \
    /tmp/reinvoke-bluetooth.log \
    /usr/bin/bluetooth
  wait_for_bluetooth_registration

  if ((PAIR == 1)); then
    node "${WAMP_CALL}" com.harman.bluetoothPairing --timeout 8000 \
      >/dev/null
    printf "Bluetooth pairing window is active\n"
  fi
}

main() {
  local script_dir
  local rootfs_size

  ROOTFS_PATH=""
  ADB_SERIAL="${DEFAULT_SERIAL}"
  IDENTITY_HEX="${DEFAULT_IDENTITY_HEX}"
  MUSIC_VOLUME=20
  PAIR=0
  START_DSP=0
  START_BLUETOOTH=1

  while (( $# > 0 )); do
    case "$1" in
      --rootfs)
        [[ -n "${2:-}" ]] || err "--rootfs requires a path"
        ROOTFS_PATH="$2"
        shift 2
        ;;
      --adb-serial)
        [[ -n "${2:-}" ]] || err "--adb-serial requires a value"
        ADB_SERIAL="$2"
        shift 2
        ;;
      --identity-hex)
        [[ "${2:-}" =~ ^[0-9a-fA-F]{12}$ ]] ||
          err "--identity-hex requires 12 hexadecimal characters"
        IDENTITY_HEX="${2,,}"
        shift 2
        ;;
      --music-volume)
        [[ "${2:-}" =~ ^[0-9]+$ ]] ||
          err "--music-volume requires an integer"
        MUSIC_VOLUME="$2"
        ((MUSIC_VOLUME >= 0 && MUSIC_VOLUME <= 100)) ||
          err "--music-volume must be from 0 through 100"
        shift 2
        ;;
      --pair)
        PAIR=1
        shift
        ;;
      --start-dsp)
        START_DSP=1
        shift
        ;;
      --no-bluetooth)
        START_BLUETOOTH=0
        shift
        ;;
      --help|-h)
        usage
        ;;
      *)
        err "unknown argument: $1"
        ;;
    esac
  done

  [[ -f "${ROOTFS_PATH}" ]] || err "rootfs image not found: ${ROOTFS_PATH}"
  rootfs_size="$(stat --format="%s" "${ROOTFS_PATH}")"
  [[ "${rootfs_size}" == "${ROOTFS_SIZE}" ]] ||
    err "rootfs image size does not match the reviewed block-aligned carve"

  for command_name in adb bash grep node nohup sha256sum stat tail timeout; do
    require_command "${command_name}"
  done
  printf "%s  %s\n" "${ROOTFS_SHA256}" "${ROOTFS_PATH}" |
    sha256sum --check --status ||
    err "rootfs image checksum mismatch"

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  WAMP_CALL="${script_dir}/../control/wamp-call.mjs"
  WAMP_SERVICE="${script_dir}/../control/wamp-fixed-service.mjs"

  [[ -f "${WAMP_CALL}" ]] || err "WAMP client not found: ${WAMP_CALL}"
  [[ -f "${WAMP_SERVICE}" ]] ||
    err "WAMP service not found: ${WAMP_SERVICE}"
  [[ "$(adb -s "${ADB_SERIAL}" get-state 2>/dev/null)" == "device" ]] ||
    err "ADB device ${ADB_SERIAL} is not ready"

  ensure_rootfs_mount
  ensure_router
  ensure_mcu
  if ((START_DSP == 1)); then
    printf "WARNING: DSP boot transiently unmutes the physical outputs\n"
    ensure_dsp
  else
    printf "DSP adapter was not started; physical outputs remain muted\n"
  fi
  ensure_audio_ui
  ensure_source_manager
  if ((START_BLUETOOTH == 1)); then
    ensure_bluetooth
  fi

  printf "Native diagnostic services are ready; persistent storage is untouched\n"
}

main "$@"
