#!/bin/busybox sh
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
#
# Start the donor Bluedroid stack.
#
# This mirrors the donor's usr/bin/bluetooth.sh with two deliberate omissions:
#   - no Android logwrapper: this runtime has a working syslog socket, and the
#     service inherits the supervisor's pipe;
#   - no insmod: the runtime already loads bt8xxx with the pinned firmware, and
#     reloading it would drop an initialised controller.
#
# The donor service is glibc-linked against its own closure, so it is invoked
# through the packaged loader with an explicit library path rather than relying
# on whatever happens to satisfy its DT_NEEDED list at runtime.

BB=/bin/busybox
ROOT=/opt/bluedroid
CONFIG_SOURCE="${ROOT}/etc/bluetooth_orig"
CONFIG_LIVE=/data/bluetooth
LOADER="${ROOT}/lib/ld-linux-armhf.so.3"
SERVICE="${ROOT}/usr/bin/bluetooth"
# The Android HAL libraries now install to the absolute /system/lib that
# libhardware.so hardcodes for hw_get_module; only the donor's private glibc
# and its own libraries stay under ROOT. Both are on the path because the
# service links against libcutils/libutils/libhardware directly as well as
# loading the HAL module by path.
LIBS="/system/lib:/system/lib/hw:${ROOT}/usr/lib:${ROOT}/lib"

fail() {
  echo "bluedroid: $*"
  exit 1
}

${BB} test -d "${CONFIG_SOURCE}" || fail "packaged configuration is missing"
${BB} test -x "${SERVICE}" || fail "packaged service is missing"
${BB} test -x "${LOADER}" || fail "packaged loader is missing"

# The controller must already be up; this script never touches the module.
${BB} test -d /sys/class/bluetooth/hci0 ||
  fail "hci0 is not present; the radio is not initialized"

# The donor keeps writable copies separate from the read-only originals.
if ! ${BB} test -d "${CONFIG_LIVE}"; then
  ${BB} mkdir -p "${CONFIG_LIVE}" || fail "writable configuration is unavailable"
  ${BB} cp "${CONFIG_SOURCE}"/* "${CONFIG_LIVE}/" ||
    fail "writable configuration could not be populated"
fi

echo "bluedroid: starting donor service against ${CONFIG_LIVE}"
exec ${BB} env LD_LIBRARY_PATH="${LIBS}" "${LOADER}" --library-path "${LIBS}" "${SERVICE}"
