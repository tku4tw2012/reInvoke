// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
'use strict';
const fs = require('fs');
const path = require('path');
const assert = require('assert');
const lib = require('./build-lib');

const archive = path.resolve(process.argv[2] || '../reinvoke-archive');
const output = path.resolve(process.argv[3] || '');
assert(process.argv.length === 4 || process.argv.length === 5,
  'usage: fakeroot node build-bsl.js ARCHIVE NEW_OUTPUT [PILOT_ARTIFACT]');
assert(!fs.existsSync(output), 'output already exists');
fs.mkdirSync(output, { mode: 0o700 });
const root = path.join(output, 'root');
const pilotArtifact = path.resolve(process.argv[4] ||
  path.join(archive, 'build/artifacts/reinvoke-nand-pilot-01-20260909-pty01'));
const pilot = path.join(pilotArtifact, 'rootfs.squashfs');
const pilotProposal = JSON.parse(fs.readFileSync(path.join(pilotArtifact, 'PROPOSAL.json'), 'utf8'));
const rc12 = path.join(archive, 'build/artifacts/rc12-nand-handoff-20260910/root');
const readback = path.join(archive, 'evidence/nand-postbundle-yellow-20260911T0149Z/readback');
const helper = path.join(output, 'set-private-loop-offset');
lib.verify(pilot, pilotProposal.image);
assert.equal(pilotProposal.extent.start, 0x02920000);
const loopBytes = Math.ceil(pilotProposal.image.bytes / 131072) * 131072;
assert(loopBytes > 0 && loopBytes <= 0x05a00000, 'pilot exceeds rootfs allocation');
lib.run('arm-linux-gnueabihf-gcc', ['-Os', '-static', '-s', '-Wall', '-Wextra', '-Werror',
  `-DPILOT_ROOTFS_BYTES=${loopBytes}`, path.join(__dirname, 'bsl-loop-view.c'), '-o', helper]);
assert.equal(lib.hashFile(path.join(rc12, 'sbin/ueventd')), '878cefdf48568f08e3fee7c26e93ba790787c67f082170ff80d8b3a264f71891');
assert.equal(lib.hashFile(path.join(rc12, 'ueventd.rc')), '832846abb447cb538fe125dc90c07e9074c3672cc5b11016e9b66a2f8484d20d');
assert.equal(lib.hashFile(path.join(rc12, 'lib/libglibc_bridge.so')), '6d23bd1152e8f71abb212b2d637ea3728f1d36d5663bc80ccfc2c756a71de45c');
const original = fs.readFileSync(path.join(readback, 'bsl-with-neighbors.bin'));
assert.equal(original.length, 42 * 131072);
const baseline = original.subarray(131072, 41 * 131072);
assert.equal(lib.sha(baseline), '238187a1d490d43dc9e44c658f88735900d3b6dfc7ed312313b9d267156dd92c');
lib.run('unsquashfs', ['-processors', '1', '-no-progress', '-d', root, pilot]);
const targetInitSHA256 = lib.hashFile(path.join(root, 'init'));
const targetRuntimeSHA256 = lib.hashFile(path.join(root, 'payload/runtime.cpio.gz'));
fs.unlinkSync(path.join(root, 'payload/runtime.cpio.gz'));
for (const relative of ['usr/bin/reinvoke-status', 'usr/sbin/reinvoke-status'])
  fs.unlinkSync(path.join(root, relative));
fs.rmSync(path.join(root, 'etc/nand-pilot'), { recursive: true });
for (const [source, relative, mode] of [
  [path.join(__dirname, 'bsl-init.sh'), 'init', '0755'],
  [path.join(__dirname, 'common.sh'), 'usr/libexec/nand-pilot/common.sh', '0644'],
  [path.join(rc12, 'sbin/ueventd'), 'sbin/ueventd', '0755'],
  [path.join(rc12, 'ueventd.rc'), 'ueventd.rc', '0644'],
  [path.join(rc12, 'lib/libglibc_bridge.so'), 'lib/libglibc_bridge.so', '0755'],
  [helper, 'sbin/set-private-loop-offset', '0755'],
]) lib.run('install', ['-m', mode, source, path.join(root, relative)]);
fs.writeFileSync(path.join(root, 'etc/reinvoke-bsl-target.sha256'),
  `${targetInitSHA256}  /nand-root/init\n${targetRuntimeSHA256}  /nand-root/payload/runtime.cpio.gz\n`,
  { mode: 0o444 });
