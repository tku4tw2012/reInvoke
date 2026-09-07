#!/usr/bin/env bash
# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT
#
# Run the reInvoke DSP interface host tests with the pinned Go compiler.

set -euo pipefail

readonly GO_BINARY_SHA256="91e7a78b1e449f8ebd59de015db7bb3ac49c6d94d416d45957790184c49baa3a"

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: test.sh [options]

Options:
  --archive-root PATH  External reInvoke archive root
  --image PATH         dsp-img.ldr to check against the recovered digests
  --help               Show this help
EOF
  exit "${exit_code}"
}

err() {
  printf "ERROR: %s\n" "$1" >&2
  exit 1
}

main() {
  local script_dir
  local repo_root
  local archive_root
  local goroot
  local go_binary
  local image_path="${REINVOKE_DSP_IMAGE:-}"

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
      --image)
        [[ -n "${2:-}" ]] || err "--image requires a path"
        image_path="$2"
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

  command -v sha256sum >/dev/null || err "'sha256sum' is required"

  goroot="${archive_root}/toolchains/ubuntu-go-1.18.1/extracted/usr/lib/go-1.18"
  go_binary="${goroot}/bin/go"
  [[ -x "${go_binary}" ]] || err "local Ubuntu Go compiler not found"
  printf "%s  %s\n" "${GO_BINARY_SHA256}" "${go_binary}" |
    sha256sum --check --status ||
    err "local Ubuntu Go compiler checksum mismatch"

  if [[ -n "${image_path}" ]]; then
    [[ -r "${image_path}" ]] || err "image not readable: ${image_path}"
    image_path="$(realpath "${image_path}")"
  fi

  cd "${script_dir}"
  GOROOT="${goroot}" GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GOWORK=off \
    "${go_binary}" vet ./...
  GOROOT="${goroot}" GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GOWORK=off \
    REINVOKE_DSP_IMAGE="${image_path}" \
    "${go_binary}" test ./...
}

main "$@"
