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
  const elf = Buffer.alloc(116);
  Buffer.from('7f454c46010101', 'hex').copy(elf);
  elf.writeUInt16LE(3, 16);
  elf.writeUInt16LE(40, 18);
  elf.writeUInt32LE(1, 20);
  elf.writeUInt32LE(0x8000, 24);
  elf.writeUInt32LE(52, 28);
  elf.writeUInt16LE(52, 40);
  elf.writeUInt16LE(32, 42);
  elf.writeUInt16LE(1, 44);
  elf.writeUInt32LE(1, 52);
  elf.writeUInt32LE(84, 56);
  elf.writeUInt32LE(0x8000, 60);
  elf.writeUInt32LE(16, 68);
  elf.writeUInt32LE(16, 72);
  elf.writeUInt32LE(5, 76);
  elf.writeUInt32LE(4, 80);
  const stripped = Buffer.from(elf.subarray(0, 100));
  stripped.writeUInt32LE(100, 32);
  assert.equal(verifyLoadEquivalent(elf, stripped).loads.length, 1);
  for (const offset of [24, 56, 84]) {
    const bad = Buffer.from(stripped);
    bad[offset] ^= 1;
    assert.throws(() => verifyLoadEquivalent(elf, bad),
      /changed|truncated/, 'entry, segment layout or loaded bytes must not change');
  }
  assert.throws(() => verifyLoadEquivalent(elf, stripped.subarray(0, 99)), /truncated/);
  const source = path.join(fixture, 'source-rc12');
  fs.mkdirSync(source);
  const rc12 = path.join(archive, lib.pins.rc12.path);
  lib.verify(rc12, lib.pins.rc12);
  lib.run('bash', ['-o', 'pipefail', '-c',
    'gzip -dc "$1" | (cd "$2" && cpio -id --no-preserve-owner --quiet bin/busybox init)',
    'test-extract', rc12, source]);
  const originalBB = fs.readFileSync(path.join(source, 'bin/busybox'));
  const names = lib.run(qemu, [path.join(source, 'bin/busybox'), '--list']).trim().split('\n');
  assert.strictEqual(lib.sha(originalBB), lib.BB_SHA256);
  assert(names.includes('nandwrite') && names.includes('ubiupdatevol'), 'baseline applets must remain unchanged');
  const bb = path.join(fixture, 'busybox');
  fs.writeFileSync(bb, originalBB, { mode: 0o755 });
  const invoke = (script, args = []) => cp.spawnSync(qemu, [bb, 'sh', '-c', script, 'test', ...args],
    { encoding: 'utf8', env: { ...process.env, BB: `${qemu} ${bb}` } });
  const common = path.join(__dirname, 'common.sh');
  const kernel = path.join(__dirname, 'kernel.sh');
  const freshDev = path.join(fixture, 'fresh-dev');
  fs.mkdirSync(freshDev);
  const ptmx = path.join(freshDev, 'ptmx');
  const ptyPreflight = () => invoke('. "$1"; pilot_check_pty_node "$2"', [common, ptmx]);
  let result = ptyPreflight();
  assert.strictEqual(result.status, 1, 'fresh device tree without ptmx must fail');
  fs.writeFileSync(ptmx, '', { mode: 0o666 });
  result = ptyPreflight();
  assert.strictEqual(result.status, 1, 'a regular file named ptmx must fail');
  fs.unlinkSync(ptmx);
  fs.symlinkSync('/dev/null', ptmx);
  result = ptyPreflight();
  assert.strictEqual(result.status, 1, 'wrong character-device number must fail');
  fs.unlinkSync(ptmx);
  // Metadata only: this never opens a PTY or proves grantpt/unlockpt behavior.
  fs.symlinkSync('/dev/ptmx', ptmx);
  result = ptyPreflight();
  assert.strictEqual(result.status, 0, result.stderr);
  result = invoke('. "$1"; pilot_select_kernel 3.8.13-yocto-standard', [kernel]);
  assert.strictEqual(result.status, 0, result.stderr);
  result = invoke('. "$1"; pilot_select_kernel 3.8.13-reinvoke-audio-sd8887', [kernel]);
  assert.strictEqual(result.status, 0, result.stderr);
  for (const release of ['3.8.13-mrvl', '3.8.13-reinvoke-anything', '6.8-host']) {
    result = invoke('. "$1"; pilot_select_kernel "$2"', [kernel, release]);
    assert.strictEqual(result.status, 1);
    assert.match(result.stderr, /Unsupported kernel/);
  }
  const readonly = path.join(fixture, 'readonly');
  fs.mkdirSync(readonly, { mode: 0o555 });
  result = invoke('. "$1"; pilot_check_writable "$2"', [common, readonly]);
  assert.strictEqual(result.status, 1, 'read-only runtime paths must fail');
  result = invoke('. "$1"; pilot_check_writable "$2"', [common, path.join(fixture, 'writable')]);
  assert.strictEqual(result.status, 0, result.stderr);
  const data = path.join(fixture, 'bytes'); fs.writeFileSync(data, 'known runtime payload');
  const pin = { bytes: fs.statSync(data).size, sha256: lib.hashFile(data) };
  lib.verify(data, pin);
  assert.throws(() => lib.verify(data, { ...pin, sha256: '0'.repeat(64) }), /mismatch/);
  assert.throws(() => lib.verify(data, { ...pin, bytes: pin.bytes + 1 }), /mismatch/);
  result = invoke('. "$1"; pilot_verify_payload "$2" "$3" "$4"', [common, data, pin.sha256, String(pin.bytes)]);
  assert.strictEqual(result.status, 0, result.stderr);
  result = invoke('. "$1"; pilot_verify_payload "$2" "$3" "$4"', [common, data, '0'.repeat(64), String(pin.bytes)]);
  assert.strictEqual(result.status, 1);
  const pack = path.join(fixture, 'pack'), extracted = path.join(fixture, 'extracted');
  fs.mkdirSync(pack); fs.mkdirSync(extracted);
  fs.writeFileSync(path.join(pack, 'owned-service'), 'unchanged RC12 service');
  fs.symlinkSync('owned-service', path.join(pack, 'service-link'));
  const gzip = path.join(fixture, 'runtime.gz');
  lib.run('bash', ['-o', 'pipefail', '-c',
    'cd "$1"; find . -print | cpio -o -H newc --quiet | gzip -n >"$2"', 'pack', pack, gzip]);
  result = invoke('. "$1"; pilot_extract_payload "$2" "$3"', [common, gzip, extracted]);
  assert.strictEqual(result.status, 0, result.stderr);
  assert.strictEqual(fs.readFileSync(path.join(extracted, 'owned-service'), 'utf8'), 'unchanged RC12 service');
  assert.strictEqual(fs.readlinkSync(path.join(extracted, 'service-link')), 'owned-service');
  fs.writeFileSync(gzip, fs.readFileSync(gzip).subarray(0, 20));
  result = invoke('. "$1"; pilot_extract_payload "$2" "$3"', [common, gzip, extracted]);
  assert.notStrictEqual(result.status, 0, 'truncated gzip must not be accepted');
  fs.writeFileSync(gzip, zlib.gzipSync(Buffer.from('not a cpio archive')));
  result = invoke('. "$1"; pilot_extract_payload "$2" "$3"', [common, gzip, extracted]);
  assert.notStrictEqual(result.status, 0, 'valid gzip containing invalid cpio must fail');
  assert.strictEqual(cp.spawnSync(qemu, [bb, 'true']).status, 0);
  assert.strictEqual(cp.spawnSync(qemu, [bb, 'false']).status, 1);
  const init = fs.readFileSync(path.join(source, 'init'));
  const patchedInit = patchRuntime(init);
  assert.throws(() => patchRuntime(Buffer.concat([init, Buffer.from('\n')])), /hash mismatch/);
  assert(!patchedInit.includes('3.8.13-reinvoke*'), 'wildcard module dispatch survived');
  assert(!patchedInit.includes('switch_root'), 'cannot switch_root real SquashFS');
  assert(patchedInit.includes('pilot_check_writable /usr/var/lib/bluetooth'));
  assert.strictEqual(patchedInit.match(/start_autonomous_runtime\(\)/g).length, 1);
  const bootstrap = fs.readFileSync(path.join(__dirname, 'bootstrap.sh'), 'utf8');
  const bsl = fs.readFileSync(path.join(__dirname, 'bsl-init.sh'), 'utf8');
  const startup = [bootstrap, bsl, patchedInit, fs.readFileSync(common, 'utf8'), fs.readFileSync(kernel, 'utf8')].join('\n');
  assert(!/(?:\$\{BB\}\s+|\/(?:bin|sbin)\/)(?:nandwrite|ubiupdatevol|flash_custk|flash_erase(?:all)?)\b/.test(startup),
    'startup must not invoke storage writers');
  assert(bootstrap.includes('pilot_usb_adbd_launch'));
  assert(bootstrap.includes('PILOT_ADBD_PRODUCT=reInvoke-NAND-pilot-02'));
  assert(bootstrap.includes('PILOT_ADBD_STARTED_PHASE=early-adb-started'));
  assert(!bootstrap.includes('pilot_check_pty_node /dev/ptmx ||'),
    'bootstrap must degrade PTY diagnostics instead of hard-failing');
  assert(bootstrap.includes('exec ${BB} chroot /runtime /bin/busybox sh /init'));
  assert(bootstrap.includes('mount -o remount,bind,ro'));
  result = cp.spawnSync(qemu, [bb, 'sh', '-n', path.join(__dirname, 'bootstrap.sh')], { encoding: 'utf8' });
  assert.strictEqual(result.status, 0, result.stderr);
  result = cp.spawnSync(qemu, [bb, 'sh', '-n', path.join(__dirname, 'bsl-init.sh')], { encoding: 'utf8' });
  assert.strictEqual(result.status, 0, result.stderr);
  assert(bsl.includes('. /usr/libexec/nand-pilot/common.sh'));
  assert(bsl.includes('pilot_usb_adbd_launch'));
  assert(bsl.includes('PILOT_ADBD_PRODUCT=reInvoke-BSL-v3'));
  assert(bsl.includes('sha256sum -c /etc/reinvoke-bsl-target.sha256'));
  assert(!bsl.includes('ffec136437e2a0529'), 'BSL must bind the selected rootfs, not stale pilot-01 hashes');
  assert(bsl.indexOf('pilot_usb_adbd_launch') < bsl.indexOf('rootfs_index=""'),
    'BSL diagnostics must precede NAND source selection');
  assert(bsl.includes('rootfs:94371840') && bsl.includes('mv_nand:268435456'));
  assert(bsl.includes('mount -t squashfs -o ro "${source}" /nand-root'));
  assert(bsl.indexOf('stop_boot rootfs-hash') < bsl.indexOf('exec ${BB} chroot /nand-root'),
    'BSL must verify the source before handoff');
  assert(!bsl.includes('/lsync/rbua') && !bsl.includes('setbootflags'),
    'BSL variation must not launch the updater or restore old status');
  const patchedFile = path.join(fixture, 'init'); fs.writeFileSync(patchedFile, patchedInit);
  result = cp.spawnSync(qemu, [bb, 'sh', '-n', patchedFile], { encoding: 'utf8' });
  assert.strictEqual(result.status, 0, result.stderr);
  const launchCallsite = /if ! pilot_usb_adbd_launch; then\n[\s\S]*?\nfi/.exec(bootstrap);
  assert(launchCallsite, 'bootstrap must explicitly handle unavailable diagnostics');
  const launchHelper = `
. "$1"
pilot_log() {
  printf 'LOG:%s\\n' "$*" >>"$PILOT_STATE/events"
}
pilot_failure() {
  printf 'FAIL:%s:%s\\n' "$1" "$2" >>"$PILOT_STATE/events"
}
pilot_phase() {
  printf 'PHASE:%s\\n' "$1" >>"$PILOT_STATE/events"
  printf '%s\\n' "$1" >"$PILOT_STATE/phase"
}
configure_adb() {
  printf 'CONF:%s\\n' "$1" >>"$PILOT_STATE/events"
  return "\${CONFIG_RC}"
}
pilot_node_from_sysfs() {
  printf 'NODE:%s->%s\\n' "$1" "$2" >>"$PILOT_STATE/events"
  return "\${NODE_RC}"
}
adb_loop() {
  printf 'LOOP:%s\\n' "$$" >>"$PILOT_STATE/events"
  case "$ADB_MODE" in
    dead)
      \${BB} sleep 0 >/dev/null 2>&1 &
      printf '%s\\n' "$!" >"$PILOT_STATE/adbd.pid"
      return 0
      ;;
    *)
      \${BB} sleep 4 >/dev/null 2>&1 &
      printf '%s\\n' "$!" >"$PILOT_STATE/adbd.pid"
      wait "$!"
      ;;
  esac
}
PILOT_ADBD_PRODUCT=demo
PILOT_ADBD_STARTED_PHASE=started
PILOT_ADBD_DEGRADED_PHASE=degraded
PILOT_ADBD_GADGET="$GADGET"
PILOT_ADBD_PTMX="$PTMX"
PILOT_ADBD_NODE="$NODE"
PILOT_ADBD_DEV_FILE="$NODE_DEV"
PILOT_ADBD_ENABLE_DEV_FILE="$ENABLE_DEV"
PILOT_ADBD_ENABLE_NODE="$ENABLE_NODE"
PILOT_ADBD_TTYGS0_DEV_FILE="$TTY_DEV"
PILOT_ADBD_TTYGS0_NODE="$TTY_NODE"
${launchCallsite[0]}
printf 'RUNTIME_CONTINUED\\n' >>"$PILOT_STATE/events"
`;
  const runLaunch = (opts) => {
    const state = fs.mkdtempSync(path.join(fixture, 'launch-state-'));
    fs.mkdirSync(state, { recursive: true });
    const gadget = path.join(fixture, opts.gadget ? `gadget-${Math.random()}` : `missing-gadget-${Math.random()}`);
    if (opts.gadget) fs.mkdirSync(gadget);
    const ptmx = path.join(fixture, opts.ptmx ? `ptmx-${Math.random()}` : `missing-ptmx-${Math.random()}`);
    if (opts.ptmx) fs.symlinkSync('/dev/ptmx', ptmx);
    const nodeDev = path.join(fixture, `android-adb-${Math.random()}.dev`);
    const enableDev = path.join(fixture, `android-adb-enable-${Math.random()}.dev`);
    const ttyDev = path.join(fixture, `ttyGS0-${Math.random()}.dev`);
    fs.writeFileSync(nodeDev, '1:8');
    fs.writeFileSync(enableDev, '1:9');
    fs.writeFileSync(ttyDev, '5:0');
    const env = {
      ...process.env,
      BB: `${qemu} ${bb}`,
      PILOT_STATE: state,
      GADGET: gadget,
      PTMX: ptmx,
      NODE: path.join(state, 'android_adb'),
      NODE_DEV: nodeDev,
      ENABLE_DEV: enableDev,
      ENABLE_NODE: path.join(state, 'android_adb_enable'),
      TTY_DEV: ttyDev,
      TTY_NODE: path.join(state, 'ttyGS0'),
      NODE_RC: String(opts.nodeRc ?? 0),
      CONFIG_RC: String(opts.configRc ?? 0),
      ADB_MODE: opts.adbMode || 'live',
    };
    const result = cp.spawnSync(qemu, [bb, 'sh', '-c', launchHelper, 'launch', common], { encoding: 'utf8', env });
    const events = fs.existsSync(path.join(state, 'events'))
      ? fs.readFileSync(path.join(state, 'events'), 'utf8') : '';
    return { result, events };
  };

  let launch = runLaunch({ gadget: false, ptmx: true });
  assert.strictEqual(launch.result.status, 0, launch.result.stderr);
  assert(launch.events.includes('FAIL:usb-gadget:android_usb gadget absent for actual kernel; continuing without early adbd'));
  assert(launch.events.includes('PHASE:degraded'));
  assert(launch.events.includes('LOG:early USB diagnostics unavailable'));
  assert(launch.events.includes('RUNTIME_CONTINUED'));
  assert(!launch.events.includes('CONF:demo'));
  assert(!launch.events.includes('LOOP:'));

  launch = runLaunch({ gadget: true, ptmx: false });
  assert.strictEqual(launch.result.status, 0, launch.result.stderr);
  assert(launch.events.includes('FAIL:pty:early ADB requires /dev/ptmx character device 5:2 mode 0666'));
  assert(launch.events.includes('PHASE:degraded'));
  assert(launch.events.includes('LOG:early USB diagnostics unavailable'));
  assert(launch.events.includes('RUNTIME_CONTINUED'));
  assert(!launch.events.includes('CONF:demo'));
  assert(!launch.events.includes('LOOP:'));

  launch = runLaunch({ gadget: true, ptmx: true, nodeRc: 0 });
  assert.strictEqual(launch.result.status, 0, launch.result.stderr);
  assert(launch.events.includes('CONF:demo'));
  assert(launch.events.includes('NODE:'));
  assert(launch.events.includes('LOOP:'));
  assert(launch.events.includes('PHASE:started'));
  assert(launch.events.includes('RUNTIME_CONTINUED'));
  assert(!launch.events.includes('LOG:early USB diagnostics unavailable'));
  assert(!launch.events.includes('FAIL:adb:early adbd did not remain alive; runtime continuing'));

  launch = runLaunch({ gadget: true, ptmx: true, nodeRc: 0, adbMode: 'dead' });
  assert.strictEqual(launch.result.status, 0, launch.result.stderr);
  assert(launch.events.includes('FAIL:adb:early adbd did not remain alive; runtime continuing'));
  assert(launch.events.includes('PHASE:degraded'));
  assert(launch.events.includes('CONF:demo'));
  assert(launch.events.includes('LOG:early USB diagnostics unavailable'));
  assert(launch.events.includes('RUNTIME_CONTINUED'));

  launch = runLaunch({ gadget: true, ptmx: true, configRc: 1 });
  assert.strictEqual(launch.result.status, 0, launch.result.stderr);
  assert(launch.events.includes('FAIL:usb-gadget:USB gadget configuration failed'));
  assert(launch.events.includes('RUNTIME_CONTINUED'));
  assert(!launch.events.includes('LOOP:'));

  launch = runLaunch({ gadget: true, ptmx: true, nodeRc: 1 });
  assert.strictEqual(launch.result.status, 0, launch.result.stderr);
  assert(launch.events.includes('FAIL:android-adb-node:'));
  assert(launch.events.includes('RUNTIME_CONTINUED'));
  assert(!launch.events.includes('LOOP:'));

  const gadget = path.join(fixture, 'real-config-fixture');
  fs.mkdirSync(gadget);
  const configure = () => invoke('. "$1"; PILOT_ADBD_GADGET="$2"; configure_adb fixture', [common, gadget]);
  result = configure();
  assert.strictEqual(result.status, 0, result.stderr);
  assert.strictEqual(fs.readFileSync(path.join(gadget, 'functions'), 'utf8').trim(), 'adb');
  fs.mkdirSync(path.join(gadget, 'f_acm'));
  result = configure();
  assert.strictEqual(result.status, 0, result.stderr);
  assert.strictEqual(fs.readFileSync(path.join(gadget, 'functions'), 'utf8').trim(), 'acm,adb');
  assert.strictEqual(fs.readFileSync(path.join(gadget, 'enable'), 'utf8').trim(), '1');
  fs.unlinkSync(path.join(gadget, 'enable'));
  fs.mkdirSync(path.join(gadget, 'enable'));
  assert.notStrictEqual(configure().status, 0, 'a failed gadget write must remain a failure');

  const supervisorState = path.join(fixture, 'supervisor');
  fs.mkdirSync(supervisorState);
  result = invoke(`
. "$1"
PILOT_STATE="$2"
PILOT_ADBD_RUNTIME_ROOT=/runtime
PILOT_ADBD_LOG="$2/daemon.log"
pilot_failure() { echo "$*" >>"$PILOT_STATE/errors"; }
pilot_adbd_run() {
  echo "$1" >>"$PILOT_STATE/launches"
  exec \${BB} sleep 30
}
adb_loop &
supervisor=$!
cleanup() {
  \${BB} touch "$PILOT_STATE/stop-adb"
  if \${BB} test -s "$PILOT_STATE/adbd.pid"; then
    \${BB} kill "$(\${BB} cat "$PILOT_STATE/adbd.pid")" 2>/dev/null || true
  fi
  \${BB} kill "$supervisor" 2>/dev/null || true
  wait "$supervisor" 2>/dev/null || true
}
trap cleanup EXIT
for expected in early runtime; do
  count=0
  while [ "$(\${BB} cat "$PILOT_STATE/adbd-root" 2>/dev/null)" != "$expected" ]; do
    count=$((count + 1))
    \${BB} test "$count" -le 5 || exit 1
    \${BB} sleep 1
  done
  \${BB} touch "$PILOT_STATE/runtime-ready"
done
`, [common, supervisorState]);
  assert.strictEqual(result.status, 0, JSON.stringify({
    stdout: result.stdout, stderr: result.stderr,
    files: Object.fromEntries(['launches', 'errors', 'daemon.log', 'adbd-root', 'adbd-exit', 'runtime-ready']
      .filter(name => fs.existsSync(path.join(supervisorState, name)))
      .map(name => [name, fs.readFileSync(path.join(supervisorState, name), 'utf8')])),
  }));
  assert.deepStrictEqual(fs.readFileSync(path.join(supervisorState, 'launches'), 'utf8').trim().split('\n'),
    ['early', 'runtime'], 'supervisor must re-enter the actual runtime root');
  console.log('PASS: fresh-device-tree PTY metadata preflight and startup ordering (not PTY allocation), pinned input/patch failures, exact kernel dispatch, unchanged ARM BusyBox payload failure controls, no storage-writing startup calls, writable paths, handoff and early diagnostics');
} finally {
  fs.rmSync(fixture, { force: true, recursive: true });
}
