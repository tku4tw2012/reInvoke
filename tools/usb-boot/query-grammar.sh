#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT

query_command_allowed() {
  [[ $# -eq 1 ]] || return 1
  local command="$1"
  local LC_ALL=C
  local hex='(0[xX])?[0-9A-Fa-f]+'

  [[ ! "${command}" =~ [[:cntrl:]] ]] || return 1
  case "${command}" in
    version|bdinfo|mtdparts) return 0 ;;
  esac
  [[ "${command}" =~ ^printenv(\ +[-A-Za-z0-9_.]+)*$ ]] && return 0
  [[ "${command}" =~ ^help(\ +[A-Za-z_][A-Za-z0-9_.-]*)?$ ]] && return 0
  [[ "${command}" =~ ^nand\ +info$ ]] && return 0
  [[ "${command}" =~ ^nand\ +dump(\.oob)?\ +${hex}$ ]] && return 0
  [[ "${command}" =~ ^nandrd\ +${hex}\ +${hex}(\ +${hex})?$ ]] && return 0
  [[ "${command}" =~ ^md(\.[bwl])?\ +${hex}(\ +${hex})?$ ]] && return 0
  return 1
}
