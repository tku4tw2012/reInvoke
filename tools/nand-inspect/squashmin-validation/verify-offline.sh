#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
set -euo pipefail
umask 077

if [[ "${1:-}" == --fakeroot-child ]]; then
  [[ "$#" == 4 && -n "${FAKEROOTKEY:-}" ]] || exit 1
  source_image="$2"
  candidate="$3"
  evidence="$4"
  for label in source candidate; do
    image="${source_image}"
    [[ "${label}" == source ]] || image="${candidate}/minimal-probe.squashfs"
    unsquashfs -processors 2 -no-progress -dest "${evidence}/${label}-tree" \
      "${image}" >"${evidence}/${label}-extraction.log" 2>&1
    (
      cd "${evidence}/${label}-tree"
      find . -printf '%y %m %U %G %T@ %n %p %l\n' | LC_ALL=C sort
    ) >"${evidence}/${label}-metadata.txt"
    (
      cd "${evidence}/${label}-tree"
      find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum
    ) >"${evidence}/${label}-files.sha256"
    (
      cd "${evidence}/${label}-tree"
      # Canonicalize host inode numbers to first path to compare hardlink groups.
      find . -type f -printf '%p\t%i\n' | LC_ALL=C sort |
        awk -F '\t' '!($2 in first) {first[$2]=$1} {print $1 "\t" first[$2]}'
    ) >"${evidence}/${label}-hardlinks.txt"
  done
  cmp "${evidence}/source-metadata.txt" "${evidence}/candidate-metadata.txt"
  cmp "${evidence}/source-hardlinks.txt" "${evidence}/candidate-hardlinks.txt"
  cmp <(grep -v '  \./init\.rc$' "${evidence}/source-files.sha256") \
    <(grep -v '  \./init\.rc$' "${evidence}/candidate-files.sha256")
  cmp "${candidate}/original-init.rc" "${evidence}/source-tree/init.rc"
  cmp "${candidate}/candidate-init.rc" "${evidence}/candidate-tree/init.rc"
  if cmp -s "${candidate}/original-init.rc" "${candidate}/candidate-init.rc"; then exit 1; fi
  diff -u "${candidate}/original-init.rc" "${candidate}/candidate-init.rc" \
    >"${evidence}/init.diff" || [[ "$?" == 1 ]]
  {
    printf 'PASS full unsquashfs extraction: exactly init.rc content changed\n'
    printf 'PASS types, modes, uid/gid, mtime, link counts, symlink targets and hardlink groups identical\n'
    printf 'Tree entries: '; wc -l <"${evidence}/source-metadata.txt"
    printf 'Regular-file paths: '; wc -l <"${evidence}/source-files.sha256"
  } >"${evidence}/extraction-result.txt"
  rm -rf -- "${evidence}/source-tree" "${evidence}/candidate-tree"
  exit
fi

[[ "$#" == 5 || "$#" == 6 ]] || { echo 'Usage: verify-offline.sh SOURCE_SQUASHFS CANDIDATE_DIR KERNEL_SOURCE NEW_EXTERNAL_EVIDENCE_DIR PINNED_CAPTURE [stock-adb]' >&2; exit 1; }
case "${6:-legacy}" in
  legacy)
    candidate_init_sha=329259c33cdcdba7ad064333e962ebaee3997928eddf23545e3bdea3dc7485e3
    ;;
  stock-adb)
    candidate_init_sha=db83188488d5a8bc73f26dbd9333dd6479c51ec50d1c80cfd97822da8ad8ee62
    ;;
  *)
    echo 'Unknown diagnostic variant' >&2
    exit 1
    ;;
esac
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "${script_dir}/../../.." && pwd)"
source_image="$(realpath "$1")"
candidate="$(realpath "$2")"
kernel="$(realpath "$3")"
evidence="$(realpath -m "$4")"
capture="$(realpath "$5")"
case "${evidence}" in "${repo}"|"${repo}/"*) echo 'Evidence must be outside repository' >&2; exit 1;; esac
[[ ! -e "${evidence}" ]] || { echo 'Evidence directory must be new' >&2; exit 1; }
for tool in fakeroot unsquashfs cc sha256sum cmp awk dd stat; do command -v "${tool}" >/dev/null; done
printf '%s  %s\n' 717041d874bba6a16cda6578101ab1b7e1ff7737ade1b0921b77c9e4e65f6170 "${source_image}" | sha256sum --check --status
printf '%s  %s\n' 2b2a189751d3a2d7c9c5dcfba55da2ab5374cc3d0d1bbc03e809db1289442d14 "${candidate}/original-init.rc" | sha256sum --check --status
printf '%s  %s\n' "${candidate_init_sha}" "${candidate}/candidate-init.rc" | sha256sum --check --status
[[ "$(stat -c '%s' "${candidate}/minimal-probe.squashfs")" == 48831891 ]]
# These extents are independently parsed/asserted by the hash-pinned Go test.
# They include no metadata; comparing both outer ranges preserves inode IDs too.
cmp -n 3768546 "${source_image}" "${candidate}/minimal-probe.squashfs"
cmp -i 3816746 "${source_image}" "${candidate}/minimal-probe.squashfs"
cmp "${candidate}/fragment-original.zlib" <(dd if="${source_image}" \
  bs=65536 skip=3768546 count=48200 iflag=skip_bytes,count_bytes status=none)
