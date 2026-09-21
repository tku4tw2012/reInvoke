#!/bin/bash
# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT
#
# Write the staged 83_IMAGE to NAND. Nothing else.
#
# This does not wait for a device, does not hunt for a prompt, and has no
# opinion about when the right moment is. Catching the device is arm-seize.sh
# and deciding a prompt is live is prompt-control.sh; both already do their
# job and neither needs to be reimplemented here. Folding them together is
# what produced a flash path that had to guess how long to keep probing, and
# a probe that reported a live prompt against a device that had already gone.
#
# So the only thing left in here is the write and its receipt.
#
# It still re-asks before writing. That is not a substitute for checking
# first: it is a few seconds of insurance against the device having gone in
# the gap between the operator deciding and this running, and it costs one
# nonce.
#
# Usage: flash-nand.sh EVIDENCE_DIR
set -uo pipefail

here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
evidence="${1:?EVIDENCE_DIR}"
console=/tmp/uboot.log
fifo=/tmp/uboot_cmd
report="${evidence}/flash-report.txt"

mkdir -p "${evidence}"
say() { printf '%s %s\n' "$(date +%H:%M:%S)" "$*" | tee -a "${report}"; }

# Keep the device's own console with the report on every exit path. The
# 05.8.13 flash left it in /tmp, so the run that put firmware on the unit
# archived no console and the release criteria reported failures against it.
keep_console() {
  [[ -s "${console}" ]] && cp "${console}" "${evidence}/console.raw" || true
}
trap keep_console EXIT

send() { timeout 3 dd of="${fifo}" status=none 2>/dev/null <<<"$1"; }

say "confirming the prompt is still there before writing anything"
if ! "${here}/prompt-control.sh" "${console}" "${fifo}" 3 >>"${report}" 2>&1; then
  say "no prompt; refusing to write"
  exit 1
fi

# A command can reach a live prompt and still be swallowed. On the 05.8.13
# write the first l2nand landed while the console was mid-line and produced
# nothing at all for three minutes while the watcher reported success. The
# device prints "Erase NAND chip" before it writes a byte, so that is the
# receipt to wait for. Resending is safe while nothing has started.
say "writing NAND: l2nand 83"
for attempt in 1 2 3; do
  send "$(printf 'l2nand 83\r')"
  for _ in $(seq 1 10); do
    sleep 2
    if grep -aqiE "erase nand|writing NAND" "${console}"; then break 2; fi
  done
  say "no erase after attempt ${attempt}; resending"
done

if ! grep -aqiE "erase nand|writing NAND" "${console}"; then
  say "the device never acknowledged the write command; nothing was written"
  tail -c 300 "${console}" | tr -cd '\11\12\15\40-\176' | tail -3 | tee -a "${report}"
  exit 1
fi

say "erase started; watching for completion"
last=0
still=0
while true; do
  if grep -aqF "u2nand succeed" "${console}" 2>/dev/null; then
    say "u2nand succeed: NAND written"
    exit 0
  fi
  now="$(wc -c <"${console}" 2>/dev/null || echo 0)"
  if [[ "${now}" -eq "${last}" ]]; then
    still=$((still + 1))
    if [[ "${still}" -ge 40 ]]; then
      say "console silent for 200s without success; stopping"
      tail -c 300 "${console}" | tr -cd '\11\12\15\40-\176' | tail -3 | tee -a "${report}"
      exit 1
    fi
  else
    still=0
    addr=$(grep -ao 'writing NAND at address 0x[0-9A-F]*' "${console}" | tail -1)
    [[ -z "${addr}" ]] || say "${addr}"
  fi
  last="${now}"
  sleep 5
done
