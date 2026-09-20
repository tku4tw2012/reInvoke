#!/bin/sh
# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT
#
# Does g_android survive being removed and put back?
#
# Before 2.2.8 it did not: cleanup() destroyed the android_usb class without
# destroying the android0 device init() had created in it, so a reload met a
# leaked kobject. The fix adds that device_destroy. It has never run on
# hardware, which is what this script is for.
#
# The test takes ADB down with it either way, because removing g_android is
# removing the transport. So nothing here is reported over ADB: every line
# goes to /persist, which is yaffs2 on mtdblock11 and survives a reboot, and
# is synced before each step that could stop the machine. Read the log after,
# whether ADB comes back or you have to power cycle.
#
# panic_on_oops and panic are turned off first. This unit ships with both at 1,
# which turns a recoverable warning into a panic and a reboot one second later,
# taking the evidence with it. With them off, a failure that is only a warning
# leaves the kernel alive and the trace readable.
#
# Usage: usb-adb-reload-test.sh
set -u

BB=${BB:-/bin/busybox}
DIR=/opt/reinvoke/usb-adb
GADGET=/sys/class/android_usb/android0
UDC=/sys/class/udc/f7ed0100.udc
LOG=/persist/usb-adb-reload-test.log

note() {
  echo "$*" >>"${LOG}"
  ${BB} sync
}

[ -d /persist ] || { echo "no /persist; refusing to run blind" >&2; exit 1; }

note ""
note "=== reload test $(${BB} date -u '+%Y-%m-%dT%H:%M:%SZ') ==="
note "build      $(${BB} cat /etc/nand-pilot/build-id 2>/dev/null)"
note "g_android  $(${BB} sha256sum "${DIR}/g_android.ko" 2>/dev/null | ${BB} cut -c1-64)"
note "uptime     $(${BB} cat /proc/uptime 2>/dev/null)"
note "panic_on_oops=$(${BB} cat /proc/sys/kernel/panic_on_oops) panic=$(${BB} cat /proc/sys/kernel/panic)"

# Keep a warning from becoming a reboot.
echo 0 >/proc/sys/kernel/panic_on_oops 2>/dev/null
echo 0 >/proc/sys/kernel/panic 2>/dev/null
note "set to     panic_on_oops=$(${BB} cat /proc/sys/kernel/panic_on_oops) panic=$(${BB} cat /proc/sys/kernel/panic)"

# f_adb sets .owner = THIS_MODULE, so the module cannot leave while adbd holds
# the device open. This is where ADB goes away.
note "-- stopping adbd"
for proc in /proc/[0-9]*; do
  if ${BB} grep -qa adbd-root "${proc}/cmdline" 2>/dev/null; then
    ${BB} kill "$(${BB} basename "${proc}")" 2>/dev/null
  fi
done
${BB} sleep 3

${BB} test -e "${GADGET}/enable" && echo 0 >"${GADGET}/enable" 2>/dev/null
${BB} sleep 1

note "-- rmmod g_android (the removal has always been the safe half)"
${BB} rmmod g_android 2>>"${LOG}"
note "rmmod exit=$?  /sys/module/g_android present=$(${BB} test -e /sys/module/g_android && echo yes || echo no)"
${BB} dmesg | ${BB} tail -25 >>"${LOG}"
${BB} sync

# Everything above here was already known to work. This is the new part.
note "-- insmod g_android (this is what used to panic)"
${BB} insmod "${DIR}/g_android.ko" 2>>"${LOG}"
insmod_status=$?
note "insmod exit=${insmod_status}  /sys/module/g_android present=$(${BB} test -e /sys/module/g_android && echo yes || echo no)"
${BB} dmesg | ${BB} tail -40 >>"${LOG}"
${BB} sync

if [ "${insmod_status}" -ne 0 ]; then
  note "RESULT: reload failed, but the kernel is alive and the trace is above."
  note "Power cycle to get ADB back."
  exit 1
fi

# It loaded. Put the transport back so the result can be read over ADB.
note "-- restoring the transport"
echo disconnect >"${UDC}/soft_connect" 2>/dev/null
dev="$(${BB} cat /sys/class/misc/android_adb/dev 2>/dev/null)"
case "${dev}" in
  [0-9]*:[0-9]*)
    ${BB} test -e /dev/android_adb || ${BB} mknod /dev/android_adb c "${dev%%:*}" "${dev##*:}"
    ${BB} chmod 660 /dev/android_adb
    ;;
  *) note "misc device absent after reload; transport cannot come back"; exit 1 ;;
esac

mac="$(${BB} cat /sys/class/net/mlan0/address 2>/dev/null | ${BB} tr -d ':' | ${BB} tr 'a-f' 'A-F')"
if ${BB} test -n "${mac}"; then
  echo "${mac}" >"${GADGET}/iSerial"
  echo "reInvoke-$(echo "${mac}" | ${BB} cut -c7-12)" >"${GADGET}/iProduct"
  echo "Harman Kardon" >"${GADGET}/iManufacturer"
fi

echo adb >"${GADGET}/functions"
echo 1 >"${GADGET}/enable"
${BB} sleep 1
echo disconnect >"${UDC}/soft_connect" 2>/dev/null
${BB} sleep 1
echo connect >"${UDC}/soft_connect" 2>/dev/null
${BB} sleep 2

exec 9<"${DIR}/properties" || { note "properties unreadable"; exit 1; }
ANDROID_PROPERTY_WORKSPACE=9,32768 /sbin/adbd-root >/dev/null 2>&1 &
exec 9<&-

${BB} sleep 3
note "RESULT: reloaded. gadget state=$(${BB} cat "${GADGET}/state" 2>/dev/null)"
note "If you are reading this over ADB, the transport was rebuilt from nothing."
exit 0
