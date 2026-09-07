#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Run the reInvoke provisioning tests with the pinned Go compiler.

set -euo pipefail

readonly GO_BINARY_SHA256="91e7a78b1e449f8ebd59de015db7bb3ac49c6d94d416d45957790184c49baa3a"

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

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  repo_root="$(cd "${script_dir}/../.." && pwd)"
  archive_root="${REINVOKE_ARCHIVE:-${repo_root}/../reinvoke-archive}"

  if [[ "${1:-}" == "--archive-root" ]]; then
    [[ -n "${2:-}" ]] || err "--archive-root requires a path"
    archive_root="$2"
    shift 2
  fi
  (( $# == 0 )) || err "unknown argument: $1"
  command -v sha256sum >/dev/null || err "'sha256sum' is required"

  goroot="${archive_root}/toolchains/ubuntu-go-1.18.1/extracted/usr/lib/go-1.18"
  go_binary="${goroot}/bin/go"
  [[ -x "${go_binary}" ]] || err "local Ubuntu Go compiler not found"
  printf "%s  %s\n" "${GO_BINARY_SHA256}" "${go_binary}" |
    sha256sum --check --status ||
    err "local Ubuntu Go compiler checksum mismatch"

  cd "${script_dir}"
  GOROOT="${goroot}" GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GOWORK=off \
    "${go_binary}" vet ./...
  GOROOT="${goroot}" GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GOWORK=off \
    "${go_binary}" test ./...
}

main "$@"
