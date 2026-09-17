#!/bin/busybox sh
# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT
#
# Bring up USB ADB.
#
# Ordering is the whole problem here, and every step exists because something
# was observed to fail without it. docs/usb-adb.md carries the detail.
#
# The port is shared. The EHCI host at f7ed0000 and the device controller at
# f7ed0100 are one hardware block. The device driver selects device mode with
#     tmp = readl(usbmode); tmp |= USBMODE_CTRL_MODE_DEVICE;
# which is an OR. Host mode is 3 and device mode is 2, so if EHCI has already
# claimed the block that OR leaves it in host mode and the controller never
# answers as a device.
#
# The gadget withholds its configuration until adbd opens the device. f_adb
# does this deliberately:
#     /* Disable the gadget until adbd is ready */
#     if (!data->opened) android_disable(dev);
# So adbd must hold the device open before the gadget is enabled, or
# disable_depth never reaches zero, no configuration is added, and a host sees
# a device advertising none.
#
# /dev/android_adb only appears once the composite binds to a UDC, and that
# same bind asserts the D+ pullup. That leaves a window where a host enumerates
# a configuration-less device, answers "can't read configurations", and then
# latches its port off until the cable is physically replugged. The pullup is
# dropped immediately and re-asserted only once the configuration exists.

USB_ADB_DIR=${USB_ADB_DIR:-/opt/reinvoke/usb-adb}
USB_ADB_UDC=/sys/class/udc/f7ed0100.udc
USB_ADB_GADGET=/sys/class/android_usb/android0

pilot_usb_adb_disabled() {
  # A persistent marker turns USB ADB off without removing it, so it can be
  # switched back on without reflashing.
  ${BB} test -f /persist/reinvoke/usb-adb-disabled
}

pilot_usb_adb_holder() {
  for usb_adb_proc in /proc/[0-9]*; do
    if ${BB} ls -l "${usb_adb_proc}/fd" 2>/dev/null | ${BB} grep -q android_adb; then
      ${BB} basename "${usb_adb_proc}"
      return 0
    fi
  done
  return 1
}

