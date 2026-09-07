#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Test explicit remote status handling against a legacy-ADB stand-in.

set -euo pipefail

TEMP_DIR=""

err() {
  printf "FAIL %s\n" "$1" >&2
  exit 1
}

cleanup() {
  if [[ -n "${TEMP_DIR}" ]]; then
    rm -rf -- "${TEMP_DIR}"
  fi
}

main() {
  local script_dir
  local temp_dir
  local output
  local status=0

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  temp_dir="$(mktemp -d)"
  TEMP_DIR="${temp_dir}"
  trap cleanup EXIT
  cat >"${temp_dir}/adb" <<'EOF'
#!/usr/bin/env bash
while [[ "${1:-}" != "shell" && $# -gt 0 ]]; do
  shift
done
[[ "${1:-}" == "shell" ]] || exit 0
shift
bash -c "${1:-}" || true
exit 0
EOF
  chmod 0700 "${temp_dir}/adb"

  # shellcheck source=/dev/null
  source "${script_dir}/collect-microphone-capture.sh"
  ADB_SERVER_PORT=5037
  ADB_SERIAL=test
  output="${temp_dir}/output"
  PATH="${temp_dir}:${PATH}"

  if remote_status "${output}" 'printf "payload\n"; exit 7'; then
    status=0
  else
    status=$?
  fi
  [[ "${status}" == "7" ]] ||
    err "remote status = ${status}, want 7"
  [[ "$(cat "${output}")" == "payload" ]] ||
    err "remote output was not preserved"

  if ! remote_status "${output}" 'printf "success\n"; exit 0'; then
    err "successful remote command was rejected"
  fi
  [[ "$(cat "${output}")" == "success" ]] ||
    err "successful output was not preserved"
  printf "PASS microphone collector remote status\n"
}

main "$@"
