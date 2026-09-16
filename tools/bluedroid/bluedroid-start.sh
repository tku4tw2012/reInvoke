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
# The donor binary hardcodes both of these paths; they are not ours to choose.
# /etc/bluetooth/bt_stack.conf is read at startup and /data/misc/bluedroid is
# where the stack keeps bt_config.conf. Candidate 05.4 invented /data/bluetooth
# instead, so the stack found no config, config_new returned NULL and the first
# lookup dereferenced it: SIGSEGV in stack_manager, every five seconds forever.
CONFIG_LIVE=/data/misc/bluedroid
PERSIST_ROOT=/persist
PERSIST_CONFIG=/persist/reinvoke/bluedroid
STACK_CONF=/etc/bluetooth/bt_stack.conf
# Opt-in HCI capture. Creating SNOOP_FLAG turns it on at the next start; the
# capture and the rewritten config both live in tmpfs so neither reaches NAND.
SNOOP_FLAG=/persist/reinvoke/hci-capture
SNOOP_CONF=/run/reinvoke/bt_stack.snoop.conf
SNOOP_FILE=/run/reinvoke/logs/btsnoop_hci.log
HCI_DOWN=/bin/reinvoke-hci-down
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

# The donor drives the controller through the kernel's HCI user channel, which
# is only granted while the adapter is closed. Candidate 05.5 opened it first
# and the vendor transport could never attach: the stack ran, logged every
# property write as successful, and sent 44 bytes of HCI traffic, all header.
# Closing it first took the same build past 13000 bytes and gave working
# discovery, pairing and A2DP.
${BB} test -x "${HCI_DOWN}" || fail "the HCI handover helper is missing"
"${HCI_DOWN}" -index 0 || fail "hci0 could not be released for the donor stack"

# Read at startup from a path compiled into the donor binary.
${BB} test -f "${STACK_CONF}" ||
  fail "${STACK_CONF} is missing; the stack would load a NULL config"

# Opt-in HCI capture. The donor can log every HCI command and event, which is
# the only way to see what the radio was actually told: it is how the 05.8.1
# scan state was settled (Write Scan Enable 0x02, connectable but not
# discoverable) against a host scan that suggested otherwise.
#
# It stays off unless the flag file exists, and the capture is redirected into
# the runtime tmpfs. The donor never rotates this file, so a default-on capture
# would grow without bound on an appliance that is expected to run unattended,
# and pointing it at the vendor path would write that growth to NAND.
if ${BB} test -f "${SNOOP_FLAG}"; then
  if ${BB} sed -e 's#^BtSnoopLogOutput=false#BtSnoopLogOutput=true#' \
      -e "s#^BtSnoopFileName=.*#BtSnoopFileName=${SNOOP_FILE}#" \
      "${STACK_CONF}" >"${SNOOP_CONF}" &&
    ${BB} mount --bind "${SNOOP_CONF}" "${STACK_CONF}"; then
    echo "bluedroid: HCI capture enabled at ${SNOOP_FILE}"
  else
    echo "bluedroid: HCI capture could not be enabled; continuing without it"
  fi
fi

# The donor keeps writable copies separate from the read-only originals.
if ! ${BB} test -d "${CONFIG_LIVE}"; then
  ${BB} mkdir -p "${CONFIG_LIVE}" || fail "writable configuration is unavailable"
fi

# Bluetooth bonds live in bt_config.conf under CONFIG_LIVE, and that path is
# on the root tmpfs, so every pairing was lost at the next power cycle: the
# file went from 1482 bytes with three bond entries to 778 bytes with none,
# and the peer was then refused with br-connection-unknown. /persist is a
# writable YAFFS2 partition that does survive, so the donor's directory is
# bound onto it. A bind rather than a link: the donor opens these paths
# directly and a dangling link is indistinguishable from a missing file.
if ${BB} test -d "${PERSIST_ROOT}"; then
  ${BB} mkdir -p "${PERSIST_CONFIG}" || fail "persistent configuration is unavailable"
  # The guard has to compare the path the kernel reports, not the one written
  # here. /data is a symlink to /home/galois_rwdata, so the mount table lists
  # the resolved directory and a grep for CONFIG_LIVE never matched: every
  # restart stacked another identical bind. Observed on 05.8.1 with three
  # bluedroid starts and three nested binds, which the persistence service
  # then refused as PERSIST_MOUNT_CONFLICT, leaving no state.json at all.
  config_live_real="$(${BB} readlink -f "${CONFIG_LIVE}" 2>/dev/null)"
  [ -n "${config_live_real}" ] || config_live_real="${CONFIG_LIVE}"
  if ! ${BB} grep -q " ${config_live_real} " /proc/self/mounts; then
    ${BB} mount --bind "${PERSIST_CONFIG}" "${CONFIG_LIVE}" ||
      fail "persistent configuration could not be bound"
  fi
else
  echo "bluedroid: ${PERSIST_ROOT} is absent; pairings will not survive a reboot"
fi

# Seed the read-only originals only where the persistent copy has none, so an
# existing bt_config.conf and its bonds are never overwritten.
for original in "${CONFIG_SOURCE}"/*; do
  ${BB} test -f "${original}" || continue
  target="${CONFIG_LIVE}/$(${BB} basename "${original}")"
  ${BB} test -f "${target}" || ${BB} cp "${original}" "${target}" ||
    fail "writable configuration could not be populated"
done

echo "bluedroid: starting donor service against ${CONFIG_LIVE}"
exec ${BB} env LD_LIBRARY_PATH="${LIBS}" "${LOADER}" --library-path "${LIBS}" "${SERVICE}"
