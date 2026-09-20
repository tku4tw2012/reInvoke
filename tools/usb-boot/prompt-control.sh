#!/bin/bash
# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT
#
# Is there a U-Boot prompt on the other end?
#
# Not "did the console say MV88DE3100", which arrives chopped across USB
# interrupt transfers and stalled at "MV88D" for fifty seconds while the
# device was perfectly alive. Not "did the log grow", which is satisfied by
# the boot script printing on its own and, worse, by the relay writing its
# own "=== console closed ===" marker: that produced a PROMPT-CONTROL report
# against a device that had already gone.
#
# Both of those are guesses about text. This asks a question instead. The
# token below did not exist a millisecond ago, so nothing in a log, nothing
# in the boot script, and nothing the device would say on its own can contain
# it. Getting it back means something read a line, parsed it, and acted.
#
# Only bytes that arrive after the token is sent are considered, so nothing
# already in the console can satisfy it.
#
# Usage: prompt-control.sh [CONSOLE] [FIFO] [TRIES]
# Exit 0 with PROMPT-CONTROL on stdout when the device answered.
set -uo pipefail

console="${1:-/tmp/uboot.log}"
fifo="${2:-/tmp/uboot_cmd}"
tries="${3:-6}"

[[ -p "${fifo}" ]] || { echo "NO-CONTROL ${fifo} is not a FIFO" >&2; exit 2; }

for attempt in $(seq 1 "${tries}"); do
  token="rv$(head -c 6 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  mark=$(wc -c <"${console}" 2>/dev/null || echo 0)

  # A bare newline first: the boot script prints progress, and a command that
  # lands mid-line once became "Unknown command '+l2nand'" and wrote nothing.
  #
  # Bounded, because opening a FIFO for writing blocks until something opens
  # the read end. With the relay gone that blocks forever, and a detector that
  # hangs is no better than one that lies.
  send() { timeout 3 dd of="${fifo}" status=none 2>/dev/null <<<"$1"; }
  send "$(printf '\r')"
  sleep 0.4
  send "$(printf 'echo %s\r' "${token}")"

  for _ in $(seq 1 10); do
    sleep 0.5
    reply="$(tail -c +$((mark + 1)) "${console}" 2>/dev/null | tr -cd '\11\12\15\40-\176')"
    hits="$(grep -o "${token}" <<<"${reply}" | wc -l)"
    # Twice means the line was echoed back by the command editor and then
    # acted on. Once plus a refusal means it was parsed and rejected, which
    # proves a prompt just as well.
    if [[ "${hits}" -ge 2 ]] ||
       { [[ "${hits}" -ge 1 ]] && grep -qi "unknown command" <<<"${reply}"; }; then
      echo "PROMPT-CONTROL token=${token} returned=${hits}"
      exit 0
    fi
  done
done

echo "NO-CONTROL: ${tries} tokens sent, none came back"
exit 1
