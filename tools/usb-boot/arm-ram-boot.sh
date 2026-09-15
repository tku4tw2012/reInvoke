#!/bin/bash
# Arm a RAM boot before the operator enters yellow mode.
#
# Same shape as arm-flash.sh, and for the same reason: everything that can be
# validated is validated while no hardware interaction is underway, then one
# helper and one console relay are left waiting. The operator's yellow entry is
# never a race against host setup.
#
# The difference from arm-flash.sh is what cannot happen here. Staging carries
# no 83_IMAGE, so there is no payload to write, and boot-native-ram.sh sends
# only usbload/bootargs/bootm. A NAND write is not reachable from this path.
#
# Usage:
#   arm-ram-boot.sh STAGING_DIR KERNEL KERNEL_SHA INITRAMFS INITRAMFS_SHA EVIDENCE_DIR
set -euo pipefail

here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "${here}/../.." && pwd)"
archive="${REINVOKE_ARCHIVE:-${repo}/../reinvoke-archive}"
staging="${1:?STAGING_DIR}"
kernel="${2:?KERNEL}"
kernel_sha="${3:?KERNEL_SHA}"
initramfs="${4:?INITRAMFS}"
initramfs_sha="${5:?INITRAMFS_SHA}"
evidence="${6:?EVIDENCE_DIR}"
helper="${INVOKE_USB_BOOT_BIN:-${archive}/tools/hk-invoke-arm-flasher/fast-poll/usb_boot_arm}"
port="${INVOKE_CONSOLE_PORT:-8141}"
console_log="/tmp/uboot.log"
console_fifo="/tmp/uboot_cmd"
log="${evidence}/arm-ram.log"

fail() {
  printf 'FAIL %s\n' "$*" >&2
  exit 1
}

[[ -d "${staging}" ]] || fail "staging not found: ${staging}"
[[ -x "${helper}" ]] || fail "helper not executable: ${helper}"

# Fail closed on anything that could turn this into a write.
[[ ! -e "${staging}/83_IMAGE" ]] || fail "83_IMAGE is staged; this path must not be able to write NAND"
[[ ! -e "${staging}/99_IMAGE" ]] || fail "99_IMAGE is staged"
grep -qvE '^\s*#|^\s*$' "${staging}/79_IMAGE" &&
  fail "79_IMAGE carries active commands; it must be comment-only"

[[ "$(pgrep -cx usb_boot_arm || true)" == "0" ]] || fail "a boot helper is already running"
[[ "$(ps -eo cmd --no-headers | grep -c '[u]boot-console.py' || true)" == "0" ]] ||
  fail "a console relay is already running"

mkdir -p "${evidence}"
: >"${log}"
: >"${console_log}"
rm -f "${console_fifo}"
mkfifo "${console_fifo}"

helper_pid=""
console_pid=""
loader_pid=""

cleanup() {
  for pid in "${loader_pid}" "${console_pid}" "${helper_pid}"; do
    [[ -z "${pid}" ]] || kill "${pid}" 2>/dev/null || true
  done
}
trap cleanup EXIT

# The relay only has something to connect to once the helper reaches its telnet
# phase, so it reconnects rather than exiting on a refused connection. Its log
# and FIFO paths are compiled in, not options, so they are not passed here.
(
  while true; do
    python3 -u "${here}/uboot-console.py" >>"${log}" 2>&1 || true
    sleep 0.5
  done
) &
console_pid=$!

(
  while true; do
    "${helper}" 1286 8174 "${staging}/" "${port}" >>"${log}" 2>&1 || true
    sleep 0.2
  done
) &
helper_pid=$!

printf 'READY helper and console relay are waiting; enter yellow mode at any time\n'
printf 'kernel %s\n' "${kernel_sha}"
printf 'initramfs %s\n' "${initramfs_sha}"
printf 'NOTE no 83_IMAGE is staged, so this path cannot write NAND\n'

bash "${here}/boot-native-ram.sh" \
  --kernel "${kernel}" --kernel-sha256 "${kernel_sha}" \
  --initramfs "${initramfs}" --initramfs-sha256 "${initramfs_sha}" \
  --firmware-dir "${staging}" \
  --console-log "${console_log}" \
  --console-fifo "${console_fifo}" \
  --wait-for-prompt 2>&1 | tee -a "${log}" &
loader_pid=$!

wait "${loader_pid}"
