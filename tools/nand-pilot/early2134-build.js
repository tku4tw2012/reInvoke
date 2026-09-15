// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
'use strict';
const assert = require('assert');
const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const cp = require('child_process');
const zlib = require('zlib');

const pins = {
  image: { bytes: 46370816, sha256: '4ec3feef37707168a9d317b360099e884ec3b9ec5651c592ec7a969a0cddaa45' },
  initRC: { bytes: 8862, sha256: '2b2a189751d3a2d7c9c5dcfba55da2ab5374cc3d0d1bbc03e809db1289442d14' },
  init: { bytes: 63884, sha256: '59893db4cd70e298e085d1dba326f467edfbc49165857f182026978e23906010' },
  adbd: { bytes: 79768, sha256: '7a142bbf80eeefd29f8bb1be24e3c9b9d39f55f3f7d7538a7501e6cb646e3a55' },
};
const helperPath = 'sbin/early2134-adb.sh';
const helperPin = { bytes: 2167, sha256: '286efb551a8d4b70b8c19a170721f7431fda0b9125b2fe6d02a83a08d45d40a0' };
const product = 'RI2134-ADB01';
const sha = bytes => crypto.createHash('sha256').update(bytes).digest('hex');
function verify(bytes, pin) {
  assert.strictEqual(bytes.length, pin.bytes, 'source size mismatch');
  assert.strictEqual(sha(bytes), pin.sha256, 'source hash mismatch');
}
function once(text, before, after) {
  assert.strictEqual(text.split(before).length, 2, 'patch anchor mismatch');
  return text.replace(before, after);
}
function patchInit(bytes) {
  verify(bytes, pins.initRC);
  let text = bytes.toString();
  const start = text.indexOf('# Enable ADB and ACM\n');
  const end = text.indexOf('\n    # create directory for wpa_supplicant', start);
  assert(start > 0 && end > start);
  const oldUSB = text.slice(start, end);
  assert.strictEqual((oldUSB.match(/^\s+write /gm) || []).length, 5);
  let usb = once(oldUSB, '"MRVL USB SDK"', `"${product}"`);
  usb = once(usb, '    write /sys/class/android_usb/android0/enable "1"\n', '');
  usb += '\n    start adbd\n    write /sys/class/android_usb/android0/enable "1"\n';
  text = once(text, oldUSB, '# USB configuration moved to on init; do not reset the active gadget.\n');
  const early = '\n    # Diagnostic before vendor mounts, /data waits and module/network startup.\n' +
    '    write /run/early2134-phase-01 on-init-entered\n' +
    '    exec /bin/sh /sbin/early2134-adb.sh prepare\n' + usb +
    '    exec /bin/sh /sbin/early2134-adb.sh observe\n' +
    '    write /run/early2134-phase-02 on-init-adbd-start-requested\n';
  // The actual vendor write builtin uses O_CREAT without O_TRUNC: use separate files.
  text = once(text, '\non fs\n', early + '\non fs\n    write /run/early2134-phase-03 fs-before-mount\n');
  text = once(text, '    exec /bin/sh /sbin/mount_partition.sh\n',
    '    exec /bin/sh /sbin/mount_partition.sh\n    write /run/early2134-phase-04 fs-after-mount-before-data\n');
  text = once(text, '    wait /data/\n',
    '    wait /data/\n    write /run/early2134-phase-05 fs-after-data\n');
  text = once(text, '    exec /bin/sh /usr/bin/load-kmod.sh\n',
    '    exec /bin/sh /usr/bin/load-kmod.sh\n    write /run/early2134-phase-06 fs-after-modules\n');
  text = once(text, '\non post-fs\n', '\non post-fs\n    write /run/early2134-phase-07 post-fs-entered\n');
  text = once(text, '\non boot\n', '\non boot\n    write /run/early2134-phase-08 boot-entered\n');
  text = once(text, 'service adbd /sbin/adbd\n    disabled\n', 'service adbd /sbin/adbd\n');
  return Buffer.from(text);
}
function verifyPatch(original, candidate) {
  assert(candidate.equals(patchInit(original)), 'tampered or unexpected init patch');
}
function run(command, args, options = {}) {
  const result = cp.spawnSync(command, args, {
    encoding: 'utf8', maxBuffer: 32 * 1024 * 1024, ...options,
  });
  if (result.error) throw result.error;
  assert.strictEqual(result.status, 0, `${command} failed: ${result.stderr || result.stdout}`);
  return result.stdout;
}
function logged(output, name, command, args, options = {}) {
  const result = cp.spawnSync(command, args, {
    encoding: 'utf8', maxBuffer: 32 * 1024 * 1024, ...options,
  });
  fs.writeFileSync(path.join(output, name), (result.stdout || '') + (result.stderr || ''));
  if (result.error) throw result.error;
  assert.strictEqual(result.status, 0, `${command} failed; see ${name}`);
}
function snapshot(root) {
  // GNU find/stat see fakeroot's ownership and device records; Node's statx may not.
  const fields = run('find', ['.', '-printf', '%p\\0%y\\0%m\\0%U\\0%G\\0%T@\\0%n\\0%l\\0%i\\0'],
    { cwd: root }).split('\0');
  fields.pop();
  assert.strictEqual(fields.length % 9, 0);
  const entries = [];
  for (let i = 0; i < fields.length; i += 9) {
    const [name, type, mode, uid, gid, mtime, nlink, link, inode] = fields.slice(i, i + 9);
    const entry = { name, type, mode, uid, gid, mtime, nlink, link, inode };
    if (type === 'f') entry.sha256 = sha(fs.readFileSync(path.join(root, name)));
    if (type === 'c' || type === 'b')
      entry.device = run('stat', ['-c', '%t:%T', path.join(root, name)]).trim();
    entries.push(entry);
  }
  entries.sort((a, b) => a.name < b.name ? -1 : a.name > b.name ? 1 : 0);
  const first = new Map();
  for (const entry of entries) {
    if (entry.type === 'f') {
      if (!first.has(entry.inode)) first.set(entry.inode, entry.name);
      entry.hardlink = first.get(entry.inode);
    }
    delete entry.inode;
  }
  return entries;
}
function compareTrees(source, candidate) {
  const added = candidate.filter(e => !source.some(s => s.name === e.name));
  assert.strictEqual(added.length, 1, 'unexpected added entries');
  assert.strictEqual(added[0].name, './' + helperPath);
  assert.strictEqual(added[0].type, 'f');
  assert.strictEqual(added[0].mode, '755');
  assert.strictEqual(added[0].uid + ':' + added[0].gid, '0:0');
  const originalEntries = candidate.filter(e => e.name !== './' + helperPath);
  const omitInitHash = entries => entries.map(e => e.name === './init.rc' ? { ...e, sha256: '' } : e);
  assert.deepStrictEqual(omitInitHash(originalEntries), omitInitHash(source),
    'original content, metadata, devices, symlinks or hardlinks changed');
  assert.notStrictEqual(source.find(e => e.name === './init.rc').sha256,
    candidate.find(e => e.name === './init.rc').sha256);
}
function json(output, name, value) {
  fs.writeFileSync(path.join(output, name), JSON.stringify(value, null, 2) + '\n');
}
function verifyVendorEvidence(tree) {
  const init = fs.readFileSync(path.join(tree, 'init'));
  const adbd = fs.readFileSync(path.join(tree, 'sbin/adbd'));
  const resolve = (bytes, base, instruction, literal, expected) => {
    const at = instruction + 4 + bytes.readInt32LE(literal - base) - base;
    assert.strictEqual(bytes.subarray(at, bytes.indexOf(0, at)).toString(), expected);
  };
  for (const [instruction, literal, text] of [
    [0x62f6, 0x62fc, '/default.prop'],
    [0x301a, 0x31e4, 'early-init'],
    [0x3026, 0x31e8, 'wait_for_coldboot_done'],
    [0x3032, 0x31f0, 'keychord_init'],
    [0x303e, 0x31f8, 'console_init'],
    [0x304a, 0x3200, 'init'],
    [0x3054, 0x3204, 'early-fs'],
    [0x305e, 0x3208, 'fs'],
  ]) resolve(init, 0, instruction, literal, text);
  resolve(adbd, 0x10000, 0x1336c, 0x1343c, 'persist.adb.trace_mask');
  resolve(adbd, 0x10000, 0x133a8, 0x13448, '/data/adb');
  resolve(adbd, 0x10000, 0x133cc, 0x1344c, '/data/adb/adb-%Y-%m-%d-%H-%M-%S.txt');
  assert.strictEqual(init.readUInt16LE(0x3620), 0x2141, 'write open flags must be O_WRONLY|O_CREAT');
  assert.strictEqual(adbd.readUInt16LE(0x133e4 - 0x10000), 0xdbcf,
    'trace log open failure must return, not terminate startup');
}
function kernelInflater(output, offset) {
  const kernel = path.resolve(__dirname,
    '../../../reinvoke-archive/sources/harman/invoke-kernel/Invoke-kernel');
  const harness = path.resolve(__dirname, '../nand-inspect/squashmin-validation');
  const wrapper = fs.readFileSync(path.join(kernel, 'fs/squashfs/zlib_wrapper.c'), 'utf8');
  const body = wrapper.match(/^static int zlib_uncompress\([\s\S]*?^}/m);
  assert(body, 'vendor wrapper function not found');
  fs.writeFileSync(path.join(output, 'zlib_uncompress.inc'), body[0] + '\n');
  const inputs = ['fs/squashfs/zlib_wrapper.c', 'lib/zlib_inflate/inflate.c',
    'lib/zlib_inflate/inffast.c', 'lib/zlib_inflate/inftrees.c',
    'include/linux/zlib.h', 'include/linux/zutil.h', 'include/linux/zconf.h'];
  json(output, 'kernel-inflater-sources.json', { kernel, inputs: inputs.map(name =>
    ({ path: name, sha256: sha(fs.readFileSync(path.join(kernel, name))) })) });
  logged(output, 'kernel-compile.log', 'cc', ['-O2', '-Wall', '-Wextra', '-Wno-unused-parameter',
    '-I' + path.join(harness, 'kernel-shim'), '-I' + path.join(kernel, 'include'), '-I' + output,
    path.join(harness, 'kernel-inflate-check.c'),
    ...inputs.slice(1, 4).map(name => path.join(kernel, name)),
    '-o', path.join(output, 'kernel-inflate-check')]);
  logged(output, 'kernel-inflate.log', path.join(output, 'kernel-inflate-check'), [
    path.join(output, 'inode-metadata.zlib'), path.join(output, 'inode-metadata.expanded'), String(offset)]);
}
function profile(image, source = false) {
  assert.strictEqual(image.toString('ascii', 0, 4), 'hsqs');
  assert.strictEqual(image.readUInt16LE(28), 4);
  assert.strictEqual(image.readUInt16LE(30), 0);
  assert.strictEqual(image.readUInt16LE(20), 1, 'require gzip/zlib');
  assert.strictEqual(image.readUInt32LE(12), 131072);
  assert.strictEqual(image.readUInt16LE(24) & 0x400, 0, 'no compressor options extension');
  const result = { bytes: image.length, sha256: sha(image),
    bytes_used: Number(image.readBigUInt64LE(40)), inodes: image.readUInt32LE(4),
    fragments: image.readUInt32LE(16), block_bytes: image.readUInt32LE(12),
    compression: 'gzip (RFC1950 zlib streams)', mkfs_time: image.readUInt32LE(8) };
  assert(result.bytes_used <= image.length && image.length <= 90 * 1024 * 1024);
  if (source) {
    verify(image, pins.image);
    assert.strictEqual(result.bytes_used, 46369609);
    assert.strictEqual(result.inodes, 3812);
    assert.strictEqual(result.fragments, 263);
  }
  return result;
}
function buildChild(source, output) {
  assert(process.env.FAKEROOTKEY, 'build must run in fakeroot');
  const input = fs.readFileSync(source);
  const sourceProfile = profile(input, true);
  const tree = path.join(output, 'work/source');
  const packed = path.join(output, 'work/packed');
  logged(output, 'extract-source.log', 'unsquashfs',
    ['-processors', '1', '-no-progress', '-dest', tree, source]);
  const original = fs.readFileSync(path.join(tree, 'init.rc'));
  verify(original, pins.initRC);
  verify(fs.readFileSync(path.join(tree, 'init')), pins.init);
  verify(fs.readFileSync(path.join(tree, 'sbin/adbd')), pins.adbd);
  verifyVendorEvidence(tree);
  assert.strictEqual(fs.readFileSync(path.join(tree, 'etc/version.txt'), 'utf8').trim(),
    'Barracuda_libre-12.2134.0');
  const before = snapshot(tree);
  assert.strictEqual(before.find(e => e.name === './data').link, '/lsync/data1');
  assert(!before.some(e => e.name.startsWith('./run/early2134')), 'no baked checkpoints');
  json(output, 'source-files.json', before);
  run('cp', ['-p', path.join(tree, 'init.rc'), path.join(output, 'original-init.rc')]);
  run('touch', ['-r', path.join(tree, 'sbin'), path.join(output, 'work/sbin-time')]);
  const candidate = patchInit(original);
  verifyPatch(original, candidate);
  fs.writeFileSync(path.join(tree, 'init.rc'), candidate);
  const helper = fs.readFileSync(path.join(__dirname, 'early2134-adb.sh'));
  verify(helper, helperPin);
  run('cp', [path.join(__dirname, 'early2134-adb.sh'), path.join(tree, helperPath)]);
  run('chmod', ['0755', path.join(tree, helperPath)]);
  run('chown', ['0:0', path.join(tree, helperPath)]);
  run('touch', ['-r', path.join(output, 'original-init.rc'),
    path.join(tree, 'init.rc'), path.join(tree, helperPath)]);
  run('touch', ['-r', path.join(output, 'work/sbin-time'), path.join(tree, 'sbin')]);
  const expected = snapshot(tree);
  compareTrees(before, expected);
  fs.writeFileSync(path.join(output, 'candidate-init.rc'), candidate);
  fs.writeFileSync(path.join(output, 'early2134-adb.sh'), helper);
  const diff = cp.spawnSync('diff', ['-u', path.join(output, 'original-init.rc'),
    path.join(output, 'candidate-init.rc')], { encoding: 'utf8' });
  assert.strictEqual(diff.status, 1);
  fs.writeFileSync(path.join(output, 'init.diff'), diff.stdout);
  logged(output, 'repack.log', 'mksquashfs', [tree, path.join(output, 'rootfs.squashfs'),
    '-noappend', '-comp', 'gzip', '-b', '131072', '-mkfs-time', String(sourceProfile.mkfs_time),
    '-processors', '1', '-no-progress', '-no-recovery', '-no-xattrs']);
  logged(output, 'extract-candidate.log', 'unsquashfs',
    ['-processors', '1', '-no-progress', '-dest', packed, path.join(output, 'rootfs.squashfs')]);
  const after = snapshot(packed);
  assert.deepStrictEqual(after, expected, 'independent extraction differs');
  compareTrees(before, after);
  verifyPatch(original, fs.readFileSync(path.join(packed, 'init.rc')));
  assert(fs.readFileSync(path.join(packed, helperPath)).equals(helper));
  json(output, 'candidate-files.json', after);
  const qemu = '/usr/bin/qemu-arm-static';
  const loader = path.join(packed, 'lib/ld-2.23.so');
  const libpath = [path.join(packed, 'lib'), path.join(packed, 'usr/lib')].join(':');
  for (const executable of ['init', 'sbin/adbd', 'bin/bash', 'bin/mknod.coreutils', 'bin/stat.coreutils']) {
    logged(output, 'loader-' + executable.replaceAll('/', '-') + '.log', qemu,
      ['-L', packed, loader, '--library-path', libpath, '--list', path.join(packed, executable)]);
  }
  logged(output, 'arm-bash-syntax.log', qemu, ['-L', packed, loader, '--library-path', libpath,
    path.join(packed, 'bin/bash'), '--noprofile', '--norc', '-n', path.join(packed, helperPath)]);
  logged(output, 'arm-bash-execution.log', qemu, ['-L', packed, loader, '--library-path', libpath,
    path.join(packed, 'bin/bash'), '--noprofile', '--norc', '-c',
    'printf "PASS: image ARM bash executed; diagnostic helper and adbd NOT executed\\n"']);
  const image = fs.readFileSync(path.join(output, 'rootfs.squashfs'));
  const candidateProfile = profile(image);
  const inodeTable = Number(image.readBigUInt64LE(64));
  const header = image.readUInt16LE(inodeTable);
  assert.strictEqual(header & 0x8000, 0, 'expected compressed inode metadata');
  const compressed = image.subarray(inodeTable + 2, inodeTable + 2 + (header & 0x7fff));
  const expanded = zlib.inflateSync(compressed);
  assert(expanded.length > 0 && expanded.length <= 8192);
  fs.writeFileSync(path.join(output, 'inode-metadata.zlib'), compressed);
  fs.writeFileSync(path.join(output, 'inode-metadata.expanded'), expanded);
  kernelInflater(output, inodeTable + 2);
  json(output, 'MANIFEST.json', {
    purpose: 'OFFLINE 12.2134.0 early native-NAND ADB diagnostic prerequisite; not full reInvoke',
    boot_verified: false, write_approved: false, source: { path: source, ...sourceProfile },
    source_pins: pins, helper_pin: helperPin, candidate: candidateProfile, usb_product: product,
    build_tools: ['early2134-build.js', 'early2134-test.js', 'early2134-adb.sh'].map(name =>
      ({ path: 'tools/nand-pilot/' + name, sha256: sha(fs.readFileSync(path.join(__dirname, name))) })),
    differences: [
      { path: '/init.rc', action: 'modify', original_sha256: sha(original), sha256: sha(candidate),
        bytes: candidate.length, metadata: 'unchanged' },
      { path: '/' + helperPath, action: 'add', sha256: sha(helper), bytes: helper.length,
        mode: '0755', uid: 0, gid: 0 },
    ],
    validation: { exact_original_entries_preserved: before.length,
      all_other_regular_files_identical: true, metadata_symlinks_devices_hardlinks_compared: true,
      full_independent_extraction: true, own_arm_loader_and_bash: true,
      actual_adbd_executed: false, source_data_symlink: '/lsync/data1',
      kernel_inflater: 'PASS candidate inode-metadata sample, exact kernel wrapper and inflater; 1024/4096-byte device blocks, complete consumption and corruption rejection',
      inode_metadata_stream_offset: inodeTable + 2 },
    vendor_binary_evidence: {
      init: 'Pinned init loads /default.prop at 0x2ffc via 0x62f4 before queuing early-init; normal queue at 0x301e..0x3060 orders coldboot, console, init, early-fs, fs.',
      exec: 'Vendor exec handler 0x36e4 calls 0x3420; fork at 0x3464, synchronous waitpid at 0x3484. Helper never waits for NAND or networking.',
      write: 'Vendor write_file 0x3618 opens with 0x41 (O_WRONLY|O_CREAT), without O_TRUNC. Eight distinct volatile phase files prevent stale suffixes.',
      adbd_data: 'Pinned adbd main calls trace setup 0x13348; /data/adb logging only follows successful persist.adb.trace_mask integer parse. Log open failure branches at 0x133e4 back to return path. No /data mount prerequisite or authentication-key path found.',
      properties: 'Stock /default.prop supplies ro.secure=0 and ro.debuggable=1. Property service socket starts later; property area and default property values are prepared before normal on-init.',
    },
    caveats: [
      'Full recompression; NOT the 2050 two-block layout. Parent must compose and independently size write extents.',
      'Use coherent 12.2134.0 boot/kernel/modules; target gadget driver, coldplug, property area and PTYs remain hardware prerequisites.',
      'Existing vendor mounts, services, persistent data and update policy are preserved; no factory reset or flashing commands added.',
      'Volatile /run checkpoints record executed phases and sysfs readbacks, not a working transport or proof of native boot.',
      'Init starts stock adbd even if helper reports a missing gadget/node; configuration failures are not success.',
    ],
  });
  fs.writeFileSync(path.join(output, 'validation.log'),
    'PASS donor pin/profile; only init.rc changed and sbin/early2134-adb.sh added\n' +
    'PASS independent full extraction: original modes, owners, mtimes, links, devices and file hashes preserved\n' +
    'PASS own ARM loader checks and actual ARM bash syntax/execution (no adbd/helper runtime execution)\n' +
    'PASS actual vendor kernel inflater on candidate inode-metadata sample, including invalid-stream rejection\n' +
    'PASS SquashFS 4.0 gzip, 128KiB blocks, no compressor-options extension, within 90MiB\n' +
    'OFFLINE ONLY: target boot and native USB/ADB transport unverified\n');
}
function main(args) {
  if (args[0] === '--child') return buildChild(args[1], args[2]);
  assert.strictEqual(args.length, 2, 'Usage: node early2134-build.js SOURCE NEW_OUTPUT_DIRECTORY');
  const source = path.resolve(args[0]), output = path.resolve(args[1]);
  assert(fs.lstatSync(source).isFile(), 'source must be a regular file, not a symlink/device');
  profile(fs.readFileSync(source), true);
  fs.mkdirSync(output, { mode: 0o700 });
  fs.mkdirSync(path.join(output, 'work'), { mode: 0o700 });
  const env = { ...process.env, TMPDIR: path.join(output, 'work') };
  logged(output, 'build-driver.log', 'nice', ['-n', '10', 'fakeroot', '--',
    process.execPath, __filename, '--child', source, output], { env });
  fs.rmSync(path.join(output, 'work'), { recursive: true });
  const sums = fs.readdirSync(output).sort().map(name =>
    `${sha(fs.readFileSync(path.join(output, name)))}  ${name}\n`).join('');
  fs.writeFileSync(path.join(output, 'SHA256SUMS'), sums);
  console.log(fs.readFileSync(path.join(output, 'validation.log'), 'utf8').trim());
}
module.exports = { pins, helperPin, sha, verify, patchInit, verifyPatch, compareTrees, profile };
if (require.main === module) main(process.argv.slice(2));
