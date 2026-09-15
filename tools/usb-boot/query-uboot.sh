#!/bin/bash
# Ask U-Boot how it boots, without writing anything.
#
# The bootimgs record is the one record that is not a bare filesystem image: it
# carries a 128-byte header and a payload whose format we have not identified.
# Guessing that format costs a flash cycle per guess, so ask the bootloader
# instead. Every command sent here is a query.
#
# Usage:
#   query-uboot.sh STAGING_DIR EVIDENCE_DIR [COMMAND ...]
set -euo pipefail

here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "${here}/../.." && pwd)"
archive="${REINVOKE_ARCHIVE:-${repo}/../reinvoke-archive}"
staging="${1:?STAGING_DIR}"
evidence="${2:?EVIDENCE_DIR}"
shift 2
commands=("$@")
[[ "${#commands[@]}" -gt 0 ]] || commands=(printenv "nand info" version)
helper="${INVOKE_USB_BOOT_BIN:-${archive}/tools/hk-invoke-arm-flasher/fast-poll/usb_boot_arm}"
port="${INVOKE_CONSOLE_PORT:-8141}"
log="${evidence}/query.log"
console_log=/tmp/uboot.log
console_fifo=/tmp/uboot_cmd

fail() { printf 'FAIL %s\n' "$*" >&2; exit 1; }

# Refuse to run if anything here could turn into a write.
[[ ! -e "${staging}/83_IMAGE" ]] || fail "83_IMAGE is staged; queries must not be able to write"
[[ ! -e "${staging}/99_IMAGE" ]] || fail "99_IMAGE is staged"
grep -qvE '^\s*#|^\s*$' "${staging}/79_IMAGE" &&
  fail "79_IMAGE carries active commands; it must be comment-only"
for command in "${commands[@]}"; do
  case "${command}" in
    printenv*|version*|"nand info"*|"nand dump"*|nandrd*|md*|help*|bdinfo*|mtdparts*) ;;
    *) fail "refusing non-query command: ${command}" ;;
  esac
done

[[ "$(pgrep -cx usb_boot_arm || true)" == "0" ]] || fail "a boot helper is already running"
[[ "$(pgrep -cf '[u]boot-console.py' || true)" == "0" ]] || fail "a console relay is already running"

mkdir -p "${evidence}"
: >"${log}"
: >"${console_log}"
rm -f "${console_fifo}"
mkfifo "${console_fifo}"

helper_pid=""; console_pid=""
cleanup() {
  for pid in "${console_pid}" "${helper_pid}"; do
    [[ -z "${pid}" ]] || kill "${pid}" 2>/dev/null || true
  done
}
trap cleanup EXIT

( while true; do python3 -u "${here}/uboot-console.py" >>"${log}" 2>&1 || true; sleep 0.5; done ) &
console_pid=$!
( while true; do "${helper}" 1286 8174 "${staging}/" "${port}" >>"${log}" 2>&1 || true; sleep 0.2; done ) &
helper_pid=$!

printf 'READY query path armed; enter yellow mode at any time\n'
printf 'NOTE no 83_IMAGE is staged, so this path cannot write NAND\n'

# Wait for a live U-Boot prompt rather than a stale one already in the log.
printf 'waiting for the U-Boot prompt\n'
while ! grep -aq 'MV88D' "${console_log}" 2>/dev/null; do sleep 0.5; done
printf 'prompt seen; sending queries\n'
sleep 1

for command in "${commands[@]}"; do
  printf '\r\n' >"${console_fifo}"
  sleep 0.4
  printf '%s\r\n' "${command}" >"${console_fifo}"
  printf 'sent %s\n' "${command}"
  sleep 3
done

sleep 2
printf 'QUERIES COMPLETE; console log is %s\n' "${console_log}"
cp "${console_log}" "${evidence}/uboot-console.log" 2>/dev/null || true
