#!/usr/bin/env bash
# Drive U-Boot to a finished flash once catch-irom.sh has served the chain.
#
# Runs as one straight line with no cleverness, because the operator-facing
# window is short and every layer of logic added here has cost an attempt:
#
#   wait for the console port -> attach -> prove a FRESH prompt ->
#   re-verify the staged image -> l2nand 83 -> wait for the vendor's own
#   success line -> prove a fresh prompt again -> stop serving the image
#
# Fails closed at every step. It will not submit the flash unless it has just
# seen a live U-Boot answer a challenge it issued itself in this run.
set -euo pipefail

staging="${1:?STAGING_DIR}"
expected_sha="${2:?EXPECTED_SHA256}"
evidence="${3:?EVIDENCE_DIR}"
here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

console_port="${INVOKE_CONSOLE_PORT:-8141}"
fifo=/tmp/uboot_cmd
console_log=/tmp/uboot.log
state="${evidence}/flash.state"

mkdir -p "${evidence}"
: >"${state}"
note() { printf '%s %s\n' "$(date -u +%H:%M:%S)" "$*" | tee -a "${state}"; }

# A stale console log from an earlier flash contains a success line, so a
# later match would be a false positive. Start both files empty.
rm -f "${fifo}" "${console_log}"
mkfifo "${fifo}"
: >"${console_log}"

note "waiting for the helper to open the console on ${console_port}"
for _ in $(seq 1 3600); do
  ss -ltn 2>/dev/null | grep -q ":${console_port}" && break
  sleep 1
done
ss -ltn 2>/dev/null | grep -q ":${console_port}" || {
  note "ABORT console never opened"
  exit 1
}

note "attaching console"
setsid nohup python3 "${here}/uboot-console.py" >"${evidence}/console-client.log" 2>&1 </dev/null &
sleep 3

# Only a reply to a challenge issued in this run proves U-Boot is live now.
challenge="reinvoke-$(date -u +%H%M%S)-$$"
note "challenging U-Boot"
for attempt in $(seq 1 60); do
  printf 'echo %s\n' "${challenge}" >"${fifo}" 2>/dev/null || true
  sleep 1
  if strings "${console_log}" 2>/dev/null | grep -q "${challenge}"; then
    note "U-Boot answered on attempt ${attempt}"
    break
  fi
  [[ "${attempt}" == 60 ]] && { note "ABORT no fresh U-Boot reply"; exit 1; }
done

strings "${console_log}" | grep -q 'U-Boot 2013.04 (Apr 11 2016 - 10:10:25)' || {
  note "ABORT unexpected U-Boot build"
  exit 1
}

actual="$(sha256sum "${staging}/83_IMAGE" | cut -d' ' -f1)"
[[ "${actual}" == "${expected_sha}" ]] || {
  note "ABORT staged image is ${actual}"
  exit 1
}
note "image verified ${actual}"

note "submitting l2nand 83"
printf 'l2nand 83\n' >"${fifo}"

for _ in $(seq 1 600); do
  if strings "${console_log}" 2>/dev/null | grep -q 'Congratulations! u2nand succeed!'; then
    note "FLASH SUCCEEDED"
    printf 'version\n' >"${fifo}"
    sleep 3
    cp "${console_log}" "${evidence}/uboot-console.log"
    # Remove the image so nothing can re-serve it to a rebooting device.
    rm -f "${staging}/83_IMAGE"
    note "staged image removed; power-cycle normally when ready"
    exit 0
  fi
  sleep 1
done

note "ABORT no success line within 600s"
exit 1