cmp "${candidate}/fragment-candidate.zlib" <(dd if="${candidate}/minimal-probe.squashfs" \
  bs=65536 skip=3768546 count=48200 iflag=skip_bytes,count_bytes status=none)
mkdir -m 700 "${evidence}"
mkdir -m 700 "${evidence}/work"
export TMPDIR="${evidence}/work"
export GOTMPDIR="${evidence}/work" GOMAXPROCS=2 GOPROXY=off GOSUMDB=off
export GOFLAGS=-mod=readonly GOWORK=off CGO_ENABLED=0
export GOROOT="${SQUASHMIN_GOROOT:-${repo}/../reinvoke-archive/toolchains/ubuntu-go-1.18.1/extracted/usr/lib/go-1.18}"
[[ -x "${GOROOT}/bin/go" ]] || { echo 'Archived Go compiler not found' >&2; exit 1; }
trap 'rm -rf -- "${evidence}/work"' EXIT
(
  cd "${candidate}"
  sha256sum -- * >"${evidence}/candidate-artifacts.sha256"
)
(
  cd "${script_dir}/.."
  nice -n 10 "${GOROOT}/bin/go" build -p 1 -trimpath \
    -o "${evidence}/bundle-check" ./squashmin-validation/bundlecheck
) >"${evidence}/bundle-check-build.log" 2>&1
if ! nice -n 10 "${evidence}/bundle-check" -source "${source_image}" \
  -capture "${capture}" -candidate "${candidate}" >"${evidence}/bundle-check.log" 2>&1; then
  cat "${evidence}/bundle-check.log" >&2
  exit 1
fi

nice -n 10 fakeroot -- bash "${BASH_SOURCE[0]}" --fakeroot-child \
  "${source_image}" "${candidate}" "${evidence}"

# Compile the actual archived algorithm plus exact wrapper, not a reimplementation.
awk '/^static int zlib_uncompress\(/ {copying=1} copying {print} copying && /^}/ {exit}' \
  "${kernel}/fs/squashfs/zlib_wrapper.c" >"${evidence}/zlib_uncompress.inc"
[[ "$(grep -c '^static int zlib_uncompress(' "${evidence}/zlib_uncompress.inc")" == 1 ]]
nice -n 10 cc -O2 -Wall -Wextra -Wno-unused-parameter \
  -I"${script_dir}/kernel-shim" -I"${kernel}/include" -I"${evidence}" \
  "${script_dir}/kernel-inflate-check.c" \
  "${kernel}/lib/zlib_inflate/inflate.c" \
  "${kernel}/lib/zlib_inflate/inffast.c" \
  "${kernel}/lib/zlib_inflate/inftrees.c" \
  -o "${evidence}/kernel-inflate-check" >"${evidence}/kernel-compile.log" 2>&1
for label in original candidate; do
  nice -n 10 "${evidence}/kernel-inflate-check" "${candidate}/fragment-${label}.zlib" \
    "${candidate}/fragment-${label}.expanded" 3768546 \
    >"${evidence}/kernel-${label}.log" 2>&1
done
(
  cd "${kernel}"
  sha256sum fs/squashfs/{squashfs_fs.h,zlib_wrapper.c,block.c,fragment.c,inode.c,dir.c} \
    lib/zlib_inflate/{inflate.c,inffast.c,inftrees.c} include/linux/{zlib.h,zutil.h,zconf.h}
) >"${evidence}/kernel-source.sha256"
(
  cd "${candidate}"
  sha256sum --check --status "${evidence}/candidate-artifacts.sha256"
)
{
  cat "${evidence}/bundle-check.log"
  printf 'PASS exact init payload hashes, unchanged image length and every byte outside the parsed fragment\n'
  cat "${evidence}/extraction-result.txt"
  cat "${evidence}/kernel-candidate.log"
  printf 'Offline only: target sources compiled on host; no device/boot/signature validation.\n'
} | tee "${evidence}/RESULT.txt"