pilot_usb_adb_up() {
  if pilot_usb_adb_disabled; then
    log "USB ADB disabled by persistent marker"
    return 0
  fi
  ${BB} test -d "${USB_ADB_DIR}" || {
    pilot_failure usb-adb "payload-missing"
    return 1
  }

  # Release the port before the device driver touches it.
  if ${BB} test -e /sys/bus/platform/drivers/berlin-ehci/f7ed0000.usb; then
    echo f7ed0000.usb > /sys/bus/platform/drivers/berlin-ehci/unbind || {
      pilot_failure usb-adb "ehci-unbind-failed"
      return 1
    }
  fi

  for usb_adb_module in udc-core mv_udc libcomposite u_serial usb_f_acm g_android; do
    usb_adb_loaded="$(echo "${usb_adb_module}" | ${BB} tr - _)"
    ${BB} test -e "/sys/module/${usb_adb_loaded}" && continue
    ${BB} insmod "${USB_ADB_DIR}/${usb_adb_module}.ko" || {
      pilot_failure usb-adb "insmod-${usb_adb_module}"
      return 1
    }
  done

  # Drop D+ at once. Anything slower lets a host enumerate a
  # configuration-less device and latch its port off.
  echo disconnect > "${USB_ADB_UDC}/soft_connect" 2>/dev/null

  usb_adb_dev="$(${BB} cat /sys/class/misc/android_adb/dev 2>/dev/null)"
  case "${usb_adb_dev}" in
    [0-9]*:[0-9]*) ;;
    *) pilot_failure usb-adb "misc-device-absent"; return 1 ;;
  esac
  ${BB} test -e /dev/android_adb ||
    ${BB} mknod /dev/android_adb c "${usb_adb_dev%%:*}" "${usb_adb_dev##*:}" || {
      pilot_failure usb-adb "mknod-failed"
      return 1
    }
  ${BB} chmod 660 /dev/android_adb

  # The property area is inherited as a descriptor, never opened by path. This
  # one carries no service.adb.tcp.port, which is what selects USB over TCP.
  exec 9<"${USB_ADB_DIR}/properties" || {
    pilot_failure usb-adb "properties-unreadable"
    return 1
  }
  ANDROID_PROPERTY_WORKSPACE=9,32768 /sbin/adbd-root >/dev/null 2>&1 &
  echo "$!" >"${PILOT_STATE}/usb-adbd.pid"
  exec 9<&-

  # Wait for adbd to actually hold the device rather than assuming it did.
  usb_adb_wait=0
  usb_adb_pid=""
  while ${BB} test "${usb_adb_wait}" -lt 30; do
    usb_adb_pid="$(pilot_usb_adb_holder)" && break
    usb_adb_wait=$((usb_adb_wait + 1))
    ${BB} sleep 1
  done
  if ${BB} test -z "${usb_adb_pid}"; then
    pilot_failure usb-adb "adbd-never-opened-device"
    return 1
  fi

  echo adb > "${USB_ADB_GADGET}/functions" || return 1
  echo 1 > "${USB_ADB_GADGET}/enable" || return 1
  ${BB} sleep 1

  # A bare connect does not restart the controller: mv_udc_pullup returns early
  # when softconnect already holds the value being written, so this has to be a
  # real transition or the run bit stays clear with everything else looking
  # correct.
  echo disconnect > "${USB_ADB_UDC}/soft_connect" 2>/dev/null
  ${BB} sleep 1
  echo connect > "${USB_ADB_UDC}/soft_connect" 2>/dev/null
  ${BB} sleep 2

  echo usb >"${PILOT_STATE}/adb-transport"
  log "USB ADB ready: state=$(${BB} cat "${USB_ADB_GADGET}/state" 2>/dev/null)"
  return 0
}

pilot_usb_adb_down() {
  # Teardown runs on the shutdown path. adbd sleeps inside the gadget driver
  # and the driver holds the USB controller; leaving both through shutdown left
  # this unit unable to complete a soft reboot and it had to be power cycled.
  #
  # The kernel already refuses the dangerous unloads on its own: module
  # dependency refcounting returns EBUSY for a module still in use, and f_adb
  # sets .owner = THIS_MODULE so the VFS holds a reference while adbd has the
  # device open. Both were confirmed on hardware. Stopping adbd first is still
  # the right order, because it closes the descriptor through adb_release
  # rather than leaving the kernel to refuse by errno.
  #
  # Every step is best effort. A speaker that cannot stop its debug channel
  # must still be able to reboot.
  ${BB} test -e /sys/module/g_android || return 0

  for usb_adb_proc in /proc/[0-9]*; do
    if ${BB} grep -qa adbd-root "${usb_adb_proc}/cmdline" 2>/dev/null; then
      ${BB} kill "$(${BB} basename "${usb_adb_proc}")" 2>/dev/null
    fi
  done

  usb_adb_wait=0
  while ${BB} test "${usb_adb_wait}" -lt 10; do
    pilot_usb_adb_holder >/dev/null || break
    usb_adb_wait=$((usb_adb_wait + 1))
    ${BB} sleep 1
  done

  echo 0 > "${USB_ADB_GADGET}/enable" 2>/dev/null
  echo disconnect > "${USB_ADB_UDC}/soft_connect" 2>/dev/null
  ${BB} sleep 1

  for usb_adb_module in g_android usb_f_acm u_serial libcomposite mv_udc udc_core; do
    ${BB} test -e "/sys/module/${usb_adb_module}" &&
      ${BB} rmmod "${usb_adb_module}" 2>/dev/null
  done
  ${BB} test -e /dev/android_adb && ${BB} rm -f /dev/android_adb 2>/dev/null
  log "USB ADB torn down"
  return 0
}
