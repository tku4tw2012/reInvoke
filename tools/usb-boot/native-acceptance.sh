#!/bin/busybox sh
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT

BB=/bin/busybox
failures=0

pass() {
  echo "PASS $1"
}

fail() {
  echo "FAIL $1"
  failures=$((failures + 1))
}

check_pid_file() {
  name="$1"
  pid_file="/run/reinvoke/${name}.pid"
  if ${BB} test -f "${pid_file}" &&
     ${BB} kill -0 "$(${BB} cat "${pid_file}")" 2>/dev/null; then
    pass "service.${name}"
  else
    fail "service.${name}"
  fi
}

if ${BB} test -f /etc/reinvoke-release; then
  pass release.manifest
else
  fail release.manifest
fi

if ${BB} test -f /opt/reinvoke/SHA256SUMS &&
   (cd /opt/reinvoke && ${BB} sha256sum -c SHA256SUMS >/dev/null 2>&1); then
  pass runtime.hashes
else
  fail runtime.hashes
fi

if ${BB} grep -Eq '(/dev/mtd|mtdblock|ubi)' /proc/mounts; then
  fail storage.no_nand_mount
else
  pass storage.no_nand_mount
fi

writable_mtd=0
for node in /dev/mtd[0-9]* /dev/mtdblock[0-9]* \
  /dev/mtd/mtd[0-9]* /dev/mtd/mtdblock[0-9]*; do
  if ${BB} test -e "${node}"; then
    writable_mtd=1
  fi
done
if [ "${writable_mtd}" -eq 0 ]; then
  pass storage.no_raw_mtd_nodes
else
  fail storage.no_raw_mtd_nodes
fi

if ${BB} test -c /dev/reinvoke-nand-ro &&
   [ "$(${BB} stat -c '%a' /dev/reinvoke-nand-ro)" = "400" ]; then
  pass storage.read_only_node
else
  fail storage.read_only_node
fi

for interface in mlan0; do
  if ${BB} test -d "/sys/class/net/${interface}"; then
    pass "network.${interface}"
  else
    fail "network.${interface}"
  fi
done
if ${BB} test -L /sys/class/bluetooth/hci0; then
  pass bluetooth.hci0
else
  fail bluetooth.hci0
fi
if ${BB} test -S /run/reinvoke/dbus/system_bus_socket; then
  pass dbus.socket
else
  fail dbus.socket
fi
if ${BB} grep -q marvell-wm8904 /proc/asound/cards; then
  pass audio.card1
else
  fail audio.card1
fi
if ${BB} test -f /run/reinvoke/dsp-booted; then
  pass dsp.boot_event
else
  fail dsp.boot_event
fi

for service in syslogd bonefish mcu-interface dsp-interface dbus bluetoothd \
  bluealsa bluealsa-aplay pairing-agent; do
  check_pid_file "${service}"
done

zombies=0
for status in /proc/[0-9]*/status; do
  if ${BB} grep -q '^State:.*Z' "${status}" 2>/dev/null; then
    zombies=$((zombies + 1))
  fi
done
if [ "${zombies}" -eq 0 ]; then
  pass process.no_zombies
else
  echo "INFO process.zombies=${zombies}"
  fail process.no_zombies
fi

if ${BB} dmesg |
  ${BB} grep -Eq 'Kernel panic|Oops:|BUG:|Unable to handle kernel'; then
  fail kernel.no_fatal_errors
else
  pass kernel.no_fatal_errors
fi

echo "SUMMARY failures=${failures}"
exit "${failures}"
