// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
'use strict';
const fs = require('fs');
const path = require('path');
const assert = require('assert');
const lib = require('./build-lib');
const { verifyLoadEquivalent } = require('./elf-load-check');

assert(process.argv.length === 4 || process.argv.length === 5,
  'usage: fakeroot node compact-bsl.js ARCHIVE NEW_OUTPUT [BSL_ARTIFACT]');
const archive = path.resolve(process.argv[2]), output = path.resolve(process.argv[3]);
assert(!fs.existsSync(output), 'output already exists');
const erase = 131072;
let input, baseline, readback, previousFilesystemBytes, expectedInitHash;
if (process.argv[4]) {
  const artifact = path.resolve(process.argv[4]);
  const manifest = JSON.parse(fs.readFileSync(path.join(artifact, 'MANIFEST.json'), 'utf8'));
  input = path.join(artifact, 'bsl.squashfs');
  lib.verify(input, manifest.filesystem);
  baseline = fs.readFileSync(path.join(artifact, 'payload.bin'));
  assert.equal(baseline.length, 40 * erase);
  assert.equal(lib.sha(baseline), manifest.payloadSHA256);
  previousFilesystemBytes = manifest.filesystem.bytes;
  expectedInitHash = manifest.initSHA256;
} else {
  input = path.join(archive, 'evidence/nand-bsl-v2-write-20260911/bsl-with-neighbors-after.bin');
  readback = fs.readFileSync(input);
  assert.equal(readback.length, 42 * erase);
  baseline = readback.subarray(erase, 41 * erase);
  assert.equal(lib.sha(baseline), '91a9dde15fea2959232fc9a0cdafdb739e08a7498c4e8b57d867889c8f358163');
  previousFilesystemBytes = 5111808;
  expectedInitHash = '75c9b3d1e4da39b7ccfeb3e23866de9e510b210fef62027a84f265a75772ac8c';
}
fs.mkdirSync(output, { mode: 0o700 });
const root = path.join(output, 'root');
lib.run('unsquashfs', [...(readback ? ['-o', String(erase)] : []),
  '-processors', '1', '-no-progress', '-d', root, input]);
const originalInventory = lib.inventory(root), strippedLibraries = [];
for (const item of originalInventory.filter(entry => entry.type === 'f' && entry.path.startsWith('lib/'))) {
  const file = path.join(root, item.path);
  const before = fs.readFileSync(file);
  if (before.subarray(0, 4).toString('hex') !== '7f454c46') continue;
  if (!/\.debug_[a-z_]+/.test(lib.run('readelf', ['-S', file]))) continue;
  lib.run('arm-linux-gnueabihf-strip', ['--strip-debug', file]);
  const after = fs.readFileSync(file);
  const loadCheck = verifyLoadEquivalent(before, after);
  assert(after.length < before.length, 'debug removal did not reduce the library');
  strippedLibraries.push({ path: item.path, beforeBytes: before.length, afterBytes: after.length,
    beforeSHA256: lib.sha(before), afterSHA256: lib.sha(after), loadCheck });
}
assert(strippedLibraries.length > 0, 'no debug data removed');
const compactInventory = lib.inventory(root);
assert.equal(compactInventory.length, originalInventory.length);
const changedPaths = [];
for (let index = 0; index < originalInventory.length; index++) {
  const before = originalInventory[index], after = compactInventory[index];
  if (JSON.stringify(before) === JSON.stringify(after)) continue;
  const { bytes: beforeBytes, sha256: beforeSHA256, ...beforeMetadata } = before;
  const { bytes: afterBytes, sha256: afterSHA256, ...afterMetadata } = after;
  assert.deepStrictEqual(afterMetadata, beforeMetadata, 'file metadata changed');
  assert(afterBytes < beforeBytes && afterSHA256 !== beforeSHA256);
  changedPaths.push(before.path);
}
assert.deepStrictEqual(changedPaths, strippedLibraries.map(item => item.path));
assert.equal(lib.hashFile(path.join(root, 'init')), expectedInitHash);
assert.equal(lib.hashFile(path.join(root, 'bin/busybox')), lib.BB_SHA256);
assert.equal(lib.hashFile(path.join(root, 'sbin/adbd-root')), lib.ADB_SHA256);
lib.json(path.join(output, 'elf-load-equivalence.json'), strippedLibraries);
lib.json(path.join(output, 'elf-closure.json'), lib.elfClosure(root));
lib.run('chmod', ['0755', root]);
lib.run('chown', ['-hR', '0:0', root]);
lib.run('find', [root, '-exec', 'touch', '-h', '-d', '@0', '{}', '+']);
const squashfs = path.join(output, 'bsl.squashfs');
lib.run('mksquashfs', [root, squashfs, '-noappend', '-comp', 'gzip', '-b', '131072',
  '-processors', '1', '-no-progress', '-all-root', '-mkfs-time', '0']);
const image = fs.readFileSync(squashfs);
assert(image.length <= 2715648, 'compact image still exceeds the original vendor BSL payload');
const payload = Buffer.alloc(baseline.length, 0xff);
image.copy(payload);
fs.writeFileSync(path.join(output, 'payload.bin'), payload, { mode: 0o600 });
fs.writeFileSync(path.join(output, 'current-bsl.bin'), baseline, { mode: 0o600 });
const verified = path.join(output, 'verified-root');
lib.run('unsquashfs', ['-processors', '1', '-no-progress', '-d', verified, squashfs]);
const extracted = lib.compareTrees(root, verified);
lib.json(path.join(output, 'MANIFEST.json'), {
  status: 'COMPACT_FORWARD_VARIATION_NOT_YET_FLASHED',
  target: { start: 0x01a20000, endExclusive: 0x01f20000, bytes: baseline.length },
  filesystem: { bytes: image.length, sha256: lib.sha(image) },
  previousFilesystemBytes, originalVendorFilesystemBytes: 2715648,
  payloadSHA256: lib.sha(payload), currentBSLSHA256: lib.sha(baseline),
  ...(readback ? {
    beforeSHA256: lib.sha(readback.subarray(0, erase)),
    afterSHA256: lib.sha(readback.subarray(41 * erase)),
    tableSHA256: 'e25ca94fac7c1fac5df425246b9ea147751a6fb1be252e322e547c807f737083',
  } : { sourceArtifact: path.resolve(process.argv[4]),
    sourceEvidence: 'offline BSL build, not a live NAND snapshot' }),
  initSHA256: lib.hashFile(path.join(root, 'init')), strippedLibraries, extracted,
  noRestoration: true, intendedWritesOutsideBSL: false,
  change: 'Remove non-runtime ELF debug sections only; retain startup, program headers and PT_LOAD code/data',
  remainsUnproven: 'Normal boot selection, any bootloader size constraint and native cold execution',
});
console.log(`Compact BSL: ${image.length} bytes, previously ${previousFilesystemBytes}; original vendor payload 2715648.`);
