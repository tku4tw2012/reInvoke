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

# Usable two ways: sourced by init, which supplies these helpers, or run
# directly as `usb-adb-start.sh up|down|status` from a shell. The second is
# why the shims below exist. Keeping one implementation for both is the point:
# a standalone teardown script used to sit beside this one with its own copy
# of the sequence, and two copies of an ordering this fussy will drift.
BB=${BB:-/bin/busybox}
PILOT_STATE=${PILOT_STATE:-/run/nand-pilot}

# Two state directories, and they are not interchangeable. PILOT_STATE is the
# pilot's own, set by common.sh to /run/nand-pilot: boot progression, the
# entry evidence, and the transports init owns. The runtime services keep
# theirs in /run/reinvoke. Nothing here should write a log of its own into
# either; log() already reaches the pilot's boot.log, which is where a record
# made by init belongs.
# Whether init is above us.
#
# Two earlier attempts at this both failed silently, which is worse than
# failing. Matching the script's own name meant renaming it stopped the
# dispatcher and still exited 0. Testing for `log` looked safer until the
# device turned out to ship /bin/log, so the sentinel was satisfied by a
# binary that has nothing to do with init.
#
# pilot_failure is a shell function init defines and nothing on the
# filesystem provides, so it cannot be answered by accident.
if command -v pilot_failure >/dev/null 2>&1; then
  usb_adb_sourced=yes
else
  usb_adb_sourced=no
fi
# Shadow /bin/log deliberately when standalone: its output goes somewhere
# this operator cannot see.
if [ "${usb_adb_sourced}" = "no" ]; then
  log() { echo "usb-adb: $*"; }
fi
command -v pilot_failure >/dev/null 2>&1 || pilot_failure() {
  echo "usb-adb: FAILED $1: $2" >&2
}

# Leave a record where the boot log is.
#
# runtime.log is fed by the supervise wrapper, one pipe per service, and USB
# ADB is not a supervised service: it is brought up by init directly. So its
# log() went to init's stdout and nowhere that outlives the boot, and asking
# afterwards whether init had brought ADB up had no answer but inference from
# the fact that adb worked. Everything else in this file exists because a
# failure was observed; a step that cannot be observed at all is worse.
usb_adb_record() {
  # log() reaches the pilot's boot.log through pilot_log, which is where a
  # record made by init belongs and where this one has been all along:
  #
  #   29.29 runtime: ready: state=CONFIGURED
  #
  # Three builds were spent believing it was missing, because the search was
  # for "usb" and pilot_log writes the prefix "runtime:". Nothing was broken;
  # the wrong file was being read, and then the wrong string.
  #
  # Also to the kernel buffer. That is what finally settled it, and it costs
  # nothing to keep: it survives whatever happens to a filesystem and reads
  # back with dmesg.
  log "$*"
  echo "reinvoke-usb-adb: $*" >/dev/kmsg 2>/dev/null
  return 0
}

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
    usb_adb_record "disabled by persistent marker"
    return 0
  fi

  # Already up is not a reason to do anything. Running this a second time used
  # to re-run the soft_connect toggle underneath a host that had already
  # enumerated, which left the gadget DISCONNECTED and took the transport away
  # from whoever was using it. Asking for a state you are already in should
  # never be how you leave it.
  if ${BB} test "$(${BB} cat "${USB_ADB_GADGET}/state" 2>/dev/null)" = CONFIGURED &&
     pilot_usb_adb_holder >/dev/null; then
    usb_adb_record "already up: state=CONFIGURED"
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

  # Identify this speaker before enabling the gadget. The Android gadget
  # driver ships a compiled-in serial of 0123456789ABCDEF and a product string
  # of "Android", so every unit running this build would be indistinguishable
  # in adb devices and adb -s could not address one of several. The Wi-Fi MAC
  # is the identity this runtime already publishes as its Bluetooth name, so
  # the same value is used here.
  # The radio modules load before this runs, so the interface exists here even
  # when it never associates, which is the case USB ADB is for.
  usb_adb_mac="$(${BB} cat /sys/class/net/mlan0/address 2>/dev/null |
    ${BB} tr -d ':' | ${BB} tr 'a-f' 'A-F')"
  if ${BB} test -n "${usb_adb_mac}"; then
    echo "${usb_adb_mac}" > "${USB_ADB_GADGET}/iSerial"
    # The product string is the name this speaker already answers to over WAMP
    # and advertises over Bluetooth, so one unit reads the same everywhere.
    echo "reInvoke-$(echo "${usb_adb_mac}" | ${BB} cut -c7-12)" \
      > "${USB_ADB_GADGET}/iProduct"
    # All three descriptor strings are placeholders until something fills them
    # in. android_bind writes "Android", "Android" and "0123456789ABCDEF" into
    # them, and exposes these attributes so the product replaces them; a device
    # still reporting those has simply never been configured.
    echo "Harman Kardon" > "${USB_ADB_GADGET}/iManufacturer"
  else
    log "USB ADB identity unavailable; gadget keeps the driver defaults"
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
  usb_adb_record "ready: state=$(${BB} cat "${USB_ADB_GADGET}/state" 2>/dev/null)"
  return 0
}

pilot_usb_adb_down() {
  # Symmetrically: nothing loaded is nothing to tear down.
  if ! ${BB} test -e /sys/module/g_android; then
    usb_adb_record "already down"
    return 0
  fi
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

  # Give the port back to the EHCI host driver. pilot_usb_adb_up unbinds it
  # again on the way in, so this is the other half of a pair rather than a
  # one-way action, and leaving it out is what made a teardown followed by a
  # bring-up not equal to a boot. The standalone teardown script did do this;
  # the copy here did not, which is the drift that comes of two copies.
  if ! ${BB} test -e /sys/bus/platform/drivers/berlin-ehci/f7ed0000.usb; then
    echo f7ed0000.usb > /sys/bus/platform/drivers/berlin-ehci/bind 2>/dev/null ||
      log "EHCI rebind reported a problem; the port may need a reboot"
  fi

  usb_adb_record "torn down"
  return 0
}

pilot_usb_adb_status() {
  ${BB} test -e /sys/module/g_android && echo "module: loaded" || echo "module: absent"
  if holder="$(pilot_usb_adb_holder)"; then
    echo "adbd:   holding /dev/android_adb as pid ${holder}"
  else
    echo "adbd:   not holding the device"
  fi
  echo "gadget: $(${BB} cat "${USB_ADB_GADGET}/state" 2>/dev/null || echo absent)"
  # The leftover that used to survive a teardown and block the next load.
  ${BB} test -e /sys/devices/soc.0/f7ed0100.udc/gadget/lun0 &&
    echo "lun0:   present" || echo "lun0:   absent"
}

# Only when executed, never when sourced. init sources this file at its top
# and would otherwise run the dispatcher before it had defined anything.
if [ "${usb_adb_sourced}" = "no" ]; then
  case "${1:-status}" in
    up)     pilot_usb_adb_up ;;
    down)   pilot_usb_adb_down ;;
    status) pilot_usb_adb_status ;;
    cycle)  pilot_usb_adb_down; ${BB} sleep 2; pilot_usb_adb_up ;;
    *)      echo "usage: ${0##*/} up|down|cycle|status" >&2; exit 2 ;;
  esac
  exit $?
fi
