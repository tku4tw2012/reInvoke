#!/bin/bash
# Keep one USB boot helper camped until the NAND write completes.
#
# Lives in the repository, not /tmp: a /tmp wipe silently disarmed the whole rig
# mid-session and the next power cycle had nothing listening for it.
#
# No timeouts here. The pinned helper binary has its own hardcoded 120 second
# device wait and exits when it expires, so this relaunches it immediately and
# indefinitely. Exactly one helper runs at a time: two both claim interface 0
# and the device then drops every transfer.
#
# Nothing is gated on USB subclass. The helper serves whatever the device asks
# for, and the console driver reacts to the U-Boot prompt when it appears.
set -u

here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "${here}/../.." && pwd)"
A="${REINVOKE_ARCHIVE:-${repo}/../reinvoke-archive}"
H="${A}/tools/hk-invoke-arm-flasher/63444e82/usb_boot_arm"
S="${A}/staging/recovery-051-20260914"
D="${here}/nand-console-driver.py"
KEEPER="${A}/tools/keep-helper-free"  # compiled artifact, built from tools/keep-helper-free
LOG="${A}/evidence/native056-flash-20260914/arm.log"

mkdir -p "$(dirname "${LOG}")"
: >"${LOG}"

python3 -u "${D}" --port 8141 --timeout 999999 >>"${LOG}" 2>&1 &
driver=$!
echo "driver ${driver}" >>"${LOG}"

# With a bootable image on NAND the ROM only offers USB for about two seconds,
# where it used to wait indefinitely. The helper cannot see that window while it
# is committed to an ordinary 0xFE boot, so this releases it back to detection
# and then keeps its hands off entirely once the window is caught.
keeper=""
if [[ -x "${KEEPER}" ]]; then
  "${KEEPER}" >>"${LOG}" 2>&1 &
  keeper=$!
  echo "keeper ${keeper}" >>"${LOG}"
fi

cleanup() {
  kill "${driver}" 2>/dev/null || true
  [[ -n "${keeper}" ]] && kill "${keeper}" 2>/dev/null || true
}
trap cleanup EXIT

while true; do
  if grep -q "u2nand succeed" "${LOG}" 2>/dev/null; then
    echo "=== FLASH COMPLETE $(date -u +%H:%M:%S) ===" >>"${LOG}"
    exit 0
  fi
  "${H}" 1286 8174 "${S}/" 8141 >>"${LOG}" 2>&1
  sleep 0.2
done
