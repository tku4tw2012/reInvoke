#!/bin/busybox sh
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT
# The target has BusyBox ash, not Bash. All persistent mounts remain read-only.

BB=/bin/busybox
PATH=/sbin:/bin:/usr/sbin:/usr/bin
HOME=/root
export PATH HOME
PILOT_STATE=/run/reinvoke-bsl
. /usr/libexec/nand-pilot/common.sh

stop_boot() {
  echo "reInvoke BSL stopped: $*" >&2
  if test -d /run/reinvoke-bsl; then
    echo "$*" >/run/reinvoke-bsl/failure
    echo failed >/run/reinvoke-bsl/phase
  fi
  while true; do ${BB} sleep 5; done
}

make_block_node() {
  device="$(${BB} cat "/sys/block/mtdblock$1/dev")" || return 1
  case "${device}" in
    31:*) ;;
    *) echo "Unexpected NAND block device: ${device}" >&2; return 1 ;;
  esac
  minor="${device#*:}"
  case "${minor}" in
    ''|*[!0-9]*) return 1 ;;
  esac
  ${BB} mknod -m 0400 "$2" b 31 "${minor}"
}

${BB} mount -t proc proc /proc || stop_boot proc
${BB} mount -t sysfs sysfs /sys || stop_boot sysfs
${BB} mount -t tmpfs devtmpfs /dev || stop_boot dev
${BB} mkdir -p /dev/pts || stop_boot pts-directory
${BB} mount -t tmpfs -o mode=0755,size=16m tmpfs /run || stop_boot run
${BB} mount -t tmpfs -o mode=1777,size=16m tmpfs /tmp || stop_boot tmp
${BB} mkdir -p /run/reinvoke-bsl /run/adb/local/tmp /run/adb/adb ||
  stop_boot diagnostic-directories
PILOT_ADBD_PTY_READY=1
if ! ${BB} mount -t devpts devpts /dev/pts; then
  PILOT_ADBD_PTY_READY=0
  pilot_failure pty "devpts unavailable; continuing without terminal diagnostics"
fi
${BB} test -e /dev/random || ${BB} mknod -m 0444 /dev/random c 1 8
${BB} test -e /dev/urandom || ${BB} mknod -m 0444 /dev/urandom c 1 9
(umask 0; /sbin/ueventd -s) || stop_boot coldplug
${BB} ifconfig lo 127.0.0.1 up || stop_boot loopback
for spec in 'null 1 3' 'console 5 1' 'ptmx 5 2' 'kmsg 1 11'; do
  set -- ${spec}
  test -c "/dev/$1" || ${BB} mknod -m 0600 "/dev/$1" c "$2" "$3" ||
    stop_boot "device $1"
done
${BB} chmod 0666 /dev/null /dev/ptmx || stop_boot device-permissions
for node in /dev/mtd[0-9]* /dev/mtdblock[0-9]* \
  /dev/mtd/mtd[0-9]* /dev/mtd/mtdblock[0-9]*; do
  ${BB} rm -f "${node}"
done
{
  echo "reInvoke BSL forward launcher v3; pid=$$"
  ${BB} cat /proc/version /proc/cmdline /proc/mtd /proc/self/mountinfo
} >/run/reinvoke-bsl/entry.txt

PILOT_ADBD_PRODUCT=reInvoke-NAND-03
PILOT_ADBD_STARTED_PHASE=adb-started
PILOT_ADBD_DEGRADED_PHASE=adb-degraded
PILOT_ADBD_ENABLE_DEV_FILE=/sys/class/misc/android_adb_enable/dev
PILOT_ADBD_ENABLE_NODE=/dev/android_adb_enable
PILOT_ADBD_TTYGS0_DEV_FILE=/sys/class/tty/ttyGS0/dev
PILOT_ADBD_TTYGS0_NODE=/dev/ttyGS0
PILOT_ADBD_RUNTIME_ROOT=""
if ! pilot_usb_adbd_launch; then
  pilot_log "early USB diagnostics unavailable; continuing to NAND handoff"
fi

test "$$" -eq 1 ||
  stop_boot "entry is not PID 1; retaining ADB instead of claiming a handoff"

rootfs_index=""
master_index=""
for mtd in /sys/class/mtd/mtd[0-9]*; do
  test -f "${mtd}/name" || continue
  test "$(${BB} cat "${mtd}/type")" = nand || continue
  test "$(${BB} cat "${mtd}/erasesize")" = 131072 || continue
  name="$(${BB} cat "${mtd}/name")"
  size="$(${BB} cat "${mtd}/size")"
  index="${mtd##*/mtd}"
  case "${name}:${size}" in
    rootfs:94371840)
      test -z "${rootfs_index}" || stop_boot duplicate-rootfs
      rootfs_index="${index}"
      ;;
    mv_nand:268435456)
      test -z "${master_index}" || stop_boot duplicate-master
      master_index="${index}"
      ;;
  esac
done

if test -n "${rootfs_index}"; then
  source=/run/reinvoke-bsl/rootfs-ro
  make_block_node "${rootfs_index}" "${source}" || stop_boot rootfs-node
elif test -n "${master_index}"; then
  test ! -e /sys/block/loop0/loop/backing_file || stop_boot loop-in-use
  ${BB} mkdir -m 0700 /run/reinvoke-bsl/loop-view || stop_boot loop-directory
  make_block_node "${master_index}" /run/reinvoke-bsl/loop-view/mtd ||
    stop_boot master-node
  source=/run/reinvoke-bsl/loop-view/loop
  ${BB} mknod -m 0400 "${source}" b 7 0 || stop_boot loop-node
  ${BB} losetup -r "${source}" /run/reinvoke-bsl/loop-view/mtd ||
    stop_boot loop-associate
  /sbin/set-private-loop-offset || stop_boot loop-bounds
else
  stop_boot "no matching rootfs partition or whole NAND master"
fi

${BB} mount -t squashfs -o ro "${source}" /nand-root || stop_boot rootfs-mount
${BB} sha256sum -c /etc/reinvoke-bsl-target.sha256 \
  >/run/reinvoke-bsl/source-hashes.txt ||
  stop_boot rootfs-hash
${BB} cat /proc/self/mountinfo >/run/reinvoke-bsl/source-mountinfo
echo rootfs-verified >/run/reinvoke-bsl/phase

# Never resurrect a second USB opener after handing ownership to main.
${BB} touch /run/reinvoke-bsl/stop-adb || stop_boot stop-adb
bsl_stop_wait=0
while test -f /run/reinvoke-bsl/adbd-supervisor.pid; do
  bsl_stop_wait=$((bsl_stop_wait + 1))
  test "${bsl_stop_wait}" -le 5 || stop_boot usb-owner-not-stopped
  ${BB} sleep 1
done
echo handoff >/run/reinvoke-bsl/phase
exec ${BB} chroot /nand-root /bin/busybox sh /init
stop_boot runtime-exec
