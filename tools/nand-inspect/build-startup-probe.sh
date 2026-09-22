#!/usr/bin/env bash
# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT

set -euo pipefail

readonly CAPTURE_SHA256="2fac4159fe23aa25581c29f6c90033af3a1126a02593db0bd47e2c10d2c09f19"
readonly INIT_RC_SHA256="2b2a189751d3a2d7c9c5dcfba55da2ab5374cc3d0d1bbc03e809db1289442d14"
readonly ROOTFS_OFFSET=43122688
readonly ROOTFS_ALLOCATION=94371840
readonly ROOTFS_BYTES_USED=48831891
readonly ERASE_BYTES=131072

err() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

build_in_fakeroot() {
  local capture="$1"
  local output="$2"
  local stage="${output}/staging"

  unsquashfs -no-progress -offset "${ROOTFS_OFFSET}" -dest "${stage}" \
    "${capture}" >"${output}/extract.log"
  printf '%s  %s\n' "${INIT_RC_SHA256}" "${stage}/init.rc" |
    sha256sum --check --status ||
    err "captured init.rc does not match the reviewed startup"

  [[ "$(grep -c '^    write /sys/class/android_usb/android0/iProduct "MRVL USB SDK"$' "${stage}/init.rc")" = 1 ]] ||
    err "expected exactly one reviewed USB product-string setting"
  [[ "$(grep -c '^    write /sys/class/android_usb/android0/enable "1"$' "${stage}/init.rc")" = 1 ]] ||
    err "expected exactly one USB enable action"

  cp -p "${stage}/init.rc" "${output}/original-init.rc"
  touch -r "${stage}" "${output}/root-timestamp"
  (
    cd "${stage}"
    find . -printf '%y %m %U %G %T@ %p %l\n' | LC_ALL=C sort
  ) >"${output}/source-metadata.txt"
  (
    cd "${stage}"
    find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum
  ) >"${output}/source-file-hashes.txt"

  sed -i \
    -e 's|^    write /sys/class/android_usb/android0/iProduct "MRVL USB SDK"$|    write /sys/class/android_usb/android0/iProduct "reInvoke-rootfs-probe"|' \
    -e '/^    write \/sys\/class\/android_usb\/android0\/enable "1"$/a\    write /run/reinvoke-rootfs-probe post-fs-reached\n    start adbd' \
    "${stage}/init.rc"
  touch -r "${output}/original-init.rc" "${stage}/init.rc"
  touch -r "${output}/root-timestamp" "${stage}"

  (
    cd "${stage}"
    find . -printf '%y %m %U %G %T@ %p %l\n' | LC_ALL=C sort
  ) >"${output}/probe-metadata.txt"
  (
    cd "${stage}"
    find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum
  ) >"${output}/probe-file-hashes.txt"

  cmp "${output}/source-metadata.txt" "${output}/probe-metadata.txt" ||
    err "the patch changed filesystem metadata"
  cmp <(grep -v '  \./init\.rc$' "${output}/source-file-hashes.txt") \
    <(grep -v '  \./init\.rc$' "${output}/probe-file-hashes.txt") ||
    err "the patch changed a file other than init.rc"
  if cmp -s "${output}/original-init.rc" "${stage}/init.rc"; then
    err "the startup patch made no change"
  fi
  cp -p "${stage}/init.rc" "${output}/probe-init.rc"

  mksquashfs "${stage}" "${output}/startup-probe.squashfs" \
    -noappend -comp gzip -b 131072 -mkfs-time 0 -processors 1 \
    -no-progress -no-recovery >"${output}/build.log"
  rm -rf -- "${stage}"

  unsquashfs -no-progress -dest "${stage}" \
    "${output}/startup-probe.squashfs" >"${output}/verify-extract.log"
  (
    cd "${stage}"
    find . -printf '%y %m %U %G %T@ %p %l\n' | LC_ALL=C sort
  ) >"${output}/packed-metadata.txt"
  (
    cd "${stage}"
    find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum
  ) >"${output}/packed-file-hashes.txt"
  cmp "${output}/probe-metadata.txt" "${output}/packed-metadata.txt" ||
    err "repacking changed filesystem metadata"
  cmp "${output}/probe-file-hashes.txt" "${output}/packed-file-hashes.txt" ||
    err "repacking changed filesystem contents"
  rm -rf -- "${stage}"
}

