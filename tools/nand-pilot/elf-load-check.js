// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
'use strict';
const assert = require('assert');
const crypto = require('crypto');

function readProgramHeaders(bytes) {
  assert(bytes.length >= 52, 'truncated ELF header');
  assert.equal(bytes.subarray(0, 4).toString('hex'), '7f454c46', 'not ELF');
  assert.equal(bytes[4], 1, 'require ELF32');
  assert.equal(bytes[5], 1, 'require little-endian ELF');
  assert.equal(bytes.readUInt16LE(18), 40, 'require ARM');
  assert.equal(bytes.readUInt16LE(40), 52, 'unexpected ELF header size');
  assert.equal(bytes.readUInt16LE(42), 32, 'unexpected program-header size');
  const offset = bytes.readUInt32LE(28), count = bytes.readUInt16LE(44);
  assert(count > 0 && count < 256 && offset >= 52 &&
    offset + count * 32 <= bytes.length, 'invalid program-header table');
  return Array.from({ length: count }, (_, index) => {
    const header = bytes.subarray(offset + index * 32, offset + (index + 1) * 32);
    const start = header.readUInt32LE(4), size = header.readUInt32LE(16);
    assert(start + size <= bytes.length, 'truncated program segment');
    return { header, type: header.readUInt32LE(0), start, size };
  });
}

function normalizeSectionBookkeeping(bytes, offset) {
  const copy = Buffer.from(bytes);
  // strip rewrites section-table location/count fields, not program headers.
  for (const [start, end] of [[32, 36], [46, 52]]) {
    for (let position = Math.max(start, offset);
      position < Math.min(end, offset + copy.length); position++) {
      copy[position - offset] = 0;
    }
  }
  return copy;
}

function verifyLoadEquivalent(before, after) {
  const original = readProgramHeaders(before), stripped = readProgramHeaders(after);
  assert(normalizeSectionBookkeeping(before.subarray(0, 52), 0)
    .equals(normalizeSectionBookkeeping(after.subarray(0, 52), 0)),
  'loader-relevant ELF header changed');
  assert.equal(original.length, stripped.length, 'program-header count changed');
  const loads = [];
  original.forEach((segment, index) => {
    assert(segment.header.equals(stripped[index].header), 'program header changed');
    if (segment.type !== 1) return;
    const oldBytes = normalizeSectionBookkeeping(
      before.subarray(segment.start, segment.start + segment.size), segment.start);
    const newBytes = normalizeSectionBookkeeping(
      after.subarray(segment.start, segment.start + segment.size), segment.start);
    assert(oldBytes.equals(newBytes), 'PT_LOAD code/data changed');
    loads.push({
      offset: segment.start, bytes: segment.size,
      sha256: crypto.createHash('sha256').update(newBytes).digest('hex'),
    });
  });
  assert(loads.length > 0, 'no loadable segments');
  return { programHeaders: original.length, loads,
    excludedHeaderFields: 'e_shoff, e_shentsize, e_shnum, e_shstrndx only' };
}

module.exports = { verifyLoadEquivalent };
