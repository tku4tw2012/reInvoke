#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Add the Invoke DSP SPI controller and required low GPIO base.

set -euo pipefail

readonly EXPECTED_BASE_SHA256="0858bd3ce4a7d07c6bb901629b6a05250c53ef72427d5de5ad1b6e0a27a19e31"

usage() {
  local exit_code="${1:-0}"

  cat <<'EOF'
Usage: build-spi-dtb.sh --input PATH --output PATH

Creates a full DTB from the known-good recovery DTB with one DesignWare SPI
controller, a conservative 1 MHz spidev child on chip select 0, and the
low-bank base required by the Invoke GPL GPIO driver.
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
    err "input is not the reviewed known-good recovery DTB"

  mkdir -p "$(dirname "${output_path}")"
  partial_path="${output_path}.partial"
  [[ ! -e "${partial_path}" ]] ||
    err "stale partial output exists: ${partial_path}"
  trap 'rm -f -- "${partial_path}"' EXIT
  cp "${input_path}" "${partial_path}"

  fdtput -c "${partial_path}" /soc/spi@F7E81C00
  fdtput -t s "${partial_path}" /soc/spi@F7E81C00 \
    compatible snps,designware-spi
  fdtput -t x "${partial_path}" /soc/spi@F7E81C00 reg f7e81c00 100
  fdtput -t x "${partial_path}" /soc/spi@F7E81C00 num-cs 4
  fdtput -t x "${partial_path}" /soc/spi@F7E81C00 clocks c
  fdtput -t x "${partial_path}" /soc/spi@F7E81C00 interrupt-parent a
  fdtput -t x "${partial_path}" /soc/spi@F7E81C00 interrupts 7
  fdtput -t x "${partial_path}" /soc/spi@F7E81C00 \
    '#address-cells' 1
  fdtput -t x "${partial_path}" /soc/spi@F7E81C00 \
    '#size-cells' 0

  fdtput -c "${partial_path}" /soc/spi@F7E81C00/spidev@0
  fdtput -t s "${partial_path}" /soc/spi@F7E81C00/spidev@0 \
    compatible spidev
  fdtput -t x "${partial_path}" /soc/spi@F7E81C00/spidev@0 reg 0
  fdtput -t x "${partial_path}" /soc/spi@F7E81C00/spidev@0 \
    spi-max-frequency f4240
  fdtput -t s "${partial_path}" /aliases spi0 /soc/spi@F7E81C00
  fdtput -t x "${partial_path}" \
    /soc/apbgpio@F7E80400/gpio-controller@0 base-gpio 0

  mv "${partial_path}" "${output_path}"
  trap - EXIT
  stat --format="%n %s bytes" "${output_path}"
  sha256sum "${output_path}"
}

main "$@"
