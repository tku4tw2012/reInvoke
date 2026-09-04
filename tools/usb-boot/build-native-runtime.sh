#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Build the checksum-gated autonomous service bundle for the RAM platform.

set -euo pipefail

readonly BONEFISH_SHA256="f8ca28a9536b2795adee89d17c38a616fca859b89bdf11529228790e36584b24"
readonly DBUS_DAEMON_SHA256="c90afd20329d5b8b1424f85398e95bdafe8342dc3dd0620c01297dc43e75f0fd"
readonly LOADER_SHA256="358b26b694942f323277ef7a70902d725b095f6d4d50e99f7ef572f835b0159e"
readonly LIBC_SHA256="20d13fcf2cea6bae2d4fe905c639ce6dc802e1c77a14ced0c8822838b0d41efa"
readonly LIBPTHREAD_SHA256="8f4682703885181725fa0d0aa96e9e6b54d73dd2c493aa9c32e7b507f21744b0"
readonly LIBGCC_SHA256="4640f7dc24f8403a08b43f87140f0a179892935837790eb0557711bdb1491bd1"
readonly LIBM_SHA256="28c07cb64eb112b14998a5606f3a31a415b7c8cee9900065ff69fd649a98b353"
readonly LIBSTDCXX_SHA256="2e5af85d3bf99651be9de49bdf34e2e9007364ef39c0711502b33a8d9eec52a8"
readonly LIBDBUS_SHA256="267a1c0bdaf92971ba24f5461b7493c94c7054d3ff773850716c9b3ccc38f0d3"
readonly LIBEXPAT_SHA256="d19318dc7816e40de22254de3ff3ede69088c75db57fe8848dd349d6dcb5fab3"
readonly BLUETOOTHD_SHA256="22a68c5d1ff5a20f15a081dd5744e831236c9f3d38ef1796a805f60c8e1147a5"
readonly BLUEALSA_SHA256="66b43a54c1abfaaf45f3ef433466c8ecb69453795a655c89af48d2652e15e35e"
readonly BLUEALSA_APLAY_SHA256="4c9978214873589991b995b482b5503fe16b9607e6a8c8896cef251ad3b1d937"
readonly BLUEALSA_CLI_SHA256="f65d5284b649ff6e5736d588434618b005d4c22864778704ae278cd673129954"
readonly HCI_INIT_SHA256="de8161326eb7d508a116ff03e3d1bdea869ce3453df58df5ecd1262156c2ba57"
# Repinned 2026-09-04 to the reproducible build from tools/control/bluez-pairing-agent.c.
# The prior pin ae60d800... matched no archived artifact and no recorded toolchain.
readonly PAIRING_AGENT_SHA256="faaba0eb1d350ee6210cc629c956a63ca313e65fe91441bfbf5093fbb2dfdbdc"
readonly MCU_INTERFACE_SHA256="c9102b23af4ca77d8d27a4e3892e1dfe927c39b7b33adbe6f4de4858f8e79763"
readonly DSP_INTERFACE_SHA256="f5b36cc396a07158ead5ad62d9f6da0899dbef6dc1dcb5e319a5e8b713e989b8"
readonly DSP_IMAGE_SHA256="e76f6ce7c53bb5b508507354fb08523089c136b3731d5ad4f4488a50526a44c8"
readonly ARM_STRIP_SHA256="fb5832708c993a6f196aac6fca7593a24c90f7b8316ede91382e5b55a88608dc"
readonly LIGHTS_MANIFEST_SHA256="7220f194246b53f91db12f22b822710e6f7ffa5fd20f620b71b015c8519a45fa"

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: build-native-runtime.sh \
  --donor-rootfs PATH \
  --mcu-interface PATH --dsp-interface PATH --dsp-image PATH \
  --bluetoothd PATH --bluealsa PATH --bluealsa-aplay PATH \
  --bluealsa-cli PATH --hci-init PATH --pairing-agent PATH \
  --peer-address ADDRESS --output-dir PATH [--pair-seconds 0-300] \
  [--strip-tool PATH]

