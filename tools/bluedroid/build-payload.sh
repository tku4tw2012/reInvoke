#!/usr/bin/env bash
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Package the donor Bluedroid userspace closure as a pinned payload.
#
# The kernel side is already present on the installed image: the bt8xxx module
# is loaded and lib/firmware/mrvl/sd8887_bt_a2_new.bin ships with the runtime.
# Only the glibc-linked userspace is missing, so that is all this collects.
#
# Usage: build-payload.sh DONOR_ROOTFS OUTPUT_DIR

set -euo pipefail

donor="${1:?DONOR_ROOTFS}"
out="${2:?OUTPUT_DIR}"
[[ -d "${donor}" ]] || { echo "donor rootfs not found: ${donor}" >&2; exit 1; }
[[ -e "${out}" ]] && { echo "OUTPUT_DIR must not already exist" >&2; exit 1; }
mkdir -p "${out}"; chmod 0700 "${out}"

# Resolve the transitive DT_NEEDED closure from the donor tree itself rather
# than a hand-written list; a missing entry is a hard error, not a warning.
python3 - "${donor}" "${out}/closure.txt" <<'PY'
import os, subprocess, sys
donor, listing = sys.argv[1], sys.argv[2]
index = {}
for base, _, files in os.walk(donor):
    for name in files:
        if '.so' in name:
            index.setdefault(name, os.path.join(base, name))
def needed(path):
    out = subprocess.run(['readelf', '-d', path], capture_output=True, text=True).stdout
    return [line.split('[')[1].split(']')[0] for line in out.splitlines() if 'NEEDED' in line]
roots = ['usr/bin/bluetooth', 'system/lib/hw/bluetooth.default.so', 'system/lib/libbt-vendor.so']
files, queue, seen, missing = set(), [], set(), set()
for rel in roots:
    full = os.path.join(donor, rel)
    if not os.path.exists(full):
        sys.exit(f'donor is missing a required object: {rel}')
    files.add(full); queue.append(full)
while queue:
    for name in needed(queue.pop()):
        if name in seen:
            continue
        seen.add(name)
        found = index.get(name)
        if found:
            files.add(found); queue.append(found)
        else:
            missing.add(name)
if missing:
    sys.exit('unresolved donor dependencies: ' + ', '.join(sorted(missing)))
with open(listing, 'w') as handle:
    handle.write('\n'.join(sorted(os.path.relpath(f, donor) for f in files)) + '\n')
print(f'closure objects: {len(files)}')
PY

# The donor reads these by absolute path at startup. Packaging shipped only
# bt_stack.conf, so bt_did.conf and auto_pair_devlist.conf were silently lost:
# the same defect class as the relocated HAL and the missing stack config, and
# the third time it has cost a candidate. Every file the donor names must be in
# the payload, and a missing one has to fail the build rather than the radio.
configs=(
  'etc/bluetooth_orig/bt_stack.conf'
  'etc/bluetooth_orig/bt_did.conf'
  'etc/bluetooth_orig/auto_pair_devlist.conf'
)
for config in "${configs[@]}"; do
  [[ -f "${donor}/${config}" ]] || { echo "donor is missing ${config}" >&2; exit 1; }
done

# Deterministic archive: sorted members, fixed owner and timestamp.
tar --create --gzip --dereference \
  --owner=0 --group=0 --numeric-owner --mtime='@0' --sort=name \
  --directory "${donor}" --file "${out}/bluedroid.tar.gz" \
  --files-from "${out}/closure.txt" "${configs[@]}"

python3 - "${donor}" "${out}" <<'PY'
import hashlib, json, os, sys
donor, out = sys.argv[1], sys.argv[2]
def digest(path):
    return hashlib.sha256(open(path, 'rb').read()).hexdigest()
members = [line.strip() for line in open(os.path.join(out, 'closure.txt')) if line.strip()]
members.extend([
    'etc/bluetooth_orig/bt_stack.conf',
    'etc/bluetooth_orig/bt_did.conf',
    'etc/bluetooth_orig/auto_pair_devlist.conf',
])
archive = os.path.join(out, 'bluedroid.tar.gz')
manifest = {
    'purpose': 'donor Bluedroid userspace closure; kernel module and controller firmware already ship in the runtime',
    'archive': {'file': 'bluedroid.tar.gz', 'bytes': os.path.getsize(archive), 'sha256': digest(archive)},
    'memberCount': len(members),
    'members': [{'path': m, 'bytes': os.path.getsize(os.path.join(donor, m)),
                 'sha256': digest(os.path.join(donor, m))} for m in sorted(members)],
    'status': 'PACKAGED_NOT_EXECUTED',
}
with open(os.path.join(out, 'MANIFEST.json'), 'w') as handle:
    json.dump(manifest, handle, indent=2)
    handle.write('\n')
print(json.dumps({'archive': manifest['archive'], 'memberCount': manifest['memberCount']}, indent=2))
PY
