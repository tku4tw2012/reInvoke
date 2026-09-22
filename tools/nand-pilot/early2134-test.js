// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT
'use strict';
const assert = require('assert');
const fs = require('fs');
const { pins, helperPin, verify, patchInit, verifyPatch, compareTrees, profile } = require('./early2134-build');

// Optional integration inputs are extracted from the pinned vendor image, not fixtures in Git.
assert.throws(() => verify(Buffer.from('wrong source'), pins.image), /size mismatch/);
assert.throws(() => verify(Buffer.alloc(pins.initRC.bytes), pins.initRC), /hash mismatch/);
assert.throws(() => patchInit(Buffer.alloc(pins.initRC.bytes)), /hash mismatch/);
const helper = fs.readFileSync(require('path').join(__dirname, 'early2134-adb.sh'));
verify(helper, helperPin);
helper[helper.length - 1] ^= 1;
assert.throws(() => verify(helper, helperPin), /hash mismatch/);
const original = process.argv[2] && fs.readFileSync(process.argv[2]);
if (original) {
  const candidate = patchInit(original);
  verifyPatch(original, candidate);
  const tampered = Buffer.from(candidate);
  tampered[tampered.length - 1] ^= 1;
  assert.throws(() => verifyPatch(original, tampered), /tampered/);
  assert.throws(() => verifyPatch(candidate, candidate), /source size|source hash/);
  const text = candidate.toString();
  const originalText = original.toString();
  assert(text.indexOf('    start adbd\n') < text.indexOf('\non fs\n'));
  assert(text.indexOf('    start adbd\n') > text.indexOf('\non init\n'));
  assert(text.indexOf('    start adbd\n') < text.indexOf('android0/enable "1"'));
  assert.strictEqual((text.match(/^\s+start adbd$/gm) || []).length, 1);
  assert.strictEqual((text.match(/android0\/iProduct/g) || []).length, 1);
  assert.strictEqual((text.match(/android0\/enable/g) || []).length, 1);
  const phases = [...text.matchAll(/write (\/run\/early2134-phase-\d+)/g)].map(m => m[1]);
  assert.strictEqual(phases.length, 8);
  assert.strictEqual(new Set(phases).size, 8, 'vendor write does not truncate; never reuse phase paths');
  assert.strictEqual((text.match(/^    disabled$/gm) || []).length,
    (originalText.match(/^    disabled$/gm) || []).length - 1);
  assert.strictEqual(text.slice(text.indexOf('service boot_complete')),
    originalText.slice(originalText.indexOf('service boot_complete')));
  assert(!text.includes('PS1'));
}
const source = [{ name: './init.rc', sha256: 'before', mode: '644' },
  { name: './data', type: 'l', link: '/lsync/data1' }];
const candidate = [{ ...source[0], sha256: 'after' }, source[1],
  { name: './sbin/early2134-adb.sh', type: 'f', mode: '755', uid: '0', gid: '0' }];
compareTrees(source, candidate);
assert.throws(() => compareTrees(source, [{ ...candidate[0], mode: '777' }, ...candidate.slice(1)]));
assert.throws(() => compareTrees(source, [candidate[0],
  { ...candidate[1], link: '/run/data' }, candidate[2]]));
assert.throws(() => compareTrees(source, [...candidate, { name: './unexpected' }]));
if (process.argv[3]) {
  const image = fs.readFileSync(process.argv[3]);
  profile(image, true);
  image[image.length - 1] ^= 1;
  assert.throws(() => profile(image, true), /hash mismatch/);
}
console.log('PASS early2134 source pins, exact patch rejection, early startup ordering and metadata/data policy checks');
