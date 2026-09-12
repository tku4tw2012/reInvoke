#!/bin/busybox sh
# Copyright (c) Microsoft Corporation.
# SPDX-License-Identifier: MIT

pilot_ssh_start() {
  ssh_config=/etc/native-admin
  if ! ${BB} test -s "${ssh_config}/host-key" ||
     ! ${BB} test -s /root/.ssh/authorized_keys ||
     ! ${BB} test -s "${ssh_config}/allow-cidrs"; then
    pilot_failure ssh "private-key-configuration-missing"
    return 1
  fi
  # Fail closed independently of the WAMP policy: install a complete chain
  # before its first INPUT jump, and launch no listener if any rule fails.
  ssh_iptables() {
    ${BB} env XTABLES_LIBDIR=/opt/reinvoke/lib/xtables \
      /opt/reinvoke/lib/ld-linux-armhf.so.3 \
      --library-path /opt/reinvoke/lib /opt/reinvoke/bin/iptables "$@" \
      >/dev/null 2>&1
  }
  ssh_iptables -N NATIVE_SSH || {
    pilot_failure ssh "firewall-chain-unavailable"
    return 1
  }
  while IFS= read -r ssh_cidr; do
    [ -n "${ssh_cidr}" ] || continue
    ssh_iptables -A NATIVE_SSH -s "${ssh_cidr}" -j ACCEPT || {
      pilot_failure ssh "firewall-peer-rule-failed"
      return 1
    }
  done <"${ssh_config}/allow-cidrs"
  ssh_iptables -A NATIVE_SSH -j DROP &&
    ssh_iptables -I INPUT 1 -p tcp --dport 22 -j NATIVE_SSH || {
      pilot_failure ssh "firewall-install-failed"
      return 1
    }
  echo installed >/run/nand-pilot/ssh-firewall
  # Password auth and forwarding are compiled out, as well as disabled here.
  # No -R: a missing host key must never generate a different identity at boot.
  supervise sshd /usr/sbin/dropbear -F -E -j -k \
    -p 0.0.0.0:22 -r "${ssh_config}/host-key" \
    -P /run/nand-pilot/sshd-native.pid -I 900 -K 30
}
