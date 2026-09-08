#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Build the static ARMv7 microphone capture owner and test client.

set -euo pipefail

readonly GO_BINARY_SHA256="91e7a78b1e449f8ebd59de015db7bb3ac49c6d94d416d45957790184c49baa3a"

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: build.sh --output PATH --client-output PATH [options]

Options:
  --archive-root PATH  External reInvoke archive root
  --output PATH        New ARMv7 capture-owner binary
  --client-output PATH New ARMv7 test-client binary
  --help               Show this help
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

main() {
  local script_dir
  local repo_root
  local archive_root
  local goroot
  local go_binary
  local output_path=""
  local client_output=""
  local path

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  repo_root="$(cd "${script_dir}/../.." && pwd)"
  archive_root="${REINVOKE_ARCHIVE:-${repo_root}/../reinvoke-archive}"

  while (( $# > 0 )); do
    case "$1" in
      --archive-root)
        [[ -n "${2:-}" ]] || err "--archive-root requires a path"
        archive_root="$2"
        shift 2
        ;;
      --output)
        [[ -n "${2:-}" ]] || err "--output requires a path"
        output_path="$2"
        shift 2
        ;;
      --client-output)
        [[ -n "${2:-}" ]] || err "--client-output requires a path"
        client_output="$2"
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

  [[ -n "${output_path}" ]] || err "--output is required"
  [[ -n "${client_output}" ]] || err "--client-output is required"
  for command_name in file mkdir mv realpath sha256sum; do
    require_command "${command_name}"
  done
  goroot="${archive_root}/toolchains/ubuntu-go-1.18.1/extracted/usr/lib/go-1.18"
  go_binary="${goroot}/bin/go"
  [[ -x "${go_binary}" ]] || err "local Ubuntu Go compiler not found"
  printf "%s  %s\n" "${GO_BINARY_SHA256}" "${go_binary}" |
    sha256sum --check --status ||
    err "local Ubuntu Go compiler checksum mismatch"

  for path in "${output_path}" "${client_output}"; do
    path="$(realpath --canonicalize-missing "${path}")"
    [[ ! -e "${path}" ]] || err "refusing to overwrite output: ${path}"
    [[ ! -e "${path}.partial" ]] ||
      err "stale partial output exists: ${path}.partial"
    mkdir -p "$(dirname "${path}")"
  done
  output_path="$(realpath --canonicalize-missing "${output_path}")"
  client_output="$(realpath --canonicalize-missing "${client_output}")"
  trap 'rm -f -- "${output_path}.partial" "${client_output}.partial"' EXIT

  (
    cd "${script_dir}"
    GOROOT="${goroot}" \
      GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
      GOPROXY=off GOSUMDB=off GOWORK=off \
      "${go_binary}" build \
      -mod=readonly -trimpath -buildvcs=false -ldflags="-s -w" \
      -o "${output_path}.partial" .
    GOROOT="${goroot}" \
      GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
      GOPROXY=off GOSUMDB=off GOWORK=off \
      "${go_binary}" build \
      -mod=readonly -trimpath -buildvcs=false -ldflags="-s -w" \
      -o "${client_output}.partial" ./cmd/mic-capture-client
  )
  mv "${output_path}.partial" "${output_path}"
  mv "${client_output}.partial" "${client_output}"
  trap - EXIT
  file "${output_path}" "${client_output}"
  sha256sum "${output_path}" "${client_output}"
}

main "$@"
