#!/usr/bin/env bash
# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT
set -euo pipefail
umask 022
here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "${here}/../.." && pwd)"
archive="$(realpath "${1:-${repo}/../reinvoke-archive}")"
# The output directory is required rather than defaulted. It used to fall back
# to one named artifact from 2026-09-13, which every later build silently
# inherited if the argument was forgotten, and which refuses to overwrite
# itself, so the failure arrived as a confusing refusal rather than a missing
# argument.
output="${2:?OUTPUT_DIR is required, for example ${archive}/build/artifacts/reinvoke-<version>-<date>/main}"
[[ -n "${PILOT_PRIVATE_CONFIG:-}" ]] || { echo "PILOT_PRIVATE_CONFIG is required" >&2; exit 1; }
[[ -n "${PILOT_PERSISTENCE_CONFIG:-}" ]] || { echo "PILOT_PERSISTENCE_CONFIG is required" >&2; exit 1; }
# Required, not optional. build.js treats an unset value as "no Bluedroid", so
# omitting it produces a complete-looking build with no Bluetooth stack and no
# reinvoke-identifiers. Candidate 05.8.11 shipped that way and flashed: every
# volume apply failed with "fork/exec /bin/reinvoke-identifiers: no such file
# or directory", so the DSP never took a level and the startup chime never
# played. Nothing in the build reported a problem.
[[ -n "${PILOT_BLUEDROID_CONFIG:-}" ]] || { echo "PILOT_BLUEDROID_CONFIG is required" >&2; exit 1; }
mkdir -p "${output}"
chmod 0700 "${output}"
output="$(realpath "${output}")"
[[ ! -e "${output}/rootfs.squashfs" && ! -e "${output}/build-a" && ! -e "${output}/build-b" ]] ||
  { echo "Refusing to overwrite a previous build" >&2; exit 1; }
# The date inside BUILD_ID is what lands in /etc/reinvoke/build-id on the
# device. It is a constant, so it goes stale silently: 2.2.8 was first built
# carrying the previous day, left over from the version rename. When the
# output path names a date, hold the two to each other.
build_id_date="$(node -e 'process.stdout.write(require("./tools/nand-pilot/build-lib.js").BUILD_ID.split("-").pop())')"
if [[ "${output}" =~ ([^0-9]|^)(20[0-9]{6})([^0-9]|$) ]]; then
  output_date="${BASH_REMATCH[2]}"
  [[ "${output_date}" == "${build_id_date}" ]] || {
    echo "BUILD_ID is dated ${build_id_date} but the output directory says ${output_date}" >&2
    echo "Bump BUILD_ID in tools/nand-pilot/build-lib.js or name the output to match" >&2
    exit 1
  }
fi
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
# Built from inside each module, not from the repository root.
#
# -trimpath rewrites source paths to the module path, and in GOPATH mode
# there is no module path to rewrite to, so the builder's absolute home
# directory survived into the binary: three occurrences in reinvoke-status,
# five in reinvoke-identifiers, seven in reinvoke-source-manager. The three
# binaries that were already clean are the ones whose build scripts cd into a
# directory holding a go.mod.
#
# It is cosmetic on the device and it is not cosmetic in a repository: the
# same leak put three x86-64 binaries into git history carrying the builder's
# home, which had to be rewritten out.
# A subshell cd, not `go build -C`: that flag arrives in Go 1.20 and the
# reviewed toolchain here is 1.18, which rejects it outright.
build_module() {
  local dir out
  dir="$1"
  out="$(realpath -m "$2")"
  (
    cd "${dir}"
    nice -n 10 env -u GO111MODULE -u GOFLAGS -u GOWORK \
      "${PILOT_GO}" test -p 1 ./...
    nice -n 10 env -u GO111MODULE -u GOFLAGS -u GOWORK \
      GOOS=linux GOARCH=arm GOARM=7 "${PILOT_GO}" build \
      -p 1 -trimpath -ldflags="-s -w -buildid=" -o "${out}" ./
  )
}

build_module ./tools/nand-pilot/status "${output}/reinvoke-status"
build_module ./tools/propertyd "${output}/reinvoke-propertyd"
build_module ./tools/source-manager "${output}/reinvoke-source-manager"
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
