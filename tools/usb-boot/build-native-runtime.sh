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
readonly LIBDL_SHA256="5ca9fa02d913cd40d8d19d0808a5d9ce6675b74ecf7f001264e2fb5f610b235d"
readonly LIBSTDCXX_SHA256="2e5af85d3bf99651be9de49bdf34e2e9007364ef39c0711502b33a8d9eec52a8"
readonly LIBDBUS_SHA256="267a1c0bdaf92971ba24f5461b7493c94c7054d3ff773850716c9b3ccc38f0d3"
readonly LIBEXPAT_SHA256="d19318dc7816e40de22254de3ff3ede69088c75db57fe8848dd349d6dcb5fab3"
readonly XTABLES_MULTI_SHA256="e5fb1081278a686b0b18f868e0d0a68b1436869909f341a2fb9a70f2416da36a"
readonly LIBIP4TC_SHA256="af27663f28809479ef53d34c2f54050d2160e59a4eaf3c38997c4ca0b77fd134"
readonly LIBIP6TC_SHA256="d34641ed9977ecfe3fbf933044145b83cc90e11080bacea8a3095103cf666a36"
readonly LIBXTABLES_SHA256="7e9445ae749599b4bb97a867743dd35928a26def88b1311762f82890b9427f81"
readonly LIBXT_STANDARD_SHA256="f5c4cc7348fdbe1abaca92737f2a7bfb211b66c30b989c1898789c47ffce7469"
readonly LIBXT_TCP_SHA256="60f2d12371114ebfa94ca2bdbc97ce1a3edb9ec8518b4240173d3d950fd8d8c7"
readonly HOSTAPD_SHA256="4f91e180a6fd967b531966438df36b5996495b6ac1002ce49fc8a660a114fe86"
readonly LIBCUTILS_SHA256="a7a8b3ad7eda0d2c3f1ad124cee3eaec91afbbe64c029c60c37cf27dc1bcb00a"
readonly LIBBINDER_SHA256="dc41217ad9825b9fc368e7a3ce5e28232c7af64fe176f86b9838e06817ce7c2e"
readonly LIBUTILS_SHA256="31b0f14a290ea602eb28a6b0a20a483edb8c705691a127c0067d7c47621a1f48"
readonly LIBENVITEMS_SHA256="2a4337b8e8ae4dde3be84b6a01e3f8bb3896903c01d123e3138ab455edd9f7a6"
readonly LIBCORKSCREW_SHA256="1295e8e51fa65667b29c4b38dd110059cfdc88ea6df06484d198188119d23d0e"
readonly LIBGCCDEMANGLE_SHA256="eebe28047959946b8e4c674c1ffe72c2f9f1c1b7c288a1c980dd59b331034be3"
readonly LIBLOG_SHA256="31b518c75e7eaea0d715cda4dbbcddd106f7df4140d12d317194e11065e5e3bb"
readonly LIBGLIBC_BRIDGE_SHA256="73e484bc8fded3017781ecf57c75257d076eccd2e09ac7effbfacb2569b5a3b6"
readonly LIBCRYPTO_SHA256="87ec0ce52f0940effe8efa25b4811896253034b9aa43851469427a3563fa8e7f"
readonly LIBSSL_SHA256="24361d67c73cee0a21e8bf486c1c8b7fb1ccae8a57542edd441557341e44e27e"
readonly LIBNL_SHA256="0456df48473aef6666690740199ba2cdac0ba1c1e8e08273cc7eda2356027eb8"
readonly LIBZ_SHA256="762f40f1e097b757c76e40626c3cac00c50ae657a076af5be977c1ca0cf1070d"
readonly LIBRT_SHA256="a355777befa4986e79b4759186c0f14b0e5424bd1453145f6b7197932453b140"
readonly BLUETOOTHD_SHA256="d3710607908e36ce1b3826ee5cb4154950267ff3828f10c71893c574436f778d"
readonly BLUEALSA_SHA256="62a3c8c465437240b9c8f1fa41bddbbde8fd98796a51527636c70a5ede605348"
readonly BLUEALSA_APLAY_SHA256="edc3a6cccb01bf4ac5ab8ab2898fad29e7fbafbebc1e9053aeed8b4c5f006558"
readonly BLUEALSA_CLI_SHA256="c39ffaae9f0c7c4b48aadf3a1e9dbd084680b657f6d721bd539cd687a6bcc0b5"
readonly HCI_INIT_SHA256="af1daebebf3df479c6ad132219ea9efba64fda2e072629b0b43bc71f64fb9330"
readonly MEDIA_CONTROL_SHA256="c8b864c1a7ded033b43d498821b7d9ff6edebc0bef7889b85ff2f6a2b64f0970"
# Repinned 2026-09-04 to the reproducible build from tools/control/bluez-pairing-agent.c.
# The prior pin ae60d800... matched no archived artifact and no recorded toolchain.
readonly PAIRING_AGENT_SHA256="faaba0eb1d350ee6210cc629c956a63ca313e65fe91441bfbf5093fbb2dfdbdc"
readonly MCU_INTERFACE_SHA256="21948ea319fa013f45b47255fbf809a40f75369f4f0504d9855f6796a3287833"
readonly DSP_INTERFACE_SHA256="4dc868e270e2d86e134f99b914da7e661fd0f1405494e225dd2eab01a2799a74"
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
  --bluealsa-cli PATH --hci-init PATH --media-control PATH \
  --pairing-agent PATH \
  --peer-address ADDRESS --output-dir PATH [--pair-seconds 0-300] \
  [--wamp-allow-cidr IPV4_OR_CIDR]... \
  [--provision-ap-ssid-file PATH --provision-ap-psk-file PATH] \
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

