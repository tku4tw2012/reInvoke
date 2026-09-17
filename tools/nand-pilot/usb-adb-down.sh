#!/bin/busybox sh
# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT
#
# Tear USB ADB down.
#
# The kernel already refuses the dangerous unloads on its own. Module
# dependency refcounting returns EBUSY for a module something else uses, and
# f_adb sets .owner = THIS_MODULE so the VFS holds a reference while adbd has
# /dev/android_adb open. Both were confirmed on hardware: rmmod udc_core with
# two users returned EBUSY and the kernel survived.
#
# What is not safe is re-inserting g_android after unloading it. That panicked
# this unit, and with no pstore or last_kmsg there is no log to say why. So
# this tears down and stops; bringing USB ADB back is a reboot, not a reload.
#
# Stopping adbd first is still the right order. It closes the descriptor
# through adb_release, which runs adb_closed_callback and removes the
# configuration, rather than leaving the kernel to refuse an unload by errno.
set -e

BB=${BB:-/bin/busybox}
USB_ADB_UDC=/sys/class/udc/f7ed0100.udc
USB_ADB_GADGET=/sys/class/android_usb/android0

say() { echo "usb-adb: $*"; }

say "stopping adbd"
for usb_adb_proc in /proc/[0-9]*; do
  if ${BB} grep -qa adbd-root "${usb_adb_proc}/cmdline" 2>/dev/null; then
    ${BB} kill "$(${BB} basename "${usb_adb_proc}")" 2>/dev/null || true
  fi
done
${BB} sleep 2

# Confirm the descriptor is gone before touching modules. The kernel would
# refuse anyway; failing here says why instead of returning an errno.
usb_adb_held=""
usb_adb_wait=0
while ${BB} test "${usb_adb_wait}" -lt 15; do
  usb_adb_held=""
  for usb_adb_proc in /proc/[0-9]*; do
    ${BB} ls -l "${usb_adb_proc}/fd" 2>/dev/null | ${BB} grep -q android_adb &&
      usb_adb_held="$(${BB} basename "${usb_adb_proc}")"
  done
  ${BB} test -z "${usb_adb_held}" && break
  usb_adb_wait=$((usb_adb_wait + 1))
  ${BB} sleep 1
done
if ${BB} test -n "${usb_adb_held}"; then
  say "refusing to unload: pid ${usb_adb_held} still holds /dev/android_adb"
  exit 1
fi

say "removing the configuration and dropping the pullup"
${BB} test -e "${USB_ADB_GADGET}/enable" && echo 0 > "${USB_ADB_GADGET}/enable" 2>/dev/null || true
${BB} sleep 1
${BB} test -e "${USB_ADB_UDC}/soft_connect" &&
  echo disconnect > "${USB_ADB_UDC}/soft_connect" 2>/dev/null || true
${BB} sleep 1

say "unloading in reverse dependency order"
for usb_adb_module in g_android usb_f_acm u_serial libcomposite mv_udc udc_core; do
  ${BB} test -e "/sys/module/${usb_adb_module}" &&
    ${BB} rmmod "${usb_adb_module}" 2>/dev/null || true
done

${BB} test -e /dev/android_adb && ${BB} rm /dev/android_adb 2>/dev/null || true

say "returning the port to the EHCI host driver"
${BB} test -e /sys/bus/platform/drivers/berlin-ehci/f7ed0000.usb ||
  echo f7ed0000.usb > /sys/bus/platform/drivers/berlin-ehci/bind 2>/dev/null || true

say "down. bringing it back requires a reboot, not a reload"
