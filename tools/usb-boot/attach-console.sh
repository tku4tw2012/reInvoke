#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Attach the console client when a boot tool opens its local TCP relay.

set -euo pipefail

err() {
  printf "ERROR: %s\n" "$1" >&2
  exit 1
}

port_is_listening() {
  local port="$1"

  ss -ltn | grep -qE \
    "127\\.0\\.0\\.1:${port}|0\\.0\\.0\\.0:${port}|\\[::\\]:${port}"
}

main() {
  local port="${1:-}"
  local client="${2:-}"
  local timeout_seconds="${3:-180}"
  local checks
  local index

  [[ "${port}" =~ ^[0-9]+$ ]] || err "Provide a numeric TCP port"
  [[ -f "${client}" ]] || err "Console client not found: ${client}"
  [[ "${timeout_seconds}" =~ ^[0-9]+$ ]] || err "Timeout must be an integer"

  checks=$((timeout_seconds * 10))
  for ((index = 0; index < checks; index++)); do
    if port_is_listening "${port}"; then
      exec /usr/bin/python3 "${client}"
    fi
    sleep 0.1
  done

  err "Console port ${port} did not open within ${timeout_seconds} seconds"
}

main "$@"
