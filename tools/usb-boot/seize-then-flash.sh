#!/bin/bash
# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT
#
# Watch for a seized U-Boot prompt, confirm it is real, then write NAND.
#
# This is the second half of the split that arm-seize.sh starts. Entry is a
# dice roll and the prompt is the prize; once it is held, nothing here is
# rushed.
#
# The prompt is found by poking it, not by matching a string.
#
# An earlier version waited for "MV88DE3100" to appear in the console log.
# That assumes the prompt arrives whole, and it does not: the console is a
# stream of USB interrupt transfers chopped at arbitrary boundaries. On the
# 05.8.12 write it stalled at "MV88D" for about fifty seconds, 1348 bytes in,
# with the device sitting at a perfectly live prompt. Matching the full string
# would have waited forever and reported nothing wrong. The stall was broken
# by sending a newline by hand, which is the tell that the newline was doing
# the real work all along.
#
# So: once the console has said anything, send a newline and watch for a
# reply. Something that answers a newline is a prompt, whatever arrived in the
# log before it. This assumes nothing about chunk boundaries and cannot hang
# on a partial line.
#
# The prompt is then confirmed with `version`, because a prompt that echoes is
# not yet proof it will execute, the same reasoning that made CUE_PLAYED a bad
# proof of sound. Every command is preceded by a bare newline, because U-Boot's
# boot script prints progress characters that concatenate with a command sent
# mid-line, which once produced "Unknown command '+l2nand'" and no write.
#
# Usage: seize-then-flash.sh EVIDENCE_DIR
set -uo pipefail

evidence="${1:?EVIDENCE_DIR}"
console=/tmp/uboot.log
fifo=/tmp/uboot_cmd
report="${evidence}/flash-report.txt"

say() { printf '%s %s\n' "$(date +%H:%M:%S)" "$*" | tee -a "${report}"; }

send() {
  printf '\r\n'   >"${fifo}"
  sleep 0.3
  printf '%s\r\n' "$1" >"${fifo}"
}

size() { wc -c <"${console}" 2>/dev/null || echo 0; }
poke() { printf '\r\n' >"${fifo}"; }

mkdir -p "${evidence}"
say "waiting for a prompt; nothing is written until one answers"

# Wait for the console to exist and say anything. No deadline: the operator
# may need many entry attempts and each failed one costs nothing here.
while [[ ! -s "${console}" ]]; do sleep 2; done
say "console has output; probing for a prompt"

# Probe until something answers. A device mid-boot ignores a newline; a device
# sitting at a prompt echoes and redraws. This is what finds the prompt, and
# it does not care where the USB transfers were chopped.
while true; do
  before="$(size)"
  poke
  sleep 2
  if [[ "$(size)" -gt "${before}" ]]; then
    say "a newline was answered; a prompt is live"
    break
  fi
  sleep 3
done

# Confirm with something that must produce output. Answering a newline could
# be an echo; a version string could not.
before="$(size)"
send "version"
sleep 3
if [[ "$(size)" -le "${before}" ]]; then
  say "prompt did not answer version; holding, not writing"
  exit 1
fi
say "prompt answered version; it is live"
tail -c 400 "${console}" | tr -cd '\11\12\15\40-\176' | tail -4 | tee -a "${report}"

say "writing NAND: l2nand 83"
send "l2nand 83"

# The write takes minutes and prints progress the whole way, so silence is the
# failure signal rather than elapsed time.
last=0; still=0
while true; do
  if grep -aqF "u2nand succeed" "${console}" 2>/dev/null; then
    say "u2nand succeed: NAND written"
    exit 0
  fi
  now="$(size)"
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
