// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
'use strict';
const assert = require('assert');
const fs = require('fs');
const path = require('path');
const cp = require('child_process');
const zlib = require('zlib');
const lib = require('./build-lib');
const { patchRuntime } = require('./patch-runtime');
const { verifyLoadEquivalent } = require('./elf-load-check');
const archive = path.resolve(__dirname, '../../../reinvoke-archive');
const qemu = path.join(archive, 'emulation/qemu-arm-static');
const fixture = fs.mkdtempSync(path.join(__dirname, '.shell-test-'));
try {
  const source = path.join(fixture, 'source');
  fs.mkdirSync(source);
  const rc12 = path.join(archive, lib.pins.rc12.path);
  lib.verify(rc12, lib.pins.rc12);
  lib.run('bash', ['-o', 'pipefail', '-c',
    'gzip -dc "$1" | (cd "$2" && cpio -id --no-preserve-owner --quiet bin/busybox init)',
    'test-extract', rc12, source]);
  const bb = path.join(source, 'bin/busybox');
  assert.equal(lib.hashFile(bb), lib.BB_SHA256);
  const invoke = (script, args = []) => cp.spawnSync(qemu, [bb, 'sh', '-c', script, 'test', ...args],
    { encoding: 'utf8', timeout: 30000, env: { ...process.env, BB: `${qemu} ${bb}` } });
  const check = result => assert.equal(result.status, 0, result.stderr || result.stdout);
  const common = path.join(__dirname, 'common.sh');
  const kernel = path.join(__dirname, 'kernel.sh');
  const init = fs.readFileSync(path.join(source, 'init'));
  const patched = patchRuntime(init);
  assert.throws(() => patchRuntime(Buffer.concat([init, Buffer.from('\n')])), /hash mismatch/);
  const patchedFile = path.join(fixture, 'init');
  fs.writeFileSync(patchedFile, patched);
  for (const file of ['bootstrap.sh', 'bsl-init.sh', 'common.sh', 'kernel.sh', 'ssh-start.sh'])
    check(cp.spawnSync(qemu, [bb, 'sh', '-n', path.join(__dirname, file)], { encoding: 'utf8' }));
  check(cp.spawnSync(qemu, [bb, 'sh', '-n', patchedFile], { encoding: 'utf8' }));
  for (const release of ['3.8.13-yocto-standard', '3.8.13-reinvoke-audio-sd8887'])
    check(invoke('. "$1"; pilot_select_kernel "$2"', [kernel, release]));
  assert.equal(invoke('. "$1"; pilot_select_kernel 3.8.13-unreviewed', [kernel]).status, 1);
  const bootstrap = fs.readFileSync(path.join(__dirname, 'bootstrap.sh'), 'utf8');
  const bsl = fs.readFileSync(path.join(__dirname, 'bsl-init.sh'), 'utf8');
  assert(bootstrap.includes('PILOT_ADBD_PRODUCT=reInvoke-NAND-03'));
  assert(bsl.includes('PILOT_ADBD_PRODUCT=reInvoke-NAND-03'));
  assert(!bsl.includes('fallback.pid'), 'no competing BSL transport resurrection');
  assert(bsl.indexOf('stop_boot rootfs-hash') < bsl.indexOf('exec ${BB} chroot /nand-root'));
  assert(bootstrap.includes('exec ${BB} chroot /runtime /bin/busybox sh /init'));
  assert(!/(?:\$\{BB\}\s+|\/(?:bin|sbin)\/)(?:nandwrite|ubiupdatevol|flash_custk|flash_erase(?:all)?)\b/
    .test(bootstrap + bsl + patched + fs.readFileSync(common)));
  assert(!fs.readFileSync(kernel, 'utf8').includes('mac_addr=02:'), 'no hardcoded fleet MAC');
  assert(patched.includes('pilot_ssh_start'));
  const data = path.join(fixture, 'bytes');
  fs.writeFileSync(data, 'payload');
  const pin = { bytes: 7, sha256: lib.hashFile(data) };
  lib.verify(data, pin);
  assert.throws(() => lib.verify(data, { ...pin, bytes: 8 }), /mismatch/);
  assert.throws(() => lib.verify(data, { ...pin, sha256: '0'.repeat(64) }), /mismatch/);
  check(invoke('. "$1"; pilot_verify_payload "$2" "$3" "$4"', [common, data, pin.sha256, '7']));
  assert.equal(invoke('. "$1"; pilot_verify_payload "$2" "$3" 7', [common, data, '0'.repeat(64)]).status, 1);
  const packed = path.join(fixture, 'packed'), extracted = path.join(fixture, 'extracted');
  fs.mkdirSync(packed); fs.mkdirSync(extracted);
  fs.writeFileSync(path.join(packed, 'service'), 'retained');
  fs.symlinkSync('service', path.join(packed, 'link'));
  const payload = path.join(fixture, 'payload.gz');
  lib.run('bash', ['-o', 'pipefail', '-c',
    'cd "$1"; find . -print | cpio -o -H newc --quiet | gzip -n >"$2"', 'pack', packed, payload]);
  check(invoke('. "$1"; pilot_extract_payload "$2" "$3"', [common, payload, extracted]));
  assert.equal(fs.readlinkSync(path.join(extracted, 'link')), 'service');
  fs.writeFileSync(payload, fs.readFileSync(payload).subarray(0, 20));
  assert.notEqual(invoke('. "$1"; pilot_extract_payload "$2" "$3"', [common, payload, extracted]).status, 0);
  fs.writeFileSync(payload, zlib.gzipSync(Buffer.from('not cpio')));
  assert.notEqual(invoke('. "$1"; pilot_extract_payload "$2" "$3"', [common, payload, extracted]).status, 0);

  const elf = Buffer.alloc(100);
  Buffer.from('7f454c46010101', 'hex').copy(elf);
  for (const [offset, value] of [[16, 3], [18, 40], [40, 52], [42, 32], [44, 1]]) elf.writeUInt16LE(value, offset);
  for (const [offset, value] of [[20, 1], [24, 0x8000], [28, 52], [52, 1], [56, 84],
    [60, 0x8000], [68, 16], [72, 16], [76, 5], [80, 4]]) elf.writeUInt32LE(value, offset);
  assert.equal(verifyLoadEquivalent(elf, elf).loads.length, 1);
  const changed = Buffer.from(elf); changed[84] ^= 1;
  assert.throws(() => verifyLoadEquivalent(elf, changed), /changed/);

  const gadget = path.join(fixture, 'gadget');
  fs.mkdirSync(gadget);
  const configure = () => invoke('. "$1"; PILOT_ADBD_GADGET="$2"; configure_adb fixture', [common, gadget]);
  check(configure());
  assert.equal(fs.readFileSync(path.join(gadget, 'functions'), 'utf8').trim(), 'adb');
  fs.chmodSync(path.join(gadget, 'enable'), 0o444);
  check(configure()); // Matching healthy gadget: no disable/re-enable write.
  fs.chmodSync(path.join(gadget, 'enable'), 0o644);
  fs.mkdirSync(path.join(gadget, 'f_acm'));
  check(configure());
  assert.equal(fs.readFileSync(path.join(gadget, 'functions'), 'utf8').trim(), 'acm,adb');
  const proc = path.join(fixture, 'proc'), fd = path.join(proc, '42/fd');
  fs.mkdirSync(fd, { recursive: true });
  fs.symlinkSync('/dev/null', path.join(fd, '0'));
  const fdCheck = () => invoke('. "$1"; PILOT_ADBD_PROC="$2"; PILOT_ADBD_NODE=/dev/zero; pilot_adbd_usb_open 42', [common, proc]);
  assert.equal(fdCheck().status, 1, 'alive PID with wrong FD is not transport readiness');
  const alias = path.join(fixture, 'runtime-device-alias');
  fs.symlinkSync('/dev/zero', alias);
  fs.symlinkSync(alias, path.join(fd, '3'));
  check(fdCheck());
  fs.rmSync(fd, { recursive: true });
  assert.equal(fdCheck().status, 2, 'unreadable FD evidence remains unknown');
  const devFile = path.join(fixture, 'misc-dev'), node = path.join(fixture, 'adb-node');
  for (const value of ['bad', '0:1', '10:', '10:9:2', '10:20']) {
    fs.writeFileSync(devFile, value);
    fs.writeFileSync(node, 'not a character device');
    assert.notEqual(invoke('. "$1"; pilot_node_from_sysfs "$2" "$3"', [common, devFile, node]).status, 0);
  }
  assert.equal(invoke('. "$1"; pilot_check_pty_node "$2"', [common, node]).status, 1);
  check(invoke('. "$1"; pilot_check_pty_node /dev/ptmx', [common]));
  fs.writeFileSync(devFile, '10:71');
  check(invoke(`
. "$1"
originalBB="$BB"
mockbb() {
  case "$1" in
    test) [ "$2" = -c ] && return 0 ;;
    stat)
      case "$3" in
        '%t:%T') echo a:47 ;;
        '%t:%T:%a:%u:%g') echo "a:47:\${fixture_mode}:0:0" ;;
      esac
      return 0 ;;
    chown|chmod) return 0 ;;
  esac
  \${originalBB} "$@"
}
BB=mockbb
fixture_mode=600
pilot_node_from_sysfs "$2" "$3" || exit 1
fixture_mode=666
pilot_node_from_sysfs "$2" "$3" && exit 2
exit 0
`, [common, devFile, node]));

  const exhausted = path.join(fixture, 'exhausted'); fs.mkdirSync(exhausted);
  check(invoke(`
. "$1"
PILOT_STATE="$2"
originalBB="$BB"
fastbb() { [ "$1" = sleep ] && return 0; \${originalBB} "$@"; }
BB=fastbb
pilot_log() { :; }
pilot_failure() { :; }
pilot_usb_prerequisites() { echo retry >>"$PILOT_STATE/attempts"; return 1; }
adb_loop
`, [common, exhausted]));
  assert.equal(fs.readFileSync(path.join(exhausted, 'attempts'), 'utf8').trim().split('\n').length, 30);
  assert.equal(fs.readFileSync(path.join(exhausted, 'usb-last-failure'), 'utf8').trim(), 'retry-budget-exhausted');

  const state = path.join(fixture, 'state'); fs.mkdirSync(state);
  const supervised = invoke(`
. "$1"
PILOT_STATE="$2"
PILOT_ADBD_RUNTIME_ROOT=/runtime
PILOT_ADBD_LOG="$2/daemon.log"
PILOT_ADBD_PTY_READY=0
PILOT_ADBD_PRODUCT=fixture
pilot_log() { :; }
pilot_failure() { echo "$1" >>"$PILOT_STATE/failures"; }
pilot_usb_prerequisites() {
  if ! \${BB} test -f "$PILOT_STATE/late-node"; then
    echo waiting >"$PILOT_STATE/waiting"
    return 1
  fi
  return 0
}
pilot_adbd_run() { echo "$1" >>"$PILOT_STATE/launches"; exec \${BB} sleep 30; }
pilot_adbd_usb_open() {
  \${BB} test -e "$PILOT_STATE/fd-unreadable" && return 2
  return 0
}
pilot_usb_adbd_launch || exit 1
supervisor=$!
cleanup() {
  \${BB} touch "$PILOT_STATE/stop-adb"
  wait "$supervisor"
}
trap cleanup EXIT
pilot_usb_adbd_launch && exit 2
# Missing PTY and late device do not block the foreground runtime.
\${BB} test -f "$PILOT_STATE/adbd-supervisor.pid" || exit 3
\${BB} touch "$PILOT_STATE/late-node"
for expected in early runtime; do
  count=0
  while [ "$(\${BB} cat "$PILOT_STATE/adbd-root" 2>/dev/null)" != "$expected" ]; do
    count=$((count + 1)); [ "$count" -le 5 ] || exit 4
    \${BB} sleep 1
  done
  \${BB} touch "$PILOT_STATE/runtime-ready"
done
\${BB} touch "$PILOT_STATE/fd-unreadable"
\${BB} sleep 4
\${BB} test "$(\${BB} cat "$PILOT_STATE/adb-transport")" = fd-unreadable || exit 5
\${BB} rm "$PILOT_STATE/fd-unreadable"
\${BB} sleep 2
\${BB} test "$(\${BB} cat "$PILOT_STATE/adb-transport")" = open
`, [common, state]);
  check(supervised);
  assert.deepEqual(fs.readFileSync(path.join(state, 'launches'), 'utf8').trim().split('\n'), ['early', 'runtime']);
  assert(fs.readFileSync(path.join(state, 'failures'), 'utf8').includes('pty'));
  assert(!fs.existsSync(path.join(state, 'adb-owner')), 'owner released at clean handoff shutdown');
  console.log('PASS: retained ARM BusyBox syntax/extraction, payload/hash/ELF negatives, kernel dispatch, dynamic-node/FD failures, PTY-independent late-device retry, healthy gadget no-reset and single-owner runtime handoff.');
} finally {
  fs.rmSync(fixture, { recursive: true, force: true });
}
