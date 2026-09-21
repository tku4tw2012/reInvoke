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
assert(process.argv.length <= 3, 'usage: node test.js [ARCHIVE]');
const archive = path.resolve(process.argv[2] || path.join(__dirname, '../../../reinvoke-archive'));
const qemu = path.join(archive, 'emulation/qemu-arm-static');
const fixture = fs.mkdtempSync(path.join(__dirname, '.shell-test-'));
const bundleFixture = fs.mkdtempSync(path.join(archive, 'build/.bundle-test-'));
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

  // A comment placed inside a backslash continuation silently truncates the
  // command: the shell removes the backslash-newline, the comment then runs to
  // end of line, and every argument on the following lines is lost. That is
  // how a logcat invocation lost -f, -r256 and -n and stopped writing a log
  // while still passing `sh -n`, which cannot see it.
  {
    const lines = patched.toString().split('\n');
    for (let i = 0; i < lines.length - 1; i += 1) {
      if (!/\\$/.test(lines[i])) continue;
      assert.ok(!/^\s*#/.test(lines[i + 1]),
        `continuation at line ${i + 1} is followed by a comment: ${lines[i + 1].trim()}`);
    }
  }
  // Candidate 4.1 behavioural changes must survive into the patched RC12 init;
  // editing tools/usb-boot/native-ram-init alone does not reach this image.
  for (const marker of [
    '/system/bin/logcat -v threadtime',
    'runtime_logger_failures=$((runtime_logger_failures + 1))',
    '"${runtime_logger_failures}" -ge 5',
    'generation_attempts % 12',
    '"${generation_hci_init}" --reset >/dev/null 2>&1 && break',
    'HCI initialization recovered after',
    'supervise identifiers',
    'supervise bluedroid',
    '/opt/bluedroid/start.sh',
  ])
    assert(patched.includes(marker), `patched init is missing 4.1 change: ${marker}`);
  assert(!patched.includes('log "HCI initialization failed; retrying"\n'),
    'unsampled HCI failure record survived the 4.1 patch');
  assert(!patched.includes('supervise bluetoothd'),
    'the BlueZ stack survived the candidate 05 replacement');
  // The stock supervisor does an unbounded "wait" on its logger after a
  // service dies. The logger reads a FIFO, so any other process still holding
  // that FIFO open keeps it alive and the supervisor never restarts the
  // service. Observed on hardware: bonefish crashed, its supervisor sat in
  // do_wait with two stale loggers alive, and the runtime lost its WAMP router
  // until it was relaunched by hand.
  assert(!patched.includes('wait "${logger_pid}"'),
    'the unbounded logger wait survived; a crashed service will never restart');
  for (const marker of [
    'logger_wait=0',
    '"${logger_wait}" -lt 5',
    'logger did not exit; supervisor continuing',
  ])
    assert(patched.includes(marker), `bounded logger wait is missing: ${marker}`);
  // The same behaviour is maintained in two places: this patch against the
  // pinned RC12 init, and tools/usb-boot/native-ram-init for any future
  // rebuild. Marker checks alone would not catch the two drifting apart.
  {
    const ramInit = fs.readFileSync(
      path.join(__dirname, '../usb-boot/native-ram-init'), 'utf8');
    const body = (text, name) => {
      const start = text.indexOf(`${name}() {\n`);
      assert(start >= 0, `missing shell function ${name}`);
      const end = text.indexOf('\n}\n', start);
      assert(end > start, `unterminated shell function ${name}`);
      return text.slice(start, end).split('\n').map(line => line.trim())
        .filter(line => line && !line.startsWith('#')).join('\n');
    };
    for (const name of ['start_runtime_logger', 'run_bluetoothd_generation'])
      assert.equal(body(patched.toString(), name), body(ramInit, name),
        `${name} diverged between patch-runtime.js and usb-boot/native-ram-init`);
  }
  assert.throws(() => patchRuntime(Buffer.concat([init, Buffer.from('\n')])), /hash mismatch/);
  const patchedFile = path.join(fixture, 'init');
  fs.writeFileSync(patchedFile, patched);
  for (const file of ['bootstrap.sh', 'bsl-init.sh', 'common.sh', 'kernel.sh',
    'ssh-start.sh', 'usb-adb-start.sh', 'persistence-start.sh'])
    check(cp.spawnSync(qemu, [bb, 'sh', '-n', path.join(__dirname, file)], { encoding: 'utf8' }));
  check(cp.spawnSync(qemu, [bb, 'sh', '-n', patchedFile], { encoding: 'utf8' }));
  for (const release of ['3.8.13-yocto-standard', '3.8.13-reinvoke-audio-sd8887'])
    check(invoke('. "$1"; pilot_select_kernel "$2"', [kernel, release]));
  assert.equal(invoke('. "$1"; pilot_select_kernel 3.8.13-unreviewed', [kernel]).status, 1);
  const bootstrap = fs.readFileSync(path.join(__dirname, 'bootstrap.sh'), 'utf8');
  const bsl = fs.readFileSync(path.join(__dirname, 'bsl-init.sh'), 'utf8');
  // The scheme itself, not just this build's number. Builds are
  // ERA.MILESTONE.ITERATION and docs/versions.md explains what the parts
  // mean and maps the six older naming conventions this replaced. A build
  // number that stops matching that shape leaves the mapping unreadable.
  {
    const parts = /^(\d+)\.(\d+)\.(\d+)$/.exec(lib.CANDIDATE);
    assert(parts, `CANDIDATE ${lib.CANDIDATE} is not ERA.MILESTONE.ITERATION`);
    // Era 1 needed a host to boot and era 2 boots from NAND. Nothing ships
    // from era 1 any more, and an era beyond 2 does not exist yet, so a
    // number outside that range means the old zero-padded naming has crept
    // back rather than that a new era began.
    const era = Number(parts[1]);
    assert(era === 2,
      `CANDIDATE ${lib.CANDIDATE} is not in era 2; see docs/versions.md`);
    assert(!/^0/.test(parts[1]) && !/^0\d/.test(parts[2]),
      `CANDIDATE ${lib.CANDIDATE} is zero-padded like the old scheme`);
  }
  {
    const versions = fs.readFileSync(
      path.join(__dirname, '../../docs/versions.md'), 'utf8');
    assert(versions.includes(lib.CANDIDATE),
      `docs/versions.md does not mention ${lib.CANDIDATE}`);
    // The unit in hand runs 2.2.7, built as 05.8.13. Losing that pairing
    // means nobody can tell what is installed.
    assert(/2\.2\.7[\s\S]{0,400}05\.8\.13|05\.8\.13[\s\S]{0,400}2\.2\.7/
      .test(versions),
      'docs/versions.md no longer maps the installed build to its old name');
  }
  // The voice output stage: the donor's LADSPA equaliser and the one
  // library it needs. Both must stay under /usr/lib, because elfClosure
  // classifies an object by where it sits and resolves a /usr/lib object
  // against /lib and /usr/lib only. Splitting them put the library in the
  // runtime's private directory and the build refused it.
  {
    const voice = require('./voice-config');
    assert.deepEqual(voice.validateVoice(undefined), { enabled: false });
    assert.deepEqual(voice.validateVoice({ enabled: true }), { enabled: true });
    assert.throws(() => voice.validateVoice({ enabled: true, extra: 1 }),
      /unknown voiceOutput key/);
    assert.throws(() => voice.validateVoice([]), /must be an object/);
    for (const [, destination] of voice.VOICE_FILES) {
      assert(destination.startsWith('usr/lib/'),
        `${destination} sits outside the loader family elfClosure uses`);
    }
  }

  assert.equal(lib.CANDIDATE, '2.2.10');
  // Not a copy of the constant, which only forces an edit in two places when
  // the date moves. The date is stamped into /etc/nand-pilot/build-id on the
  // device, and 2.2.8 was first assembled carrying the previous day left over
  // from the version rename, so check it is a real calendar date that has
  // actually happened and that the version in it is the one being built.
  {
    const parts = /^reInvoke-(\d+\.\d+\.\d+)-(\d{4})(\d{2})(\d{2})$/
      .exec(lib.BUILD_ID);
    assert(parts, `BUILD_ID ${lib.BUILD_ID} is not reInvoke-VERSION-YYYYMMDD`);
    assert.equal(parts[1], lib.CANDIDATE,
      `BUILD_ID ${lib.BUILD_ID} names a different version than ${lib.CANDIDATE}`);
    const [year, month, day] = [+parts[2], +parts[3], +parts[4]];
    const stamped = new Date(Date.UTC(year, month - 1, day));
    assert(stamped.getUTCFullYear() === year &&
      stamped.getUTCMonth() === month - 1 && stamped.getUTCDate() === day,
      `BUILD_ID ${lib.BUILD_ID} is not a real date`);
    assert(stamped.getTime() <= Date.now(),
      `BUILD_ID ${lib.BUILD_ID} is dated in the future`);
  }
  assert.equal(lib.BLUETOOTH_NAME, 'reInvoke-2.2.10');
  assert.equal(lib.BUNDLE_NAME, '83_IMAGE.reinvoke-2.2.10');
  // The bootstrap no longer launches an early USB ADB daemon. That launcher
  // was written for booting from RAM over USB, where the boot ROM had already
  // put the port in device mode. Booting from NAND there is no gadget until
  // the runtime loads one, and the launcher spent the boot polling for it and
  // then reconfigured the gadget out from under the runtime the moment it
  // appeared, rewriting functions and iProduct behind it.
  assert(!bootstrap.includes('pilot_usb_adbd_launch'));
  assert(!bootstrap.includes('PILOT_ADBD_PRODUCT'));
  // The BSL still carries it: that path is the writer, and it never reaches
  // the runtime that owns USB ADB.
  assert(bsl.includes(`PILOT_ADBD_PRODUCT=${lib.BLUETOOTH_NAME}`));

  // The gadget identity must be written before the gadget is enabled. The host
  // reads string descriptors at enumeration and caches them, so an identity
  // written afterwards changes sysfs and nothing else: the unit still appears
  // as the driver's compiled-in 0123456789ABCDEF in adb devices while every
  // file on the device reads correctly. Observed exactly that way on this unit.
  {
    // The recovery path sets the same three strings in common.sh, and has
    // the same ordering requirement. It went unguarded while the runtime was
    // guarded, which is how it shipped for releases enumerating as the
    // driver's own Android/0123456789ABCDEF.
    {
      const bsl = fs.readFileSync(path.join(__dirname, 'common.sh'), 'utf8');
      const enableAt = bsl.indexOf('echo 1 >"${gadget}/enable"');
      assert(enableAt > 0, 'common.sh never enables the gadget');
      for (const name of ['iSerial', 'iProduct', 'iManufacturer']) {
        const at = bsl.indexOf(`>"\${gadget}/${name}"`);
        assert(at > 0, `common.sh never writes ${name}`);
        assert(at < enableAt,
          `${name} is written after the gadget is enabled; the host caches ` +
          'string descriptors at enumeration and will not see it');
      }
    }
    const up = fs.readFileSync(path.join(__dirname, 'usb-adb-start.sh'), 'utf8');
    const enable = up.indexOf('> "${USB_ADB_GADGET}/enable"');
    assert(enable >= 0, 'usb-adb-start.sh never enables the gadget');
    // All three descriptor strings, not just the serial. android_bind fills
    // them with "Android", "Android" and "0123456789ABCDEF" and exposes these
    // attributes so the product replaces them. A unit still reporting those
    // has never been configured, which is what this speaker did report.
    for (const name of ['iSerial', 'iProduct', 'iManufacturer']) {
      const at = up.indexOf('/' + name);
      assert(at >= 0, `usb-adb-start.sh never writes ${name}`);
      assert(at < enable,
        `${name} is written after the gadget is enabled; the host will not see it`);
    }
    // The identity comes from the Wi-Fi MAC, which exists even unassociated.
    assert(up.includes('/sys/class/net/mlan0/address'),
      'the gadget identity no longer derives from the Wi-Fi MAC');
  }
  const oldMain = path.join(fixture, 'old-main');
  const oldBSL = path.join(fixture, 'old-bsl');
  fs.mkdirSync(oldMain);
  fs.mkdirSync(oldBSL);
  const oldBuildId = 'reInvoke-NAND-03-20260912';
  lib.json(path.join(oldMain, 'PROPOSAL.json'), { buildId: oldBuildId });
  lib.json(path.join(oldBSL, 'MANIFEST.json'), { buildId: oldBuildId });
  for (const [script, args, message] of [
    ['build-bsl.js', [archive, path.join(fixture, 'rejected-bsl'), oldMain],
      /main candidate does not match this builder/],
    ['compact-bsl.js', [archive, path.join(fixture, 'rejected-compact'), oldBSL],
      /BSL candidate does not match this builder/],
    ['native-bundle.js', [archive, oldMain, oldBSL, path.join(bundleFixture, 'rejected-bundle')],
      /main candidate does not match this builder/],
  ]) {
    const result = cp.spawnSync(process.execPath, [path.join(__dirname, script), ...args],
      { encoding: 'utf8', timeout: 30000 });
    assert.notEqual(result.status, 0, 'retired artifacts must not be relabeled');
    assert.match(result.stderr, message);
  }
  lib.json(path.join(oldMain, 'PROPOSAL.json'), { buildId: lib.BUILD_ID });
  const mixed = cp.spawnSync(process.execPath, [path.join(__dirname, 'native-bundle.js'),
    archive, oldMain, oldBSL, path.join(bundleFixture, 'rejected-mixed-bundle')],
  { encoding: 'utf8', timeout: 30000 });
  assert.notEqual(mixed.status, 0);
  assert.match(mixed.stderr, /BSL candidate does not match this builder/);
  assert(!bsl.includes('fallback.pid'), 'no competing BSL transport resurrection');
  assert(bsl.indexOf('stop_boot rootfs-hash') < bsl.indexOf('exec ${BB} chroot /nand-root'));
  assert(bootstrap.includes('exec ${BB} chroot /runtime /bin/busybox sh /init'));
  assert(!/(?:\$\{BB\}\s+|\/(?:bin|sbin)\/)(?:nandwrite|ubiupdatevol|flash_custk|flash_erase(?:all)?)\b/
    .test(bootstrap + bsl + patched + fs.readFileSync(common)));
  assert(!fs.readFileSync(kernel, 'utf8').includes('mac_addr=02:'), 'no hardcoded fleet MAC');
  assert(patched.includes('pilot_ssh_start'));
  assert(patched.includes('pilot_usb_adb_up'));
  // Every flag the init hands mcu-interface must be one that service defines.
  // Removing the amplifier owner restriction dropped a flag from here while
  // the service still refused to start without it, and it crash-looped on
  // hardware: LEDs, buttons, volume and the boot cue all went with it.
  {
    const start = patched.indexOf('supervise mcu-interface');
    assert(start >= 0, 'init does not supervise mcu-interface');
    const invocation = [];
    for (const line of patched.slice(start).split('\n')) {
      invocation.push(line);
      if (!line.trimEnd().endsWith('\\')) break;
    }
    const passed = invocation.join('\n').match(/--[a-z0-9-]+/g) || [];
    const source = fs.readFileSync(path.join(__dirname, '../mcu-interface/main.go'), 'utf8');
    const defined = new Set(
      [...source.matchAll(/flag\.(?:String|Int|Bool|Duration)\(\s*"([a-z0-9-]+)"/g)]
        .map(match => match[1]));
    assert(defined.size >= 10, 'flag definitions were not found in mcu-interface');
    for (const flag of passed) {
      assert(defined.has(flag.slice(2)),
        `init passes ${flag} but mcu-interface does not define it`);
    }
    // A blank line inside a continued command silently truncates it. That is
    // not a style point: a removal that left one shipped a service running on
    // three flags instead of thirteen, with no error anywhere, and the boot
    // cue simply never happened.
    assert(!invocation.join('\n').includes('\\\n\n'),
      'the mcu-interface invocation contains a blank continuation line');
    // Count the flags as the shell would, so a truncation is caught by its
    // effect rather than by recognising one way of causing it.
    assert(passed.length >= 13,
      `init passes only ${passed.length} flags to mcu-interface; ` +
      'the invocation is truncated');
  }
  // Teardown must be on the shutdown path. Leaving adbd asleep inside the
  // gadget driver and the driver holding the USB controller stopped this unit
  // completing a soft reboot; it had to be power cycled.
  assert(patched.includes('pilot_usb_adb_down'));
  assert(patched.includes('pilot_persistence_start'));
  assert(patched.includes('supervise provision-windowd pilot_resume_then_exec'));
  assert(patched.includes('--music-volume-state /run/reinvoke/music-volume'));
  assert(patched.indexOf('pilot_persistence_start ||') < patched.indexOf('pilot_usb_adb_up ||'));
  assert(patched.indexOf('pilot_persistence_start ||') < patched.indexOf('supervise mcu-interface'));
  assert(patched.indexOf('wait_service_stop bluetoothd') < patched.indexOf('stop_service persistence'));
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
  fs.rmSync(bundleFixture, { recursive: true, force: true });
}