Builds a deterministic runtime directory for the autonomous RAM platform.
The donor rootfs supplies only the pinned open-source Bonefish and D-Bus
binaries plus their isolated runtime libraries. It is not copied wholesale.
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

verify_sha256() {
  local path="$1"
  local expected="$2"

  [[ -f "${path}" ]] || err "artifact not found: ${path}"
  printf "%s  %s\n" "${expected}" "${path}" |
    sha256sum --check --status ||
    err "checksum mismatch: ${path}"
}

verify_static_arm() {
  local path="$1"

  file "${path}" | grep -q "ELF 32-bit.*ARM.*statically linked" ||
    err "artifact is not a static 32-bit ARM ELF: ${path}"
  if readelf -l "${path}" | grep -q "INTERP"; then
    err "artifact has a dynamic interpreter: ${path}"
  fi
  if readelf -d "${path}" 2>/dev/null | grep -q "NEEDED"; then
    err "artifact has a shared-library dependency: ${path}"
  fi
}

main() {
  local donor_rootfs=""
  local mcu_interface=""
  local dsp_interface=""
  local dsp_image=""
  local bluetoothd=""
  local bluealsa=""
  local bluealsa_aplay=""
  local bluealsa_cli=""
  local hci_init=""
  local pairing_agent=""
  local peer_address=""
  local pair_seconds=300
  local strip_tool=""
  local output_dir=""
  local partial_dir
  local script_dir
  local cleanup_command
  local donor_version
  local lights_manifest_sha256
  local -a required_paths
  local local_conf

  # Host-specific values live in an untracked local.conf so that Bluetooth
  # addresses never reach the repository. Command-line flags override it.
  local_conf="$(dirname "${BASH_SOURCE[0]}")/local.conf"
  if [[ -f "${local_conf}" ]]; then
    # shellcheck source=/dev/null
    source "${local_conf}"
    peer_address="${REINVOKE_PEER_ADDRESS:-${peer_address}}"
    pair_seconds="${REINVOKE_PAIR_SECONDS:-${pair_seconds}}"
  fi

  while (( $# > 0 )); do
    case "$1" in
      --donor-rootfs)
        donor_rootfs="${2:-}"
        shift 2
        ;;
      --mcu-interface)
        mcu_interface="${2:-}"
        shift 2
        ;;
      --dsp-interface)
        dsp_interface="${2:-}"
        shift 2
        ;;
      --dsp-image)
        dsp_image="${2:-}"
        shift 2
        ;;
      --bluetoothd)
        bluetoothd="${2:-}"
        shift 2
        ;;
      --bluealsa)
        bluealsa="${2:-}"
        shift 2
        ;;
      --bluealsa-aplay)
        bluealsa_aplay="${2:-}"
        shift 2
        ;;
      --bluealsa-cli)
        bluealsa_cli="${2:-}"
        shift 2
        ;;
      --hci-init)
        hci_init="${2:-}"
        shift 2
        ;;
      --pairing-agent)
        pairing_agent="${2:-}"
        shift 2
        ;;
      --peer-address)
        peer_address="${2:-}"
        shift 2
        ;;
      --pair-seconds)
        pair_seconds="${2:-}"
        shift 2
        ;;
      --strip-tool)
        strip_tool="${2:-}"
        shift 2
        ;;
      --output-dir)
        output_dir="${2:-}"
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

  [[ -d "${donor_rootfs}" ]] ||
    err "--donor-rootfs must name an extracted rootfs"
  [[ "${peer_address}" =~ ^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$ ]] ||
    err "--peer-address must be a Bluetooth address"
  [[ "${pair_seconds}" =~ ^[0-9]+$ ]] &&
    ((pair_seconds >= 0 && pair_seconds <= 300)) ||
    err "--pair-seconds must be from 0 through 300"
  [[ -n "${output_dir}" ]] || err "--output-dir is required"

  for command_name in chmod cut du file find grep install readelf realpath \
    sha256sum sort tr xargs; do
    require_command "${command_name}"
  done
  strip_tool="${strip_tool:-$(command -v arm-linux-gnueabihf-strip)}"
  verify_sha256 "${strip_tool}" "${ARM_STRIP_SHA256}"

  donor_rootfs="$(realpath "${donor_rootfs}")"
  output_dir="$(realpath --canonicalize-missing "${output_dir}")"
  partial_dir="${output_dir}.partial"
  [[ ! -e "${output_dir}" ]] ||
    err "refusing to overwrite output: ${output_dir}"
  [[ ! -e "${partial_dir}" ]] ||
    err "stale partial output exists: ${partial_dir}"

  required_paths=(
    "${mcu_interface}"
    "${dsp_interface}"
    "${dsp_image}"
    "${bluetoothd}"
    "${bluealsa}"
    "${bluealsa_aplay}"
    "${bluealsa_cli}"
    "${hci_init}"
    "${pairing_agent}"
  )
  for path in "${required_paths[@]}"; do
    [[ -n "${path}" ]] || err "all runtime artifact options are required"
  done

  verify_sha256 "${mcu_interface}" "${MCU_INTERFACE_SHA256}"
  verify_sha256 "${dsp_interface}" "${DSP_INTERFACE_SHA256}"
  verify_sha256 "${dsp_image}" "${DSP_IMAGE_SHA256}"
  verify_sha256 "${bluetoothd}" "${BLUETOOTHD_SHA256}"
  verify_sha256 "${bluealsa}" "${BLUEALSA_SHA256}"
  verify_sha256 "${bluealsa_aplay}" "${BLUEALSA_APLAY_SHA256}"
  verify_sha256 "${bluealsa_cli}" "${BLUEALSA_CLI_SHA256}"
  verify_sha256 "${hci_init}" "${HCI_INIT_SHA256}"
  verify_sha256 "${pairing_agent}" "${PAIRING_AGENT_SHA256}"
  for path in \
    "${mcu_interface}" "${dsp_interface}" "${bluetoothd}" \
    "${bluealsa}" "${bluealsa_aplay}" "${bluealsa_cli}" \
    "${hci_init}" "${pairing_agent}"; do
    verify_static_arm "${path}"
  done

  verify_sha256 "${donor_rootfs}/usr/bin/bonefish" "${BONEFISH_SHA256}"
  verify_sha256 "${donor_rootfs}/usr/bin/dbus-daemon" "${DBUS_DAEMON_SHA256}"
  verify_sha256 "${donor_rootfs}/lib/ld-2.23.so" "${LOADER_SHA256}"
  verify_sha256 "${donor_rootfs}/lib/libc-2.23.so" "${LIBC_SHA256}"
  verify_sha256 \
    "${donor_rootfs}/lib/libpthread-2.23.so" "${LIBPTHREAD_SHA256}"
  verify_sha256 "${donor_rootfs}/lib/libgcc_s.so.1" "${LIBGCC_SHA256}"
  verify_sha256 "${donor_rootfs}/lib/libm-2.23.so" "${LIBM_SHA256}"
  verify_sha256 \
    "${donor_rootfs}/usr/lib/libstdc++.so.6.0.21" "${LIBSTDCXX_SHA256}"
  verify_sha256 \
    "${donor_rootfs}/usr/lib/libdbus-1.so.3.14.6" "${LIBDBUS_SHA256}"
  verify_sha256 \
    "${donor_rootfs}/usr/lib/libexpat.so.1.6.0" "${LIBEXPAT_SHA256}"
  lights_manifest_sha256="$(
    cd "${donor_rootfs}/usr/share/lights"
    find . -type f -print0 |
      LC_ALL=C sort --zero-terminated |
      xargs --null sha256sum |
      sha256sum |
      cut -d' ' -f1
  )"
  [[ "${lights_manifest_sha256}" == "${LIGHTS_MANIFEST_SHA256}" ]] ||
    err "LED animation asset checksum mismatch"

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  mkdir -p "$(dirname "${output_dir}")" \
    "${partial_dir}/bin" "${partial_dir}/etc" \
    "${partial_dir}/lib" "${partial_dir}/share"
  printf -v cleanup_command 'rm -rf -- %q' "${partial_dir}"
  trap "${cleanup_command}" EXIT

  install -m 0755 "${mcu_interface}" \
    "${partial_dir}/bin/reinvoke-mcu-interface"
  install -m 0755 "${dsp_interface}" \
    "${partial_dir}/bin/reinvoke-dsp-interface"
  install -m 0644 "${dsp_image}" "${partial_dir}/share/dsp-img.ldr"
  install -m 0755 "${bluetoothd}" "${partial_dir}/bin/bluetoothd"
  install -m 0755 "${bluealsa}" "${partial_dir}/bin/bluealsa"
  install -m 0755 "${bluealsa_aplay}" "${partial_dir}/bin/bluealsa-aplay"
  install -m 0755 "${bluealsa_cli}" "${partial_dir}/bin/bluealsa-cli"
  install -m 0755 "${hci_init}" "${partial_dir}/bin/hci-init"
  install -m 0755 "${pairing_agent}" \
    "${partial_dir}/bin/bluez-pairing-agent"
  for path in bluetoothd bluealsa bluealsa-aplay bluealsa-cli hci-init \
    bluez-pairing-agent; do
    "${strip_tool}" --strip-unneeded "${partial_dir}/bin/${path}"
  done

  install -m 0755 "${donor_rootfs}/usr/bin/bonefish" \
    "${partial_dir}/bin/bonefish"
  install -m 0755 "${donor_rootfs}/usr/bin/dbus-daemon" \
    "${partial_dir}/bin/dbus-daemon"
  install -m 0755 "${donor_rootfs}/lib/ld-2.23.so" \
    "${partial_dir}/lib/ld-linux-armhf.so.3"
  install -m 0644 "${donor_rootfs}/lib/libc-2.23.so" \
    "${partial_dir}/lib/libc.so.6"
  install -m 0644 "${donor_rootfs}/lib/libpthread-2.23.so" \
    "${partial_dir}/lib/libpthread.so.0"
  install -m 0644 "${donor_rootfs}/lib/libgcc_s.so.1" \
    "${partial_dir}/lib/libgcc_s.so.1"
  install -m 0644 "${donor_rootfs}/lib/libm-2.23.so" \
    "${partial_dir}/lib/libm.so.6"
  install -m 0644 "${donor_rootfs}/usr/lib/libstdc++.so.6.0.21" \
    "${partial_dir}/lib/libstdc++.so.6"
  install -m 0644 "${donor_rootfs}/usr/lib/libdbus-1.so.3.14.6" \
    "${partial_dir}/lib/libdbus-1.so.3"
  install -m 0644 "${donor_rootfs}/usr/lib/libexpat.so.1.6.0" \
    "${partial_dir}/lib/libexpat.so.1"

  install -m 0644 "${script_dir}/dbus-session.conf" \
    "${partial_dir}/etc/dbus-session.conf"
  install -m 0644 "${script_dir}/bluez-classic.conf" \
    "${partial_dir}/etc/bluez-main.conf"
  cp -a "${donor_rootfs}/usr/share/lights" "${partial_dir}/share/"
  {
    printf "PEER_ADDRESS='%s'\n" "${peer_address^^}"
    printf "PAIR_SECONDS='%s'\n" "${pair_seconds}"
  } >"${partial_dir}/etc/runtime.conf"
  chmod 0600 "${partial_dir}/etc/runtime.conf"

  donor_version="$(tr -d '\r\n' < "${donor_rootfs}/etc/version.txt")"
  {
    printf "runtime_version=0.1\n"
    printf "donor_version=%s\n" "${donor_version}"
    printf "peer_address=%s\n" "${peer_address^^}"
    printf "pair_seconds=%s\n" "${pair_seconds}"
  } >"${partial_dir}/MANIFEST"

  find "${partial_dir}" -exec touch --no-dereference --date="@0" {} +
  (
    cd "${partial_dir}"
    find . -type f ! -name SHA256SUMS -print0 |
      LC_ALL=C sort --zero-terminated |
      xargs --null sha256sum >SHA256SUMS
  )
  mv "${partial_dir}" "${output_dir}"
  trap - EXIT
  printf "Built %s\n" "${output_dir}"
  du -sh "${output_dir}"
  sha256sum "${output_dir}/SHA256SUMS"
}

main "$@"
