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

# The device's own console is the only evidence that the iROM took the
# bootstrap and that U-Boot answered. It was being written to /tmp and left
# there, so seize-05813-2100 -- the run that put firmware on the unit in hand
# -- archived no console at all and the release criteria reported four
# failures against a flash that worked. Keep it with the report.
keep_console() {
  [[ -s "${console}" ]] && cp "${console}" "${evidence}/console.raw" || true
}
trap keep_console EXIT

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

# Probe until the device answers with something only a prompt produces.
#
# Growth alone is not an answer. The console is still printing its own boot
# script while the device is not yet listening, so "the log got bigger after I
# poked it" is satisfied by output that has nothing to do with the poke. That
# is what happened on the 05.8.13 write: this reported a live prompt at
# 21:03:38, sent l2nand, and nothing wrote for three minutes because the
# command landed mid-line and was swallowed. The write only ran after the same
# command was sent again by hand.
#
# So the test is a command whose reply is unmistakable. `version` prints the
# U-Boot banner, and nothing else on this console says "U-Boot". Asking for it
# and waiting for that word proves the device parsed a command, not merely
# that bytes arrived.
attempts=0
while true; do
  attempts=$((attempts + 1))
  marked="$(size)"
  send "version"
  sleep 3
  # Only look at what arrived after this probe, so an earlier banner in the
  # log cannot satisfy a later one.
  if tail -c +$((marked + 1)) "${console}" | grep -aqi "u-boot"; then
    say "the device answered version after ${attempts} probe(s); it is listening"
    break
  fi
  sleep 2
done
tail -c 400 "${console}" | tr -cd '\11\12\15\40-\176' | tail -4 | tee -a "${report}"

# Send the write, and confirm the device took it.
#
# A command can be sent to a live prompt and still be swallowed: on the
# 05.8.13 write the first l2nand landed while the console was mid-line and
# produced nothing at all. Sending it and assuming is how that went unnoticed
# for three minutes.
#
# The device prints "Erase NAND chip" before it writes a byte, so that is the
# receipt. Resending is safe while nothing has started; once erasing begins
# this stops and watches.
say "writing NAND: l2nand 83"
for attempt in 1 2 3; do
  send "l2nand 83"
  waited=0
  while [[ "${waited}" -lt 10 ]]; do
    if grep -aqiE "erase nand|writing NAND" "${console}"; then break 2; fi
    sleep 1
    waited=$((waited + 1))
  done
  say "l2nand produced nothing after ${attempt} attempt(s); resending"
done
if ! grep -aqiE "erase nand|writing NAND" "${console}"; then
  say "the device never acknowledged l2nand; holding, not assuming"
  exit 1
fi
say "the device acknowledged the write"

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
