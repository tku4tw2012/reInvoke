// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
'use strict';
const fs = require('fs');
const path = require('path');
const assert = require('assert');
const lib = require('./build-lib');
const out = path.resolve(process.argv[2]);
const archive = path.resolve(process.argv[3] || path.join(__dirname, '../../../reinvoke-archive'));
const proposal = JSON.parse(fs.readFileSync(path.join(out, 'PROPOSAL.json')));
const capturePath = path.join(archive, lib.pins.capture.path);
lib.verify(capturePath, lib.pins.capture);
const image = fs.readFileSync(path.join(out, 'rootfs.squashfs'));
const payload = fs.readFileSync(path.join(out, 'payload.bin'));
const rollback = fs.readFileSync(path.join(out, 'rollback.bin'));
const capture = fs.readFileSync(capturePath);
const start = 0x02920000, allocationEnd = 0x08320000, erase = 131072;
const extent = Math.ceil(image.length / erase) * erase;
assert(start + extent <= allocationEnd);
assert.strictEqual(proposal.authorization, false);
assert.strictEqual(proposal.extent.start, start);
assert.strictEqual(proposal.extent.bytes, extent);
assert.strictEqual(proposal.extent.endExclusive, start + extent);
assert.strictEqual(payload.length, extent);
assert.strictEqual(rollback.length, extent);
assert(image.equals(payload.subarray(0, image.length)));
assert(payload.subarray(image.length).every(v => v === 0xff));
assert(rollback.equals(capture.subarray(start, start + extent)));
for (const [name, data] of [['image', image], ['payload', payload], ['rollback', rollback]]) {
  assert.strictEqual(proposal[name].bytes, data.length);
  assert.strictEqual(proposal[name].sha256, lib.sha(data));
}
assert.strictEqual(proposal.image.filesystemBytesUsed, Number(image.readBigUInt64LE(40)));
assert.strictEqual(proposal.changedBlocks.length, extent / erase);
proposal.changedBlocks.forEach((block, i) => {
  assert.strictEqual(block.offset, start + i * erase);
  const before = rollback.subarray(i * erase, (i + 1) * erase);
  const after = payload.subarray(i * erase, (i + 1) * erase);
  assert.strictEqual(block.originalSha256, lib.sha(before));
  assert.strictEqual(block.candidateSha256, lib.sha(after));
  assert.strictEqual(block.changed, !before.equals(after));
});
for (const [i, offset] of [start - erase, start + extent].entries()) {
  assert.strictEqual(proposal.adjacentBlocks[i].offset, offset);
  assert.strictEqual(proposal.adjacentBlocks[i].sha256, lib.sha(capture.subarray(offset, offset + erase)));
}
for (const build of ['build-a', 'build-b']) {
  assert(image.equals(fs.readFileSync(path.join(out, build, 'rootfs.squashfs'))));
  const payloadCopy = fs.readFileSync(path.join(out, build, 'verified-bootstrap/payload/runtime.cpio.gz'));
  const conf = fs.readFileSync(path.join(out, build, 'verified-bootstrap/etc/nand-pilot/payload.conf'), 'utf8');
  assert(conf.includes(`PAYLOAD_SHA256=${lib.sha(payloadCopy)}\n`));
  assert(conf.includes(`PAYLOAD_BYTES=${payloadCopy.length}\n`));
}
console.log(`PASS independent bytes: image=${image.length} extent=${extent} rollback exact restored capture; ${extent / erase} block hashes; both neighbors; reproducible builds`);
