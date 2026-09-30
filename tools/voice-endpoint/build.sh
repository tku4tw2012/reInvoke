#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Build the owned voice endpoint using an explicitly supplied private bundle.

set -euo pipefail

main() {
  local script_dir
  script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
  exec "${PYTHON:-python3}" "${script_dir}/build.py" "$@"
}

main "$@"
