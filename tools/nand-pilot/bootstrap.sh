#!/bin/busybox sh
# Copyright (c) 2026 tku4tw2012
# SPDX-License-Identifier: MIT

BB=/bin/busybox
PATH=/sbin:/bin:/usr/sbin:/usr/bin
HOME=/root
export PATH HOME
. /usr/libexec/reinvoke/common.sh

if [ "$$" -ne 1 ]; then
  echo "NAND pilot bootstrap requires PID 1; refusing host/manual execution" >&2
  exit 1
fi

${BB} mount -t proc proc /proc || pilot_fatal "cannot mount proc"
${BB} mount -t sysfs sysfs /sys || pilot_fatal "cannot mount sysfs"
${BB} mount -t tmpfs -o mode=0755,size=4m tmpfs /dev ||
  pilot_fatal "cannot create RAM devices"
for node in 'null 1 3' 'zero 1 5' 'random 1 8' 'urandom 1 9' \
  'console 5 1' 'tty 5 0' 'ptmx 5 2' 'kmsg 1 11'; do
  set -- ${node}
  ${BB} mknod -m 0600 "/dev/$1" c "$2" "$3"
done
${BB} chmod 0666 /dev/null /dev/zero /dev/random /dev/urandom /dev/tty /dev/ptmx
${BB} mkdir -p /dev/pts
${BB} mount -t tmpfs -o mode=0755,size=16m tmpfs /run ||
  pilot_fatal "cannot mount bounded diagnostic RAM"
${BB} mkdir -p /run/reinvoke /run/reinvoke /run/reinvoke/logs \
  /run/adb/adb /run/adb/local/tmp
${BB} chmod 0700 /run/reinvoke /run/reinvoke /run/adb
PILOT_ADBD_PTY_READY=1
if ! ${BB} mount -t devpts devpts /dev/pts; then
  PILOT_ADBD_PTY_READY=0
  pilot_failure pty "devpts unavailable; continuing without terminal diagnostics"
fi
${BB} mount -t tmpfs -o mode=1777,size=16m tmpfs /tmp ||
  pilot_fatal "cannot mount diagnostic scratch RAM"
exec </dev/console >/dev/console 2>&1
${BB} cat /proc/self/mountinfo >/run/reinvoke/entry-mountinfo
${BB} cat /proc/cmdline >/run/reinvoke/entry-cmdline
${BB} uname -r >/run/reinvoke/entry-kernel
${BB} cat /proc/sys/kernel/random/boot_id >/run/reinvoke/entry-boot-id
pilot_phase bootstrap

# A real SquashFS root cannot be switch_root's disposable initramfs root.
# Leave it mounted read-only and give PID 1 an explicit chroot into tmpfs.
if ${BB} awk '$5 == "/" && $0 ~ / - squashfs / {found=1} END {exit !found}' \
    /proc/self/mountinfo; then
  ${BB} mount -o remount,ro / || pilot_fatal "cannot enforce read-only source"
fi

# The early USB ADB launcher is gone. It was written when the runtime booted
# from RAM over USB and the boot ROM had already put the port in device mode,
# so a gadget existed before the runtime started. Booting from NAND there is no
# gadget until the runtime loads one, and this launcher spent the whole boot
# polling for it, then reconfigured the gadget out from under the runtime the
# moment it appeared. Observed on hardware as the runtime reporting
# "USB ADB ready" at 27.68s while this loop went on to log
# "retry-budget-exhausted" at 41.41s, having rewritten functions and iProduct
# in between. The runtime owns USB ADB now; see usb-adb-start.sh.

. /etc/reinvoke/payload.conf
pilot_verify_payload /payload/runtime.cpio.gz "${PAYLOAD_SHA256}" "${PAYLOAD_BYTES}" ||
  pilot_fatal "runtime payload size/hash mismatch"
pilot_phase payload-verified
${BB} mount -t tmpfs -o mode=0755,size=160m tmpfs /runtime ||
  pilot_fatal "cannot allocate RAM runtime root"
pilot_extract_payload /payload/runtime.cpio.gz /runtime ||
  pilot_fatal "payload decompression/extraction failed"
${BB} test -x /runtime/bin/busybox && ${BB} test -x /runtime/init ||
  pilot_fatal "runtime executable closure missing"
pilot_phase payload-extracted

for target in proc sys dev run tmp; do
  ${BB} mount -o bind "/${target}" "/runtime/${target}" ||
    pilot_fatal "cannot bind ${target} into runtime"
done
if [ "${PILOT_ADBD_PTY_READY}" = 1 ] &&
   ! ${BB} mount -t devpts devpts /runtime/dev/pts; then
  pilot_failure pty "cannot expose runtime PTYs; continuing without terminal diagnostics"
fi
${BB} mount -o bind / /runtime/nand-source ||
  pilot_fatal "cannot retain source root evidence"
${BB} mount -o remount,bind,ro /runtime/nand-source ||
  pilot_fatal "cannot enforce read-only source view"
for identity in /etc/reinvoke-release /etc/reinvoke; do
  ${BB} mount -o bind "${identity}" "/runtime${identity}" ||
    pilot_fatal "cannot bind immutable identity"
  ${BB} mount -o remount,bind,ro "/runtime${identity}" ||
    pilot_fatal "cannot enforce immutable identity"
done
pilot_phase runtime-handoff
${BB} touch /run/reinvoke/runtime-ready
exec ${BB} chroot /runtime /bin/busybox sh /init
pilot_fatal "chroot exec failed"
