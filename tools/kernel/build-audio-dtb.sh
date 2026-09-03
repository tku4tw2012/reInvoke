#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Add the installed-firmware Berlin audio nodes to the proven SPI/GPIO DTB.

set -euo pipefail

readonly EXPECTED_BASE_SHA256="beb6de062d697d73459ea0bbe2788d854f5c1ff539520f1a66fdea02980b8a6f"

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: build-audio-dtb.sh --input PATH --output PATH

Creates a full DTB from the reviewed SPI/GPIO diagnostic DTB by adding the
WM8904, Berlin I2S/GDMA, and ASoC machine nodes from the Invoke GPL source.
EOF
  exit "${exit_code}"
}

err() {
  printf "ERROR: %s\n" "$1" >&2
  exit 1
}

main() {
  local input_path=""
  local output_path=""
  local partial_path

  while (( $# > 0 )); do
    case "$1" in
      --input)
        [[ -n "${2:-}" ]] || err "--input requires a path"
        input_path="$2"
        shift 2
        ;;
      --output)
        [[ -n "${2:-}" ]] || err "--output requires a path"
        output_path="$2"
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

  [[ -f "${input_path}" ]] || err "input DTB not found: ${input_path}"
  [[ -n "${output_path}" ]] || err "--output is required"
  [[ ! -e "${output_path}" ]] ||
    err "refusing to overwrite existing output: ${output_path}"
  command -v fdtput >/dev/null || err "'fdtput' is required"
  command -v sha256sum >/dev/null || err "'sha256sum' is required"

  printf "%s  %s\n" "${EXPECTED_BASE_SHA256}" "${input_path}" |
    sha256sum --check --status ||
    err "input is not the reviewed SPI/GPIO diagnostic DTB"

  mkdir -p "$(dirname "${output_path}")"
  partial_path="${output_path}.partial"
  [[ ! -e "${partial_path}" ]] ||
    err "stale partial output exists: ${partial_path}"
  trap 'rm -f -- "${partial_path}"' EXIT
  cp "${input_path}" "${partial_path}"

  fdtput -c "${partial_path}" /soc/i2c@0/wm8904@1A
  fdtput -t s "${partial_path}" /soc/i2c@0/wm8904@1A \
    compatible wm8904
  fdtput -t x "${partial_path}" /soc/i2c@0/wm8904@1A reg 1a
  fdtput -t x "${partial_path}" /soc/i2c@0/wm8904@1A phandle 20
  fdtput -t x "${partial_path}" /soc/i2c@0/wm8904@1A linux,phandle 20

  # Preserve Harman's bindings exactly: the disclosed Berlin drivers bind
  # these legacy Ralink/MediaTek strings and parse the atmel routing property.
  fdtput -c "${partial_path}" /soc/gdma@0
  fdtput -t s "${partial_path}" /soc/gdma@0 \
    compatible ralink,mt7620a-gdma ralink,rt2880-gdma
  fdtput -t x "${partial_path}" /soc/gdma@0 reg f7e81600 800
  fdtput -t x "${partial_path}" /soc/gdma@0 '#dma-cells' 1
  fdtput -t x "${partial_path}" /soc/gdma@0 '#dma-channels' 10
  fdtput -t x "${partial_path}" /soc/gdma@0 '#dma-requests' 10
  fdtput -t x "${partial_path}" /soc/gdma@0 phandle 21
  fdtput -t x "${partial_path}" /soc/gdma@0 linux,phandle 21

  fdtput -c "${partial_path}" /soc/i2s@0
  fdtput -t s "${partial_path}" /soc/i2s@0 \
    compatible mtk,mt7628-i2s mtk,mt7621-i2s ralink,mt7620a-i2s
  fdtput -t x "${partial_path}" /soc/i2s@0 reg f7e81500 100
  fdtput -t x "${partial_path}" /soc/i2s@0 dmas 21 4 21 5
  fdtput -t s "${partial_path}" /soc/i2s@0 dma-names tx rx
  fdtput -t x "${partial_path}" /soc/i2s@0 phandle 22
  fdtput -t x "${partial_path}" /soc/i2s@0 linux,phandle 22

  fdtput -c "${partial_path}" /soc/sound
  fdtput -t s "${partial_path}" /soc/sound \
    compatible marvell,wm8904-audio
  fdtput -t s "${partial_path}" /soc/sound pinctrl-names default
  fdtput -t s "${partial_path}" /soc/sound atmel,model \
    "wm8904 @ SAMA5D3EK"
  fdtput -t s "${partial_path}" /soc/sound atmel,audio-routing \
    "Headphone Jack" HPOUTL \
    "Headphone Jack" HPOUTR \
    IN2L "Line In Jack" \
    IN2R "Line In Jack" \
    IN1L Mic
  fdtput -t x "${partial_path}" /soc/sound marvell,ssc-controller 22
  fdtput -t x "${partial_path}" /soc/sound marvell,audio-codec 20

  mv "${partial_path}" "${output_path}"
  trap - EXIT
  stat --format="%n %s bytes" "${output_path}"
  sha256sum "${output_path}"
}

main "$@"
