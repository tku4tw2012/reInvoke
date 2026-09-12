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
  replace('export PATH\n', 'export PATH\n. /usr/libexec/nand-pilot/common.sh\n. /usr/libexec/nand-pilot/kernel.sh\n');
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
  replace('log "native RAM environment is running"', `pilot_phase runtime-dispatched
log "NAND pilot RC12 runtime dispatched; health and NAND origin require evidence, not this message"`);
  return text;
}
module.exports = { patchRuntime, INIT_SHA256 };
