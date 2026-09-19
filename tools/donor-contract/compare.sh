#!/usr/bin/env bash
# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT
#
# Report which donor WAMP procedures this runtime does not implement.
#
# The donor's behaviour is expressed as a WAMP contract: every vendor service
# registers or calls procedures under com.harman or com.cortana. Comparing that
# contract against ours turns "what did the vendor solve that we reinvented?"
# into a list instead of a recollection. Candidate 05.8.9 shipped a volume
# control that slammed the DSP to each new value; the vendor had
# VolumeManager::fade_step and restore_default_volume, which this would have
# surfaced before the work rather than after.
#
# Usage: compare.sh DONOR_ROOTFS [REPO]
set -euo pipefail
donor="${1:?DONOR_ROOTFS}"
repo="${2:-$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)}"
[[ -d "${donor}" ]] || { echo "donor rootfs not found: ${donor}" >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

# Map every procedure to the donor services that mention it.
for binary in "${donor}"/usr/bin/* "${donor}"/system/bin/*; do
  [[ -f "${binary}" ]] || continue
  file "${binary}" 2>/dev/null | grep -q ELF || continue
  name="$(basename "${binary}")"
  strings "${binary}" 2>/dev/null \
    | { grep -oE "com\.(harman|cortana)\.[a-zA-Z0-9_.-]+" || true; } | sort -u \
    | while read -r procedure; do printf '%s\t%s\n' "${procedure}" "${name}"; done
done >"${work}/donor.tsv"

# Drop namespace prefixes. The donor builds some procedure names at runtime by
# concatenating a prefix with a method, so strings like "com.harman.aui." are
# present in the binary as string-building material, not as procedures. Counting
# them produced six phantom gaps that no amount of implementing could ever
# close, because there is nothing behind them to implement.
#
# A prefix is a string ending in a dot, or a bare two-label namespace root that
# also appears as the prefix of a real procedure in the same binary.
cut -f1 "${work}/donor.tsv" | sort -u >"${work}/donor-raw.txt"
: >"${work}/donor.txt"
: >"${work}/prefixes.txt"
while read -r procedure; do
  if [[ "${procedure}" == *. ]]; then
    printf '%s\n' "${procedure}" >>"${work}/prefixes.txt"
    continue
  fi
  # A bare root with real procedures under it is a prefix, not a procedure.
  if grep -qE "^${procedure}\.[a-zA-Z0-9_-]" "${work}/donor-raw.txt"; then
    printf '%s\n' "${procedure}" >>"${work}/prefixes.txt"
    continue
  fi
  printf '%s\n' "${procedure}" >>"${work}/donor.txt"
done <"${work}/donor-raw.txt"
{ grep -rhoE '"com\.(harman|cortana|reinvoke)\.[a-zA-Z0-9_.-]+"' \
  "${repo}/tools" --include='*.go' 2>/dev/null || true; } \
  | tr -d '"' | sort -u >"${work}/ours-source.txt"

# Add the procedures that shipped donor binaries answer for us.
#
# This runtime ships parts of the donor stack rather than reimplementing them,
# so scanning our Go source alone counts those as gaps. It reported the nine
# com.harman.bluetooth transport controls as unimplemented while the donor
# Bluedroid stack we ship was registering seven of them on the device. A gap
# list that names things already working is one nobody can act on.
: >"${work}/donor-provided.txt"
if [[ -n "${DONOR_SHIPPED_DIRS:-}" ]]; then
  for shipped in ${DONOR_SHIPPED_DIRS}; do
    [[ -d "${shipped}" ]] || continue
    find "${shipped}" -type f -exec sh -c \
      'file "$1" 2>/dev/null | grep -q ELF' _ {} \; -print 2>/dev/null \
      | while read -r binary; do
          strings "${binary}" 2>/dev/null \
            | { grep -oE "com\.(harman|cortana)\.[a-zA-Z0-9_.-]+" || true; }
        done >>"${work}/donor-provided.txt"
  done
fi
sort -u "${work}/donor-provided.txt" -o "${work}/donor-provided.txt"
cat "${work}/ours-source.txt" "${work}/donor-provided.txt" | sort -u >"${work}/ours.txt"

printf 'donor procedures : %s\n' "$(wc -l <"${work}/donor.txt")"
printf 'ours             : %s\n' "$(wc -l <"${work}/ours.txt")"
printf 'unimplemented    : %s\n' \
  "$(comm -23 "${work}/donor.txt" "${work}/ours.txt" | wc -l)"
printf 'namespace prefixes excluded : %s\n' \
  "$(sort -u "${work}/prefixes.txt" | wc -l)"
printf 'answered by shipped donor binaries : %s\n\n' \
  "$(comm -12 "${work}/donor.txt" "${work}/donor-provided.txt" | wc -l)"

printf 'excluded as namespace prefixes:\n'
sort -u "${work}/prefixes.txt" | sed 's/^/    /'
printf '\n'

# Group the gaps by the service that owns them so triage is by subsystem.
cut -f2 "${work}/donor.tsv" | sort -u | while read -r service; do
  gaps="$(awk -F'\t' -v s="${service}" '$2==s {print $1}' "${work}/donor.tsv" \
    | sort -u | comm -12 - "${work}/donor.txt" \
    | comm -23 - "${work}/ours.txt" || true)"
  [[ -n "${gaps}" ]] || continue
  printf '=== %s ===\n' "${service}"
  printf '%s\n' "${gaps}" | sed 's/^/    /'
  printf '\n'
done
