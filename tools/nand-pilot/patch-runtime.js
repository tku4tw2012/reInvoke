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
  replace('export PATH\n', 'export PATH\n. /usr/libexec/nand-pilot/common.sh\n. /usr/libexec/nand-pilot/kernel.sh\n. /usr/libexec/nand-pilot/ssh-start.sh\n. /usr/libexec/nand-pilot/usb-adb-start.sh\n. /usr/libexec/nand-pilot/persistence-start.sh\n');
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
  // The stock loop does an unbounded "wait" on the logger after the service
  // dies. The logger reads a FIFO, and any other writer holding that FIFO open
  // keeps it alive forever, so the supervisor never reaches its restart. This
  // was observed on hardware: bonefish crashed, its supervisor sat in do_wait
  // with two stale loggers alive, and the whole runtime lost its WAMP router
  // until it was relaunched by hand.
  //
  // Removing the pipe first gives the logger EOF; the bounded poll then
  // guarantees the loop continues even if something still holds it open.
  replace(`        wait "\${logger_pid}"
        logger_status="$?"
        if \${BB} test "\${logger_status}" -ne 0; then
          log "\${service_name} logger exited \${logger_status}"
        fi
        \${BB} rm -f "\${service_log_pipe}"`,
    `        \${BB} rm -f "\${service_log_pipe}"
        logger_wait=0
        while \${BB} kill -0 "\${logger_pid}" 2>/dev/null &&
          \${BB} test "\${logger_wait}" -lt 5; do
          \${BB} sleep 1
          logger_wait=$((logger_wait + 1))
        done
        if \${BB} kill -0 "\${logger_pid}" 2>/dev/null; then
          \${BB} kill "\${logger_pid}" 2>/dev/null || true
          log "\${service_name} logger did not exit; supervisor continuing"
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
    '  pilot_usb_adb_up || log "USB ADB unavailable; runtime continuing"\n');
  replace('      supervise provision-windowd \\\n        /usr/sbin/reinvoke-provision-windowd',
    '      supervise provision-windowd pilot_resume_then_exec \\\n        /usr/sbin/reinvoke-provision-windowd');
  replace('      log "provisioning window requires reinvoke.wifi_mode=sta-uap"',
    '      log "provisioning window requires reinvoke.wifi_mode=sta-uap"\n' +
    '      pilot_resume_then_exec /bin/busybox true &\n' +
    '      echo "$!" >/run/reinvoke/wifi-resume.pid');
  // The cue renderer is the donor's aplay, run under the runtime loader with
  // the runtime libraries, the same way every other retained binary here is.
  replace('      --lights-dir "${runtime_root}/share/lights"',
    '      --music-volume-state /run/reinvoke/music-volume \\\n' +
    '      --cue-dir "${runtime_root}/share/cues" \\\n' +
    '      --cue-player "${runtime_bin}/aplay" \\\n' +
    '      --cue-loader "${runtime_lib}/ld-linux-armhf.so.3" \\\n' +
    '      --cue-libpath "${runtime_lib}" \\\n' +
    '      --lights-dir "${runtime_root}/share/lights"');
  // A development build can turn the peer firewall off. A single /32 allowlist
  // locks the operator out of a healthy device whenever their workstation takes
  // a new DHCP lease, and the symptom is indistinguishable from a failed boot:
  // ICMP still answers while every port hangs rather than refusing. Observed on
  // 05.8.9, which ran correctly for twenty-three minutes while appearing dead.
  replace('configure_wamp_firewall() {\n' +
    '  ${BB} mkdir -p /usr/lib/xtables\n',
    'configure_wamp_firewall() {\n' +
    '  if ${BB} test -f /etc/native-admin/firewall-disabled; then\n' +
    '    log "WAMP peer firewall disabled by build configuration"\n' +
    '    return 0\n' +
    '  fi\n' +
    '  ${BB} mkdir -p /usr/lib/xtables\n');

  replace('for service_name in mic-capture provision-windowd dsp-interface \\\n' +
    '      pairing-agent-guard \\\n' +
    '      bluealsa-aplay bluealsa bluetoothd dbus bonefish networkd; do',
    'for service_name in mic-capture provision-windowd wifi-resume dsp-interface \\\n' +
    '      pairing-agent bluedroid libreenv servicemanager source-manager \\\n' +
    '      identifiers propertyd dbus bonefish networkd; do');
  replace('  stop_service syslogd\n',
    '  wait_service_stop bluedroid\n' +
    '  stop_service persistence\n' +
    '  wait_service_stop persistence\n' +
    // USB ADB is torn down before the rest of shutdown. adbd sleeps inside the
    // gadget driver and the driver holds the USB controller; leaving both in
    // place through shutdown left this unit unable to complete a soft reboot
    // and it had to be power cycled. Teardown is best effort: a speaker that
    // cannot stop its debug channel must still be able to reboot.
    '  pilot_usb_adb_down || log "USB ADB teardown reported a problem; continuing"\n' +
    '  stop_service syslogd\n');
  // The runtime traps TERM and INT, runs stop_runtime, and then returns to
  // its sleep loop. Nothing ever asks the kernel to restart, so `reboot` on
  // this unit stopped every service and left the speaker running with no
  // network and no USB: indistinguishable from a hang, and only recoverable
  // by pulling the power. Observed twice before it was traced here.
  replace('trap stop_runtime TERM INT',
    'pilot_stop_and_restart() {\n' +
    '  stop_runtime\n' +
    '  ${BB} sync\n' +
    '  # reboot -f calls reboot(2) directly instead of signalling init, which\n' +
    '  # is this script.\n' +
    '  ${BB} reboot -f\n' +
    '  # If that does not take, force it. An orderly restart walks every\n' +
    '  # driver shutdown handler, and one that blocks there would strand the\n' +
    '  # speaker with no way back except the power lead.\n' +
    '  ${BB} sleep 8\n' +
    '  log "orderly restart did not take; forcing"\n' +
    '  echo b >/proc/sysrq-trigger\n' +
    '}\n' +
    'trap pilot_stop_and_restart TERM INT');
    replace('log "native RAM environment is running"', `pilot_phase runtime-dispatched
log "NAND pilot RC12 runtime dispatched; health and NAND origin require evidence, not this message"`);
  // /dev/log is either the BusyBox syslog socket or the Android logger
  // directory, and this BusyBox hardcodes the socket path. Earlier candidates
  // moved the logger nodes aside so syslogd could own /dev/log. The donor
  // stack is an Android binary whose liblog opens /dev/log/main by absolute
  // path, so that choice discarded every ALOGE it wrote. It hid the donor's
  // own errors through the whole A2DP investigation: the stack reported a
  // connected stream, rendered nothing, and recorded no failure anywhere.
  //
  // The Android logger wins because it cannot be reconfigured: liblog is
  // compiled into binaries this project does not build. Service output is
  // redirected instead, which costs nothing, and the donor logcat carries the
  // Android side into its own bounded file.
  replace([
    '      if ${BB} test -S /dev/log; then',
    '        service_log_pipe="/run/reinvoke/logs/${service_name}.pipe"',
  ].join('\n'), [
    '      if ${BB} test -d /run/reinvoke/logs; then',
    '        service_log_pipe="/run/reinvoke/logs/${service_name}.pipe"',
  ].join('\n'));
  replace([
    '        ${BB} logger -t "reinvoke-${service_name}" \\',
    '          <"${service_log_pipe}" &',
    '        logger_pid="$!"',
    '        ${BB} logger -t "reinvoke-${service_name}" \\',
    '          "start uptime=$(${BB} cut -d\' \' -f1 /proc/uptime)"',
  ].join('\n'), [
    '        (',
    '          while read -r service_log_line; do',
    '            echo "reinvoke-${service_name}: ${service_log_line}"',
    '          done <"${service_log_pipe}" \\',
    '            >>/run/reinvoke/logs/runtime.log',
    '        ) &',
    '        logger_pid="$!"',
    '        echo "reinvoke-${service_name}: start uptime=$(${BB} cut -d\' \' -f1 /proc/uptime)" \\',
    '          >>/run/reinvoke/logs/runtime.log',
  ].join('\n'));
  // Nothing waits on a socket that no longer exists.
  replace([
    '  runtime_logger_attempt=0',
    '  while ! ${BB} test -S /dev/log &&',
    '    ${BB} test "${runtime_logger_attempt}" -lt 5; do',
    '    ${BB} sleep 1',
    '    runtime_logger_attempt=$((runtime_logger_attempt + 1))',
    '  done',
    '  ${BB} test -S /dev/log',
  ].join('\n'), [
    '  runtime_logger_attempt=0',
    '  while ! ${BB} test -f /run/reinvoke/logs/android.log &&',
    '    ${BB} test "${runtime_logger_attempt}" -lt 5; do',
    '    ${BB} sleep 1',
    '    runtime_logger_attempt=$((runtime_logger_attempt + 1))',
    '  done',
    '  ${BB} test -f /run/reinvoke/logs/android.log',
  ].join('\n'));

  // Candidate 4.1. Android ueventd creates /dev/log as a directory of kernel
  // logger nodes, so BusyBox syslogd cannot create its socket at that path.
  // Observed on hardware: syslogd died with "bind: Address already in use",
  // /run/reinvoke/logs stayed empty for every boot of every candidate, and the
  // supervisor reported a failure every five seconds, flooding the kernel log.
  replace([
    'start_runtime_logger() {',
    '  (',
    '    while ! ${BB} test -e /run/reinvoke/shutdown; do',
    '      ${BB} syslogd -n -S \\',
    '        -O /run/reinvoke/logs/runtime.log -s 256 -b 1 &',
    '      runtime_logger_pid="$!"',
    '      echo "${runtime_logger_pid}" >/run/reinvoke/syslogd.pid',
    '',
  ].join('\n'), [
    'start_runtime_logger() {',
    '  (',
    '    runtime_logger_failures=0',
    '    while ! ${BB} test -e /run/reinvoke/shutdown; do',
    '      # logcat runs under the donor loader with a matched library path.',
    '      # The system glibc is older than the donor: logcat needs GLIBC_2.15.',
    '      /opt/bluedroid/lib/ld-linux-armhf.so.3 --library-path \\',
    '        /system/lib:/system/lib/hw:/opt/bluedroid/usr/lib:/opt/bluedroid/lib \\',
    '        /system/bin/logcat -v threadtime \\',
    '        -f /run/reinvoke/logs/android.log -r256 -n 4 &',
    '      runtime_logger_pid="$!"',
    '      echo "${runtime_logger_pid}" >/run/reinvoke/syslogd.pid',
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
    '        --identity-file /opt/bluedroid/etc/identity-hex \\',
    '        --device-name-file /opt/bluedroid/etc/device-name',
    '      identifiers_wait=0',
    '      while ! ${BB} test -f /run/reinvoke/identifiers.pid &&',
    '        ${BB} test "${identifiers_wait}" -lt 10; do',
    '        ${BB} sleep 1',
    '        identifiers_wait=$((identifiers_wait + 1))',
    '      done',
    '      # The donor arbitrated which source owned the speaker in',
    '      # music-source-manager and routed "pause whatever is playing"',
    '      # through audio-ui. This runtime answered three of those procedures',
    '      # from a fixed table whose get-active returned the wrong shape, so a',
    '      # second source could never have been arbitrated. It is seeded with',
    '      # the donor stack, which registers itself as a source at startup.',
    '      supervise source-manager \\',
    '        /usr/bin/reinvoke-source-manager \\',
    '        -router-host 127.0.0.1 -router-port 9999 \\',
    '        -register com.harman.bluetooth',
    '      source_manager_wait=0',
    '      while ! ${BB} test -f /run/reinvoke/source-manager.pid &&',
    '        ${BB} test "${source_manager_wait}" -lt 10; do',
    '        ${BB} sleep 1',
    '        source_manager_wait=$((source_manager_wait + 1))',
    '      done',
    '      # The donor blocks on Android ServiceManager the moment an A2DP',
    '      # stream config arrives, and retries forever if nobody answers.',
    '      # Traced on 05.8.6: the A2DP connection_state_cb and the first',
    '      # "Waiting for initialization" share a millisecond on the BTIF',
    '      # thread, which then spins 1257 times and emits no further btif_av',
    '      # event. The state machine never leaves "opening", so the media task',
    '      # never decodes and all 707 delivered SBC packets are discarded.',
    '      # servicemanager clears that wait, but only once it can reach a',
    '      # property service, so the property area is published first.',
    '      supervise propertyd \\',
    '        /usr/bin/reinvoke-propertyd \\',
    '        -area /dev/__properties__ \\',
    '        -socket /dev/socket/property_service \\',
    '        -ready /run/reinvoke/properties-ready',
    '      propertyd_wait=0',
    '      while ! ${BB} test -f /run/reinvoke/properties-ready &&',
    '        ${BB} test "${propertyd_wait}" -lt 10; do',
    '        ${BB} sleep 1',
    '        propertyd_wait=$((propertyd_wait + 1))',
    '      done',
    '      # Readers never open the area by path. The donor loader reads',
    '      # ANDROID_PROPERTY_WORKSPACE as "<fd>,<size>" and mmaps that',
    '      # descriptor, so the descriptor has to be inherited from here.',
    '      if ${BB} test -f /run/reinvoke/properties-ready; then',
    '        if exec 9</dev/__properties__; then',
    '          ANDROID_PROPERTY_WORKSPACE="9,32768"',
    '          export ANDROID_PROPERTY_WORKSPACE',
    '        else',
    '          echo "propertyd: property area could not be opened" >&2',
    '        fi',
    '      else',
    '        echo "propertyd: property area was not published" >&2',
    '      fi',
    '      # The donor kit ships this binary; it was simply never packaged, and',
    '      # candidate 05.8.7 then started it the wrong way. The LD_LIBRARY_PATH',
    '      # form segfaults immediately: 1380 restarts in one session, never',
    '      # once reaching the point where it publishes its own property. Under',
    '      # the donor loader it starts, becomes the binder context manager and',
    '      # sets service.servicemanager itself, which is what releases the',
    '      # stack\'s defaultServiceManager() wait.',
    '      supervise servicemanager \\',
    '        ${BB} env \\',
    '        ANDROID_PROPERTY_WORKSPACE="${ANDROID_PROPERTY_WORKSPACE}" \\',
    '        /opt/bluedroid/lib/ld-linux-armhf.so.3 \\',
    '        --library-path /system/lib:/system/lib/hw:/opt/bluedroid/usr/lib:/opt/bluedroid/lib \\',
    '        /system/bin/servicemanager',
    '      servicemanager_wait=0',
    '      while ! ${BB} test -f /run/reinvoke/servicemanager.pid &&',
    '        ${BB} test "${servicemanager_wait}" -lt 10; do',
    '        ${BB} sleep 1',
    '        servicemanager_wait=$((servicemanager_wait + 1))',
    '      done',
    '      # The stack blocks inside its A2DP connection callback until the',
    '      # binder service libre.EnvItems answers, because that is where it',
    '      # saves the last connected address. LibreEnv publishes it. Without',
    '      # it the callback never returns, the A2DP state machine never leaves',
    '      # "opening", and every decoded packet is discarded while the',
    '      # amplifier stays muted. The donor starts it right after',
    '      # servicemanager for the same reason.',
    '      supervise libreenv \\',
    '        ${BB} env \\',
    '        ANDROID_PROPERTY_WORKSPACE="${ANDROID_PROPERTY_WORKSPACE}" \\',
    '        /opt/bluedroid/lib/ld-linux-armhf.so.3 \\',
    '        --library-path /system/lib:/system/lib/hw:/opt/bluedroid/usr/lib:/opt/bluedroid/lib \\',
    '        /system/bin/LibreEnv',
    '      libreenv_wait=0',
    '      while ! ${BB} test -f /run/reinvoke/libreenv.pid &&',
    '        ${BB} test "${libreenv_wait}" -lt 10; do',
    '        ${BB} sleep 1',
    '        libreenv_wait=$((libreenv_wait + 1))',
    '      done',
    '      supervise bluedroid \\',
    '        ${BB} sh /opt/bluedroid/start.sh',
    '      # The MCU reports a Bluetooth long press by signalling a pairing agent',
    '      # at a PID file. That contract outlived BlueZ, so this bridge holds it',
    '      # and forwards the press to the donor stack, and creates the state file',
    '      # the MCU reads before it will drive the indicator LED.',
    '      supervise pairing-agent \\',
    '        /opt/reinvoke/bin/reinvoke-pairing-agent',
    '',
  ].join('\n'));

  // Candidate 05.7 removed BlueZ and BlueALSA, but the RC12 init still passed
  // the MCU three flags naming those binaries. Go exits 2 on an unknown flag,
  // so the service crash-looped and the boot sequence never reached the rest
  // of the runtime: seven services became three. Volume is answered from the
  // MCU's own state now, and the donor registers its own media procedures, so
  // these flags have no replacement.
  replace('      --bluealsa-cli "${runtime_bin}/bluealsa-cli" \\\n' +
    '      --bluealsa-peer "${PEER_ADDRESS}" \\\n' +
    '      --media-control "${runtime_bin}/bluez-media-control" \\\n', '');

  // The pairing agent is this project's now, not the donor's BlueZ one.
  replace('      --pairing-agent-executable "${runtime_bin}/bluez-pairing-agent" \\',
    '      --pairing-agent-executable /opt/reinvoke/bin/reinvoke-pairing-agent \\');

  // There is no automatic speaker muting, so neither flag has a reader.
  //
  // The amplifier used to unmute only while the process holding the playback
  // device resolved to one named executable, and re-mute when ALSA stopped.
  // That was this project's invention, not the donor's: the donor's own
  // audio-ui rendered chimes and prompts through its own players and nothing
  // checked who was rendering. Keeping it meant no sound this runtime did not
  // itself play could reach the speaker, which silenced the vendor's startup
  // chime. The amplifier and DAC are now opened when the hardware is
  // initialised and follow only the explicit mute procedures.
  replace('      --playback-lease /run/reinvoke/bluealsa-playback-active \\\n' +
    '      --playback-owner-executable "${runtime_bin}/bluealsa-aplay" \\\n',
    '');
  // One service missing its precondition must not silently cancel every
  // service after it. Candidate 05.8.4 shipped an mcu-interface that refused
  // its own flags, so the microphone state never appeared, this bare return
  // fired, and dbus, identifiers, bluedroid, pairing-agent, dsp-interface and
  // mic-capture were all skipped with nothing in the log to say why. The
  // speaker came up with no Bluetooth and no DSP for one bad flag.
  // The matched text is pinned to the base image, so it keeps that image's
  // wording. Ours below does not.
  replace('      log "microphone privacy state was not initialized"\n' +
    '      return\n',
    '      pilot_failure "service-mcu-interface" \\\n' +
    '        "microphone mute state absent; continuing without it"\n');
  return text;
}
module.exports = { patchRuntime, INIT_SHA256 };
