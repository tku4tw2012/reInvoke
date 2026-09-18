#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
set -euo pipefail
umask 022
here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "${here}/../.." && pwd)"
archive="$(realpath "${1:-${repo}/../reinvoke-archive}")"
output="${2:-${archive}/build/artifacts/reinvoke-native-05-20260913/main}"
[[ -n "${PILOT_PRIVATE_CONFIG:-}" ]] || { echo "PILOT_PRIVATE_CONFIG is required" >&2; exit 1; }
[[ -n "${PILOT_PERSISTENCE_CONFIG:-}" ]] || { echo "PILOT_PERSISTENCE_CONFIG is required" >&2; exit 1; }
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
export REINVOKE_ARCHIVE="${archive}"
export GOMAXPROCS=2 GO111MODULE=off CGO_ENABLED=0
export GOCACHE="${archive}/build/cache/go-1.18"
mkdir -p "${output}/work"
export TMPDIR="${output}/work" GOTMPDIR="${output}/work"
cd "${repo}"
# The mcu-interface binary is pinned by path and hash in the persistence
# config rather than built here, so a candidate can be built, tested and
# flashed while the pin still names the previous build. That happened: a
# build reported success with every change absent from the image, because
# the stale binary matched its own stale hash. The pin must be the binary
# this source produces.
mcu_pin_sha="$(node -e 'const c=require(process.argv[1]);process.stdout.write(c.mcu.sha256)' "${PILOT_PERSISTENCE_CONFIG}")"
mcu_pin_path="$(node -e 'const c=require(process.argv[1]);process.stdout.write(c.mcu.path)' "${PILOT_PERSISTENCE_CONFIG}")"
rm -f "${output}/work/mcu-pin-check"
env -u GO111MODULE -u GOFLAGS -u GOWORK \
  "${here}/../mcu-interface/build.sh" --output "${output}/work/mcu-pin-check" >/dev/null
mcu_built_sha="$(sha256sum "${output}/work/mcu-pin-check" | cut -d" " -f1)"
if [[ "${mcu_built_sha}" != "${mcu_pin_sha}" ]]; then
  echo "mcu-interface pin is not this source" >&2
  echo "  pinned : ${mcu_pin_sha}  (${mcu_pin_path})" >&2
  echo "  built  : ${mcu_built_sha}" >&2
  exit 1
fi
rm -f "${output}/work/mcu-pin-check"
nice -n 10 "${PILOT_GO}" test -p 1 ./tools/nand-pilot/status
nice -n 10 env GOOS=linux GOARCH=arm GOARM=7 "${PILOT_GO}" build \
  -p 1 -trimpath -ldflags="-s -w -buildid=" -o "${output}/reinvoke-status" ./tools/nand-pilot/status
nice -n 10 "${PILOT_GO}" test -p 1 ./tools/propertyd
nice -n 10 env GOOS=linux GOARCH=arm GOARM=7 "${PILOT_GO}" build \
  -p 1 -trimpath -ldflags="-s -w -buildid=" -o "${output}/reinvoke-propertyd" ./tools/propertyd
nice -n 10 "${PILOT_GO}" test -p 1 ./tools/source-manager
nice -n 10 env GOOS=linux GOARCH=arm GOARM=7 "${PILOT_GO}" build \
  -p 1 -trimpath -ldflags="-s -w -buildid=" -o "${output}/reinvoke-source-manager" ./tools/source-manager
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
