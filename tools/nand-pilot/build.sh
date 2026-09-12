#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
set -euo pipefail
umask 022
here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "${here}/../.." && pwd)"
archive="$(realpath "${1:-${repo}/../reinvoke-archive}")"
output="${2:-${archive}/build/artifacts/reinvoke-native-03-20260912/main}"
[[ -n "${PILOT_PRIVATE_CONFIG:-}" ]] || { echo "PILOT_PRIVATE_CONFIG is required" >&2; exit 1; }
mkdir -p "${output}"
chmod 0700 "${output}"
output="$(realpath "${output}")"
[[ ! -e "${output}/rootfs.squashfs" && ! -e "${output}/build-a" && ! -e "${output}/build-b" ]] ||
  { echo "Refusing to overwrite a previous build" >&2; exit 1; }
for tool in node fakeroot unsquashfs mksquashfs cpio gzip readelf strings nice; do
  command -v "${tool}" >/dev/null || { echo "Missing ${tool}" >&2; exit 1; }
done
export PILOT_GO="${PILOT_GO:-${archive}/toolchains/ubuntu-go-1.18.1/extracted/usr/lib/go-1.18/bin/go}"
[[ -x "${PILOT_GO}" ]] || { echo "Reviewed Go 1.18 runner unavailable" >&2; exit 1; }
export GOMAXPROCS=2 GO111MODULE=off CGO_ENABLED=0
export GOCACHE="${archive}/build/cache/go-1.18"
mkdir -p "${output}/work"
export TMPDIR="${output}/work" GOTMPDIR="${output}/work"
cd "${repo}"
nice -n 10 "${PILOT_GO}" test -p 1 ./tools/nand-pilot/status
nice -n 10 env GOOS=linux GOARCH=arm GOARM=7 "${PILOT_GO}" build \
  -p 1 -trimpath -ldflags="-s -w -buildid=" -o "${output}/reinvoke-status" ./tools/nand-pilot/status
for build in build-a build-b; do
  mkdir "${output}/${build}"
  nice -n 10 fakeroot -s "${output}/work/${build}.fakeroot" \
    node "${here}/build.js" prepare "${archive}" "${output}/${build}" \
    >"${output}/${build}/build.log" 2>&1 || {
      tail -35 "${output}/${build}/build.log" >&2
      exit 1
    }
done
nice -n 10 node "${here}/build.js" finalize "${archive}" "${output}"