main() {
  if [[ "${1:-}" = "--fakeroot-child" ]]; then
    [[ "$#" = 3 ]] || err "invalid internal arguments"
    [[ -n "${FAKEROOTKEY:-}" ]] || err "internal build requires fakeroot"
    build_in_fakeroot "$2" "$3"
    return
  fi

  if [[ "${1:-}" = "--help" || "$#" = 0 ]]; then
    printf 'Usage: build-startup-probe.sh CAPTURE OUTPUT_DIRECTORY\n'
    printf 'Builds an offline diagnostic SquashFS only; never accesses the device.\n'
    return
  fi
  [[ "$#" = 2 ]] || err "expected capture and new output directory"

  local script_dir repo_root capture output tool goroot inspector image_bytes
  local affected_bytes affected_blocks image_hash rollback_hash startup_hash
  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  repo_root="$(cd "${script_dir}/../.." && pwd)"
  capture="$(realpath "$1")"
  output="$(realpath -m "$2")"
  [[ -f "${capture}" ]] || err "capture is not a regular file"
  [[ ! -e "${output}" ]] || err "output directory already exists"
  case "${output}" in
    "${repo_root}"|"${repo_root}/"*)
      err "binary output must be outside the repository"
      ;;
  esac
  for tool in fakeroot unsquashfs mksquashfs sha256sum jq cmp dd; do
    command -v "${tool}" >/dev/null || err "required tool not found: ${tool}"
  done

  printf '%s  %s\n' "${CAPTURE_SHA256}" "${capture}" |
    sha256sum --check --status ||
    err "capture hash does not match the reviewed unit"

  goroot="${REINVOKE_ARCHIVE:-${repo_root}/../reinvoke-archive}/toolchains/ubuntu-go-1.18.1/extracted/usr/lib/go-1.18"
  [[ -x "${goroot}/bin/go" ]] || err "archived Go compiler not found"
  mkdir -m 0700 -p "${output}"
  inspector="${output}/nand-inspect"
  (
    cd "${script_dir}"
    GOROOT="${goroot}" GOPROXY=off GOSUMDB=off GOFLAGS=-mod=readonly \
      GOWORK=off CGO_ENABLED=0 \
      "${goroot}/bin/go" build -trimpath -o "${inspector}" .
  )
  "${inspector}" capture "${capture}" >"${output}/source-layout.json"
  jq -e \
    --argjson start "${ROOTFS_OFFSET}" \
    --argjson size "${ROOTFS_ALLOCATION}" \
    --argjson used "${ROOTFS_BYTES_USED}" \
    '.capture.records[] | select(.name == "rootfs") |
      .part1.allocation.start_byte == $start and
      .part1.allocation.bytes == $size and
      .part1.allocation == .part2.allocation and
      .part1_squashfs.bytes_used == $used' \
    "${output}/source-layout.json" >/dev/null ||
    err "captured rootfs metadata does not match the reviewed allocation"

  fakeroot -- bash "${BASH_SOURCE[0]}" --fakeroot-child "${capture}" "${output}"
  image_bytes="$(stat -c '%s' "${output}/startup-probe.squashfs")"
  [[ "${image_bytes}" -le "${ROOTFS_ALLOCATION}" ]] ||
    err "built filesystem exceeds rootfs allocation"
  affected_bytes="${image_bytes}"
  if (( ROOTFS_BYTES_USED > affected_bytes )); then
    affected_bytes="${ROOTFS_BYTES_USED}"
  fi
  affected_blocks=$(((affected_bytes + ERASE_BYTES - 1) / ERASE_BYTES))
  affected_bytes=$((affected_blocks * ERASE_BYTES))
  (( affected_bytes <= ROOTFS_ALLOCATION )) ||
    err "rounded erase span exceeds rootfs allocation"
  dd if="${capture}" of="${output}/rootfs-rollback.bin" \
    bs="${ERASE_BYTES}" skip="$((ROOTFS_OFFSET / ERASE_BYTES))" \
    count="${affected_blocks}" status=none
  [[ "$(stat -c '%s' "${output}/rootfs-rollback.bin")" = "${affected_bytes}" ]] ||
    err "rollback extraction was short"
  image_hash="$(sha256sum "${output}/startup-probe.squashfs" | cut -d ' ' -f 1)"
  rollback_hash="$(sha256sum "${output}/rootfs-rollback.bin" | cut -d ' ' -f 1)"
  startup_hash="$(sha256sum "${output}/probe-init.rc" | cut -d ' ' -f 1)"
  jq -n --arg source "${CAPTURE_SHA256}" --arg image "${image_hash}" \
    --arg rollback "${rollback_hash}" --arg startup "${startup_hash}" \
    --argjson start "${ROOTFS_OFFSET}" --argjson span "${affected_bytes}" \
    --argjson image_bytes "${image_bytes}" --argjson blocks "${affected_blocks}" \
    '{
      purpose: "normal-boot modified-rootfs diagnostic, not full reInvoke",
      write_approved: false,
      capture_sha256: $source,
      image_sha256: $image,
      rollback_sha256: $rollback,
      probe_init_rc_sha256: $startup,
      image_bytes: $image_bytes,
      start_byte: $start,
      end_exclusive: ($start + $span),
      affected_bytes: $span,
      erase_blocks: $blocks,
      backup_type: "data-only exact affected erase blocks, including tail",
      normal_hardware_ecc_required: true,
      expected_runtime_marker: "post-fs-reached",
      expected_usb_product: "reInvoke-rootfs-probe"
    }' >"${output}/PROPOSAL.json"
  unsquashfs -stat "${output}/startup-probe.squashfs" >"${output}/probe-superblock.txt"
  (
    cd "${output}"
    sha256sum startup-probe.squashfs rootfs-rollback.bin PROPOSAL.json \
      original-init.rc probe-init.rc source-layout.json \
      source-metadata.txt probe-metadata.txt source-file-hashes.txt \
      probe-file-hashes.txt packed-metadata.txt packed-file-hashes.txt >SHA256SUMS
  )
  printf 'Built diagnostic image at %s/startup-probe.squashfs\n' "${output}"
  printf 'No NAND operation was performed. This image is not a full reInvoke installation.\n'
}

main "$@"