validate_ipv4_cidr() {
  local value="$1"
  local address="${value%/*}"
  local prefix=32
  local octet
  local -a octets

  if [[ "${value}" == */* ]]; then
    prefix="${value##*/}"
  fi
  [[ "${prefix}" =~ ^([0-9]|[12][0-9]|3[0-2])$ ]] || return 1
  IFS=. read -r -a octets <<<"${address}"
  (( ${#octets[@]} == 4 )) || return 1
  for octet in "${octets[@]}"; do
    [[ "${octet}" =~ ^(0|[1-9][0-9]{0,2})$ ]] || return 1
    ((10#${octet} <= 255)) || return 1
  done
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
  local media_control=""
  local pairing_agent=""
  local provision_ap_ssid_file=""
  local provision_ap_psk_file=""
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
  local -a wamp_allow_cidrs=()
  local -a cli_wamp_allow_cidrs=()
  local local_conf

  # Host-specific values live in an untracked local.conf so that Bluetooth
  # addresses never reach the repository. Command-line flags override it.
  local_conf="$(dirname "${BASH_SOURCE[0]}")/local.conf"
  if [[ -f "${local_conf}" ]]; then
    # shellcheck source=/dev/null
    source "${local_conf}"
    peer_address="${REINVOKE_PEER_ADDRESS:-${peer_address}}"
    pair_seconds="${REINVOKE_PAIR_SECONDS:-${pair_seconds}}"
    if [[ -n "${REINVOKE_WAMP_ALLOW_CIDRS:-}" ]]; then
      read -r -a wamp_allow_cidrs <<<"${REINVOKE_WAMP_ALLOW_CIDRS}"
    fi
    provision_ap_ssid_file="${REINVOKE_PROVISION_AP_SSID_FILE:-}"
    provision_ap_psk_file="${REINVOKE_PROVISION_AP_PSK_FILE:-}"
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
      --media-control)
        media_control="${2:-}"
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
      --wamp-allow-cidr)
        [[ -n "${2:-}" ]] || err "--wamp-allow-cidr requires a value"
        cli_wamp_allow_cidrs+=("$2")
        shift 2
        ;;
      --provision-ap-ssid-file)
        provision_ap_ssid_file="${2:-}"
        shift 2
        ;;
      --provision-ap-psk-file)
        provision_ap_psk_file="${2:-}"
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
  if (( ${#cli_wamp_allow_cidrs[@]} > 0 )); then
    wamp_allow_cidrs=("${cli_wamp_allow_cidrs[@]}")
  fi
  for cidr in "${wamp_allow_cidrs[@]}"; do
    validate_ipv4_cidr "${cidr}" ||
      err "--wamp-allow-cidr must be an IPv4 address or CIDR: ${cidr}"
  done
  if [[ -n "${provision_ap_ssid_file}" ||
        -n "${provision_ap_psk_file}" ]]; then
    [[ -f "${provision_ap_ssid_file}" &&
       -f "${provision_ap_psk_file}" ]] ||
      err "both provisioning AP credential files are required"
    [[ "$(stat -c '%a' "${provision_ap_ssid_file}")" == "600" &&
       "$(stat -c '%a' "${provision_ap_psk_file}")" == "600" ]] ||
      err "provisioning AP credential files must use mode 0600"
  fi
  [[ -n "${output_dir}" ]] || err "--output-dir is required"

  for command_name in chmod cut du file find grep install readelf realpath \
    sha256sum sort stat tr xargs; do
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
    "${media_control}"
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
  verify_sha256 "${media_control}" "${MEDIA_CONTROL_SHA256}"
  verify_sha256 "${pairing_agent}" "${PAIRING_AGENT_SHA256}"
  for path in \
    "${mcu_interface}" "${dsp_interface}" "${bluetoothd}" \
    "${bluealsa}" "${bluealsa_aplay}" "${bluealsa_cli}" \
    "${hci_init}" "${media_control}" "${pairing_agent}"; do
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
  verify_sha256 "${donor_rootfs}/lib/libdl-2.23.so" "${LIBDL_SHA256}"
  verify_sha256 \
    "${donor_rootfs}/usr/lib/libstdc++.so.6.0.21" "${LIBSTDCXX_SHA256}"
  verify_sha256 \
    "${donor_rootfs}/usr/lib/libdbus-1.so.3.14.6" "${LIBDBUS_SHA256}"
  verify_sha256 \
    "${donor_rootfs}/usr/lib/libexpat.so.1.6.0" "${LIBEXPAT_SHA256}"
  verify_sha256 "${donor_rootfs}/usr/sbin/xtables-multi" \
    "${XTABLES_MULTI_SHA256}"
  verify_sha256 "${donor_rootfs}/usr/lib/libip4tc.so.0.1.0" \
    "${LIBIP4TC_SHA256}"
  verify_sha256 "${donor_rootfs}/usr/lib/libip6tc.so.0.1.0" \
    "${LIBIP6TC_SHA256}"
  verify_sha256 "${donor_rootfs}/usr/lib/libxtables.so.11.0.0" \
    "${LIBXTABLES_SHA256}"
  verify_sha256 "${donor_rootfs}/usr/lib/xtables/libxt_standard.so" \
    "${LIBXT_STANDARD_SHA256}"
  verify_sha256 "${donor_rootfs}/usr/lib/xtables/libxt_tcp.so" \
    "${LIBXT_TCP_SHA256}"
  verify_sha256 "${donor_rootfs}/system/bin/hostapd" "${HOSTAPD_SHA256}"
  verify_sha256 "${donor_rootfs}/system/lib/libcutils.so" \
    "${LIBCUTILS_SHA256}"
  verify_sha256 "${donor_rootfs}/system/lib/libbinder.so" \
    "${LIBBINDER_SHA256}"
  verify_sha256 "${donor_rootfs}/system/lib/libutils.so" \
    "${LIBUTILS_SHA256}"
  verify_sha256 "${donor_rootfs}/usr/lib/libenvitems.so" \
    "${LIBENVITEMS_SHA256}"
  verify_sha256 "${donor_rootfs}/usr/lib/libcorkscrew.so" \
    "${LIBCORKSCREW_SHA256}"
  verify_sha256 "${donor_rootfs}/usr/lib/libgccdemangle.so" \
    "${LIBGCCDEMANGLE_SHA256}"
  verify_sha256 "${donor_rootfs}/lib/liblog.so" "${LIBLOG_SHA256}"
  verify_sha256 "${donor_rootfs}/lib/libglibc_bridge.so" \
    "${LIBGLIBC_BRIDGE_SHA256}"
  verify_sha256 "${donor_rootfs}/usr/lib/libcrypto.so.1.0.0" \
    "${LIBCRYPTO_SHA256}"
  verify_sha256 "${donor_rootfs}/usr/lib/libssl.so.1.0.0" \
    "${LIBSSL_SHA256}"
  verify_sha256 "${donor_rootfs}/system/lib/libnl.so" "${LIBNL_SHA256}"
  verify_sha256 "${donor_rootfs}/lib/libz.so" "${LIBZ_SHA256}"
  verify_sha256 "${donor_rootfs}/lib/librt-2.23.so" "${LIBRT_SHA256}"
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
    "${partial_dir}/lib/hostapd" "${partial_dir}/lib/xtables" \
    "${partial_dir}/share"
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
  install -m 0755 "${media_control}" \
    "${partial_dir}/bin/bluez-media-control"
  install -m 0755 "${pairing_agent}" \
    "${partial_dir}/bin/bluez-pairing-agent"
  for path in bluetoothd bluealsa bluealsa-aplay bluealsa-cli hci-init \
    bluez-media-control bluez-pairing-agent; do
    "${strip_tool}" --strip-unneeded "${partial_dir}/bin/${path}"
  done

  install -m 0755 "${donor_rootfs}/usr/bin/bonefish" \
    "${partial_dir}/bin/bonefish"
  install -m 0755 "${donor_rootfs}/usr/bin/dbus-daemon" \
    "${partial_dir}/bin/dbus-daemon"
  install -m 0755 "${donor_rootfs}/usr/sbin/xtables-multi" \
    "${partial_dir}/bin/iptables"
  install -m 0755 "${donor_rootfs}/system/bin/hostapd" \
    "${partial_dir}/bin/hostapd"
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
  install -m 0644 "${donor_rootfs}/lib/libdl-2.23.so" \
    "${partial_dir}/lib/libdl.so.2"
  install -m 0644 "${donor_rootfs}/usr/lib/libstdc++.so.6.0.21" \
    "${partial_dir}/lib/libstdc++.so.6"
  install -m 0644 "${donor_rootfs}/usr/lib/libdbus-1.so.3.14.6" \
    "${partial_dir}/lib/libdbus-1.so.3"
  install -m 0644 "${donor_rootfs}/usr/lib/libexpat.so.1.6.0" \
    "${partial_dir}/lib/libexpat.so.1"
  install -m 0644 "${donor_rootfs}/usr/lib/libip4tc.so.0.1.0" \
    "${partial_dir}/lib/libip4tc.so.0"
  install -m 0644 "${donor_rootfs}/usr/lib/libip6tc.so.0.1.0" \
    "${partial_dir}/lib/libip6tc.so.0"
  install -m 0644 "${donor_rootfs}/usr/lib/libxtables.so.11.0.0" \
    "${partial_dir}/lib/libxtables.so.11"
  install -m 0644 "${donor_rootfs}/usr/lib/xtables/libxt_standard.so" \
    "${partial_dir}/lib/xtables/libxt_standard.so"
  install -m 0644 "${donor_rootfs}/usr/lib/xtables/libxt_tcp.so" \
    "${partial_dir}/lib/xtables/libxt_tcp.so"
  install -m 0644 "${donor_rootfs}/system/lib/libcutils.so" \
    "${partial_dir}/lib/hostapd/libcutils.so"
  install -m 0644 "${donor_rootfs}/system/lib/libbinder.so" \
    "${partial_dir}/lib/hostapd/libbinder.so"
  install -m 0644 "${donor_rootfs}/system/lib/libutils.so" \
    "${partial_dir}/lib/hostapd/libutils.so"
  install -m 0644 "${donor_rootfs}/usr/lib/libenvitems.so" \
    "${partial_dir}/lib/hostapd/libenvitems.so"
  install -m 0644 "${donor_rootfs}/usr/lib/libcorkscrew.so" \
    "${partial_dir}/lib/hostapd/libcorkscrew.so"
  install -m 0644 "${donor_rootfs}/usr/lib/libgccdemangle.so" \
    "${partial_dir}/lib/hostapd/libgccdemangle.so"
  install -m 0644 "${donor_rootfs}/lib/liblog.so" \
    "${partial_dir}/lib/hostapd/liblog.so"
  install -m 0644 "${donor_rootfs}/lib/libglibc_bridge.so" \
    "${partial_dir}/lib/hostapd/libglibc_bridge.so"
  install -m 0644 "${donor_rootfs}/usr/lib/libcrypto.so.1.0.0" \
    "${partial_dir}/lib/hostapd/libcrypto.so"
  install -m 0644 "${donor_rootfs}/usr/lib/libssl.so.1.0.0" \
    "${partial_dir}/lib/hostapd/libssl.so"
  install -m 0644 "${donor_rootfs}/system/lib/libnl.so" \
    "${partial_dir}/lib/hostapd/libnl.so"
  install -m 0644 "${donor_rootfs}/lib/libz.so" \
    "${partial_dir}/lib/hostapd/libz.so"
  install -m 0644 "${donor_rootfs}/lib/librt-2.23.so" \
    "${partial_dir}/lib/librt.so.1"

  install -m 0644 "${script_dir}/dbus-session.conf" \
    "${partial_dir}/etc/dbus-session.conf"
  install -m 0644 "${script_dir}/bluez-classic.conf" \
    "${partial_dir}/etc/bluez-main.conf"
  if [[ -n "${provision_ap_ssid_file}" ]]; then
    install -m 0600 "${provision_ap_ssid_file}" \
      "${partial_dir}/etc/provision-ap-ssid"
    install -m 0600 "${provision_ap_psk_file}" \
      "${partial_dir}/etc/provision-ap-psk"
  fi
  cp -a "${donor_rootfs}/usr/share/lights" "${partial_dir}/share/"
  {
    printf "PEER_ADDRESS='%s'\n" "${peer_address^^}"
    printf "PAIR_SECONDS='%s'\n" "${pair_seconds}"
    printf "WAMP_ALLOW_CIDRS='%s'\n" "${wamp_allow_cidrs[*]}"
  } >"${partial_dir}/etc/runtime.conf"
  chmod 0600 "${partial_dir}/etc/runtime.conf"

  donor_version="$(tr -d '\r\n' < "${donor_rootfs}/etc/version.txt")"
  {
    printf "runtime_version=0.1\n"
    printf "donor_version=%s\n" "${donor_version}"
    printf "peer_address=%s\n" "${peer_address^^}"
    printf "pair_seconds=%s\n" "${pair_seconds}"
    printf "wamp_allow_cidrs=%s\n" "${wamp_allow_cidrs[*]:-none}"
    if [[ -n "${provision_ap_ssid_file}" ]]; then
      printf "provisioning_ap=configured\n"
    else
      printf "provisioning_ap=disabled\n"
    fi
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