fs.mkdirSync(path.join(root, 'nand-root'));
fs.symlinkSync('/init', path.join(root, 'linuxrc'));
fs.unlinkSync(path.join(root, 'etc/reinvoke-release'));
fs.writeFileSync(path.join(root, 'etc/reinvoke-release'),
  'reInvoke BSL forward launcher v3\nUSB diagnostics are optional; no vendor updater or NAND-writing startup.\nDiagnostic state: /run/reinvoke-bsl\n', { mode: 0o444 });
const qemu = path.join(archive, 'emulation/qemu-arm-static');
lib.run(qemu, [path.join(root, 'bin/busybox'), 'sh', '-n', path.join(root, 'init')]);
assert.equal(lib.hashFile(path.join(root, 'bin/busybox')), lib.BB_SHA256);
assert.equal(lib.hashFile(path.join(root, 'sbin/adbd-root')), lib.ADB_SHA256);
const closure = lib.elfClosure(root);
lib.json(path.join(output, 'elf-closure.json'), closure);
lib.run('chmod', ['0755', root]);
lib.run('chown', ['-hR', '0:0', root]);
lib.run('find', [root, '-exec', 'touch', '-h', '-d', '@0', '{}', '+']);
const squashfs = path.join(output, 'bsl.squashfs');
lib.run('mksquashfs', [root, squashfs, '-noappend', '-comp', 'gzip', '-b', '131072',
  '-processors', '1', '-no-progress', '-all-root', '-mkfs-time', '0']);
const image = fs.readFileSync(squashfs);
assert(image.length <= baseline.length, 'BSL exceeds the fixed 5 MiB allocation');
const payload = Buffer.alloc(baseline.length, 0xff);
image.copy(payload);
fs.writeFileSync(path.join(output, 'payload.bin'), payload, { mode: 0o600 });
fs.writeFileSync(path.join(output, 'current-bsl.bin'), baseline, { mode: 0o600 });
const prefix = fs.readFileSync(path.join(readback, 'prefix.bin'));
const tables = [];
for (let block = 1; block <= 8; block++) {
  tables.push(lib.sha(prefix.subarray((block + 1) * 131072 - 4096, (block + 1) * 131072 - 4096 + 848)));
}
assert(tables.every(hash => hash === 'e25ca94fac7c1fac5df425246b9ea147751a6fb1be252e322e547c807f737083'));
const verifyRoot = path.join(output, 'verified-root');
lib.run('unsquashfs', ['-processors', '1', '-no-progress', '-d', verifyRoot, squashfs]);
const extracted = lib.compareTrees(root, verifyRoot);
lib.json(path.join(output, 'MANIFEST.json'), {
  status: 'FORWARD_VARIATION_NOT_YET_FLASHED',
  target: { start: 0x01a20000, endExclusive: 0x01f20000, bytes: baseline.length },
  filesystem: { bytes: image.length, sha256: lib.sha(image) },
  payloadSHA256: lib.sha(payload),
  currentBSLSHA256: lib.sha(baseline),
  beforeSHA256: lib.sha(original.subarray(0, 131072)),
  afterSHA256: lib.sha(original.subarray(41 * 131072)),
  tableSHA256: tables[0],
  initSHA256: lib.hashFile(path.join(root, 'init')),
  targetRootfsSHA256: pilotProposal.image.sha256,
  targetInitSHA256,
  targetRuntimeSHA256,
  loopView: { offset: 0x02920000, bytes: loopBytes, readOnly: true,
    helperSHA256: lib.hashFile(helper), sourceSHA256: lib.hashFile(path.join(__dirname, 'bsl-loop-view.c')),
    compiler: lib.run('arm-linux-gnueabihf-gcc', ['--version']).split('\n')[0],
    execution: 'not executed on host or device; actual native loop ioctl support remains unverified' },
  extracted,
  noRestoration: true,
  intendedWritesOutsideBSL: false,
  remainsUnproven: 'Cold boot selection, kernel reachability and complete native execution',
});
console.log(`BSL image ${image.length} bytes; fixed writer extent ${payload.length} bytes`);
