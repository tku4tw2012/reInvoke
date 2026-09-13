// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
'use strict';
const crypto = require('crypto');

const INIT_SHA256 = '2ac768600ee33b36e58f5c367e5a6b7760934fc422be29176ddb2797f1d71ca8';
function patchRuntime(source) {
  if (crypto.createHash('sha256').update(source).digest('hex') !== INIT_SHA256)
    throw new Error('RC12 /init hash mismatch; review a new context-pinned patch');
  let text = source.toString();
  function replace(old, value) {
    if (text.split(old).length !== 2) throw new Error('ambiguous/missing RC12 patch context');
    text = text.replace(old, value);
  }
  replace('export PATH\n', 'export PATH\n. /usr/libexec/nand-pilot/common.sh\n. /usr/libexec/nand-pilot/kernel.sh\n. /usr/libexec/nand-pilot/ssh-start.sh\n. /usr/libexec/nand-pilot/adb-network-start.sh\n. /usr/libexec/nand-pilot/persistence-start.sh\n');
  replace('  echo "reInvoke: $*" > /dev/kmsg', `  pilot_log "runtime: $*"
  case "$*" in
    *failed*|*incomplete*|*invalid*|*missing*|*unavailable*|*"not initialized"*)
      pilot_failure runtime "$*" ;;
  esac`);
  replace('      log "${service_name} exited ${service_status}"',
    `      log "\${service_name} exited \${service_status}"
      if [ "\${service_status}" -ne 0 ]; then
        pilot_failure "service-\${service_name}" "process exited \${service_status}; supervisor retrying"
      fi`);
  const mounts = text.slice(text.indexOf('${BB} mount -t proc proc /proc\n'),
    text.indexOf('(umask 0; /sbin/ueventd -s)'));
  replace(mounts, `pilot_phase runtime-initializing
\${BB} mkdir -p /dev/pts /tmp /run/reinvoke /data/local/tmp
\${BB} chmod 0700 /run/reinvoke
pilot_check_writable /usr/var/lib/bluetooth /run/reinvoke /data/local/tmp /tmp ||
  pilot_fatal "runtime writable paths failed"

`);
  const hardwareStart = text.indexOf('if ${BB} test -d /sys/class/android_usb/android0; then\n');
  const hardwareEnd = text.indexOf('${BB} mkdir -p /run/reinvoke/logs\n', hardwareStart);
  replace(text.slice(hardwareStart, hardwareEnd), `pilot_load_modules || pilot_fatal "kernel/radio compatibility failed; early ADB remains supervised"

`);
  replace('  . "${runtime_root}/etc/runtime.conf"\n',
    '  . "${runtime_root}/etc/runtime.conf"\n' +
    '  pilot_ssh_start || log "SSH fallback unavailable; runtime continuing"\n' +
    '  pilot_persistence_start || log "Persistent settings unavailable; runtime continuing"\n' +
    '  pilot_adb_network_start || log "Network ADB unavailable; runtime continuing"\n');
  replace('      supervise provision-windowd \\\n        /usr/sbin/reinvoke-provision-windowd',
    '      supervise provision-windowd pilot_resume_then_exec \\\n        /usr/sbin/reinvoke-provision-windowd');
  replace('      log "provisioning window requires reinvoke.wifi_mode=sta-uap"',
    '      log "provisioning window requires reinvoke.wifi_mode=sta-uap"\n' +
    '      pilot_resume_then_exec /bin/busybox true &\n' +
    '      echo "$!" >/run/reinvoke/wifi-resume.pid');
  replace('      --lights-dir "${runtime_root}/share/lights"',
    '      --music-volume-state /run/reinvoke/music-volume \\\n' +
    '      --lights-dir "${runtime_root}/share/lights"');
  replace('for service_name in mic-capture provision-windowd dsp-interface \\\n',
    'for service_name in mic-capture provision-windowd wifi-resume dsp-interface \\\n');
  replace('  stop_service syslogd\n',
    '  wait_service_stop bluetoothd\n' +
    '  stop_service persistence\n' +
    '  wait_service_stop persistence\n' +
    '  stop_service syslogd\n');
  replace('log "native RAM environment is running"', `pilot_phase runtime-dispatched
log "NAND pilot RC12 runtime dispatched; health and NAND origin require evidence, not this message"`);
  // Candidate 4.1. Android ueventd creates /dev/log as a directory of kernel
  // logger nodes, so BusyBox syslogd cannot create its socket at that path.
  // Observed on hardware: syslogd died with "bind: Address already in use",
  // /run/reinvoke/logs stayed empty for every boot of every candidate, and the
  // supervisor reported a failure every five seconds, flooding the kernel log.
  replace([
    'start_runtime_logger() {',
    '  (',
    '    while ! ${BB} test -e /run/reinvoke/shutdown; do',
    '',
  ].join('\n'), [
    'start_runtime_logger() {',
    '  if ${BB} test -d /dev/log && ! ${BB} test -S /dev/log; then',
    '    ${BB} rm -rf /dev/androidlog',
    '    if ${BB} mv /dev/log /dev/androidlog; then',
    '      log "kernel logger nodes moved to /dev/androidlog for the syslog socket"',
    '    else',
    '      log "kernel logger nodes could not be moved; runtime logging unavailable"',
    '    fi',
    '  fi',
    '  (',
    '    runtime_logger_failures=0',
    '    while ! ${BB} test -e /run/reinvoke/shutdown; do',
    '',
  ].join('\n'));
  replace([
    '      log "runtime logger exited ${runtime_logger_status}"',
    '      if ! ${BB} test -e /run/reinvoke/shutdown; then',
    '        ${BB} sleep 5',
    '      fi',
    '',
  ].join('\n'), [
    '      # An orderly shutdown must never be counted as a logger failure.',
    '      if ${BB} test -e /run/reinvoke/shutdown; then',
    '        break',
    '      fi',
    '      log "runtime logger exited ${runtime_logger_status}"',
    '      # Count every unexpected exit, including a zero status: a logger',
    '      # that keeps exiting while the system runs is failing either way.',
    '      runtime_logger_failures=$((runtime_logger_failures + 1))',
    '      if ${BB} test "${runtime_logger_failures}" -ge 5; then',
    '        log "runtime logger failed ${runtime_logger_failures} times; retries stopped"',
    '        break',
    '      fi',
    '      ${BB} sleep 5',
    '',
  ].join('\n'));
  // The HCI helper prints its own diagnostics on every failed attempt, so an
  // unrecoverable controller would rotate the bounded runtime log away.
  // The retry cadence is deliberately unchanged: no evidence identifies
  // hci-init as the cause of the one observed non-recovery.
  replace([
    '  until ${BB} test -e /run/reinvoke/shutdown ||',
    '    "${generation_hci_init}" --reset; do',
    '    log "HCI initialization failed; retrying"',
    '    ${BB} sleep 5',
    '  done',
    '  ${BB} test -e /run/reinvoke/shutdown && return 0',
    '  exec "$@"',
    '',
  ].join('\n'), [
    '  generation_attempts=0',
    '  while ! ${BB} test -e /run/reinvoke/shutdown; do',
    '    generation_attempts=$((generation_attempts + 1))',
    '    if ${BB} test "${generation_attempts}" -eq 1 ||',
    '      ${BB} test $((generation_attempts % 12)) -eq 0; then',
    '      "${generation_hci_init}" --reset && break',
    '      log "HCI initialization failed; retrying (attempt ${generation_attempts})"',
    '    else',
    '      "${generation_hci_init}" --reset >/dev/null 2>&1 && break',
    '    fi',
    '    ${BB} sleep 5',
    '  done',
    '  ${BB} test -e /run/reinvoke/shutdown && return 0',
    '  if ${BB} test "${generation_attempts}" -gt 1; then',
    '    log "HCI initialization recovered after ${generation_attempts} attempts"',
    '  fi',
    '  exec "$@"',
    '',
  ].join('\n'));
  // Candidate 05: the donor Bluedroid userspace replaces the BlueZ stack.
  replace([
    '      supervise bluetoothd \\',
    '        run_bluetoothd_generation "${runtime_bin}/hci-init" \\',
    '        ${BB} env DBUS_SYSTEM_BUS_ADDRESS="${runtime_bus}" \\',
    '        "${runtime_bin}/bluetoothd" -n -p a2dp,avrcp \\',
    '        -f "${runtime_root}/etc/bluez-main.conf"',
    '      supervise bluealsa \\',
    '        ${BB} env DBUS_SYSTEM_BUS_ADDRESS="${runtime_bus}" \\',
    '        "${runtime_bin}/bluealsa" -p a2dp-sink -i hci0 \\',
    '        --initial-volume=0',
    '      until ${BB} test -e /run/reinvoke/shutdown ||',
    '        ${BB} env DBUS_SYSTEM_BUS_ADDRESS="${runtime_bus}" \\',
    '          "${runtime_bin}/bluealsa-cli" list-services 2>/dev/null |',
    '          ${BB} grep -q \'^org\\.bluealsa$\'; do',
    '        ${BB} sleep 1',
    '      done',
    '      ${BB} test -e /run/reinvoke/shutdown && exit 0',
    '      supervise bluealsa-aplay \\',
    '        ${BB} env DBUS_SYSTEM_BUS_ADDRESS="${runtime_bus}" \\',
    '        REINVOKE_PLAYBACK_LEASE=/run/reinvoke/bluealsa-playback-active \\',
    '        "${runtime_bin}/bluealsa-aplay" \\',
    '        --pcm-buffer-time=170000 --pcm-period-time=20000 \\',
    '        -D plughw:1,0 \\',
    '        "${PEER_ADDRESS}"',
    '      supervise pairing-agent-guard \\',
    '        run_with_bluetoothd_generation \\',
    '        /run/reinvoke/pairing-agent.pid \\',
    '        /run/reinvoke/bluetooth-state \\',
    '        ${BB} env DBUS_SYSTEM_BUS_ADDRESS="${runtime_bus}" \\',
    '        "${runtime_bin}/bluez-pairing-agent" \\',
    '        "${PEER_ADDRESS}" "${PAIR_SECONDS}" "${PAIR_SECONDS}" \\',
    '        /run/reinvoke/bluetooth-state',
    '',
  ].join('\n'),
    [
    '      # Candidate 05 replaces BlueZ with the donor Bluedroid userspace. The',
    '      # donor calls com.harman.identifiersGet during initialization and exits',
    '      # if nobody answers, so its provider starts first.',
    '      supervise identifiers \\',
    '        /bin/reinvoke-identifiers \\',
    '        --identity-hex "$(${BB} cat /etc/nand-pilot/bluedroid-identity)" \\',
    '        --device-name "$(${BB} cat /etc/nand-pilot/bluedroid-name)"',
    '      identifiers_wait=0',
    '      while ! ${BB} test -f /run/reinvoke/identifiers.pid &&',
    '        ${BB} test "${identifiers_wait}" -lt 10; do',
    '        ${BB} sleep 1',
    '        identifiers_wait=$((identifiers_wait + 1))',
    '      done',
    '      supervise bluedroid \\',
    '        ${BB} sh /opt/bluedroid/start.sh',
    '',
  ].join('\n'));
  return text;
}
module.exports = { patchRuntime, INIT_SHA256 };
