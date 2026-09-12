// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
'use strict';
const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const cp = require('child_process');

const pins = {
  rc12: { path: 'build/artifacts/pre-nand-rc12-20260908/82_IMAGE', bytes: 34557374,
    sha256: 'a0f273ddfb4a7a3844078b88f4ff826a1f1d1c7ae8c01b77b87533e3bf8985de' },
  stock: { path: 'hardware/dumps/20260902T215700Z-native-ram/installed-rootfs.squashfs', bytes: 48831891,
    sha256: '717041d874bba6a16cda6578101ab1b7e1ff7737ade1b0921b77c9e4e65f6170' },
  capture: { path: 'evidence/nand-restored-ram-inspection-20260909/restored-main-256MiB.bin', bytes: 268435456,
    sha256: '2fac4159fe23aa25581c29f6c90033af3a1126a02593db0bd47e2c10d2c09f19' },
};
const BUILD_ID = 'reInvoke-NAND-pilot-02-20260911';
const BB_SHA256 = '5fc83ab6cd37841b8d73e07bf3cd8af47ae5af56c93fe085b2db91e0d1f4207b';
const ADB_SHA256 = '62593dfe9580443dca064e28c38cb647f1b719fe275666d5fb4b615781864ed6';
const sha = data => crypto.createHash('sha256').update(data).digest('hex');
const hashFile = file => sha(fs.readFileSync(file));
const run = (cmd, args, options = {}) => cp.execFileSync(cmd, args, {
  encoding: 'utf8', maxBuffer: 32 * 1024 * 1024, ...options,
});
const json = (file, value) => fs.writeFileSync(file, JSON.stringify(value, null, 2) + '\n');
function verify(file, pin) {
  if (fs.statSync(file).size !== pin.bytes || hashFile(file) !== pin.sha256)
    throw new Error(`input size/hash mismatch: ${file}`);
}
function rooted(root, name) {
  let components = name.split('/').filter(Boolean);
  let done = [], links = 0;
  while (components.length) {
    const next = components.shift();
    if (next === '.') continue;
    if (next === '..') { done.pop(); continue; }
    const file = path.join(root, ...done, next);
    if (fs.lstatSync(file).isSymbolicLink()) {
      if (++links > 40) throw new Error(`symlink loop: ${name}`);
      const target = fs.readlinkSync(file);
      if (target.startsWith('/')) done = [];
      components = [...target.split('/').filter(Boolean), ...components];
    } else done.push(next);
  }
  return path.join(root, ...done);
}
function inventory(root) {
  // Node's stat uses statx and bypasses fakeroot. GNU find/stat are authoritative
  // for ownership and devices, including after unsquashfs verification.
  const rows = run('find', [root, '-printf', '%P\t%y\t%m\t%U\t%G\t%l\t%n\t%i\n'])
    .trimEnd().split('\n').map(line => line.split('\t')).sort((a, b) => a[0].localeCompare(b[0], 'en'));
  const links = new Map();
  return rows.map(([name, type, mode, uid, gid, target, nlink, ino]) => {
    const item = { path: name, type, mode, uid: +uid, gid: +gid };
    const file = path.join(root, name);
    if (type === 'f') {
      item.bytes = fs.statSync(file).size;
      item.sha256 = hashFile(file);
      if (+nlink > 1) {
        if (!links.has(ino)) links.set(ino, name);
        item.hardlinkGroup = links.get(ino);
      }
    }
    if (type === 'l') item.target = target;
    if (type === 'c' || type === 'b') item.device = run('stat', ['-c', '%t:%T', file]).trim();
    return item;
  });
}
function compareTrees(a, b) {
  const x = inventory(a), y = inventory(b);
  if (JSON.stringify(x) !== JSON.stringify(y)) {
    const differences = x.filter((v, i) => JSON.stringify(v) !== JSON.stringify(y[i]));
    throw new Error(`extraction metadata/content mismatch: ${JSON.stringify(differences.slice(0, 5))}`);
  }
  return { entries: x.length, regularFiles: x.filter(v => v.type === 'f').length,
    devices: x.filter(v => v.type === 'c' || v.type === 'b'),
    manifestSha256: sha(JSON.stringify(x)) };
}
function elfClosure(root) {
  const report = [];
  for (const item of inventory(root).filter(v => v.type === 'f')) {
    const file = path.join(root, item.path);
    const fd = fs.openSync(file, 'r'), header = Buffer.alloc(20);
    fs.readSync(fd, header, 0, 20, 0); fs.closeSync(fd);
    if (header.subarray(0, 4).toString('hex') !== '7f454c46' || header.readUInt16LE(16) === 1) continue;
    if (header[4] !== 1 || header[5] !== 1 || header.readUInt16LE(18) !== 40)
      throw new Error(`non ARM32 little-endian ELF in candidate: ${item.path}`);
    const info = run('readelf', ['-l', '-d', file]);
    const interpreter = /\[Requesting program interpreter: ([^\]]+)\]/.exec(info)?.[1];
    if (interpreter) rooted(root, interpreter);
    const modern = item.path.startsWith('opt/reinvoke/');
    const dirs = modern ? ['/opt/reinvoke/lib/hostapd', '/opt/reinvoke/lib', '/lib', '/usr/lib'] : ['/lib', '/usr/lib'];
    const needed = [...info.matchAll(/\(NEEDED\).*Shared library: \[([^\]]+)\]/g)].map(m => m[1]);
    const dependencies = needed.map(name => {
      for (const dir of dirs) {
        try {
          const found = rooted(root, `${dir}/${name}`);
          if (fs.statSync(found).isFile()) return { name, resolved: path.relative(root, found) };
        } catch (_) { /* Try the next documented loader directory. */ }
      }
      throw new Error(`missing ELF dependency ${name} for ${item.path}`);
    });
    report.push({ path: item.path, interpreter, loaderSearch: dirs, dependencies });
  }
  return report;
}
function proposal(image, capture, out) {
  const allocationStart = 0x02920000, allocationEnd = 0x08320000, erase = 131072;
  if (image.readUInt32LE(0) !== 0x73717368 || image.length < 96) throw new Error('invalid SquashFS');
  const actual = Number(image.readBigUInt64LE(40));
  if (actual > image.length || actual < 96) throw new Error('truncated SquashFS bytes_used');
  const extent = Math.ceil(image.length / erase) * erase, end = allocationStart + extent;
  if (end > allocationEnd) throw new Error('candidate exceeds rootfs allocation');
  const payload = Buffer.alloc(extent, 0xff); image.copy(payload);
  const rollback = Buffer.from(capture.subarray(allocationStart, end));
  if (rollback.length !== extent) throw new Error('truncated rollback capture');
  fs.writeFileSync(path.join(out, 'payload.bin'), payload);
  fs.writeFileSync(path.join(out, 'rollback.bin'), rollback);
  const blocks = [];
  for (let i = 0; i < extent; i += erase) {
    const before = rollback.subarray(i, i + erase), after = payload.subarray(i, i + erase);
    blocks.push({ offset: allocationStart + i, bytes: erase, changed: !before.equals(after),
      originalSha256: sha(before), candidateSha256: sha(after) });
  }
  return {
    schema: 1, buildId: BUILD_ID, operation: 'OFFLINE_PROPOSAL_ONLY',
    authorization: false, implicitAuthorization: false, bootAcceptance: 'unproven; owner normal power cycle required',
    kernelPolicy: 'no kernel flash; stock NAND entry/support unproven; FF boot selection is not fixed by assertion',
    allocation: { name: 'rootfs', start: allocationStart, endExclusive: allocationEnd, bytes: allocationEnd - allocationStart },
    geometry: { eraseBytes: erase, pageBytes: 2048 },
    image: { file: 'rootfs.squashfs', bytes: image.length, filesystemBytesUsed: actual, sha256: sha(image) },
    extent: { start: allocationStart, endExclusive: end, bytes: extent, blocks: extent / erase },
    payload: { file: 'payload.bin', bytes: extent, sha256: sha(payload), padding: '0xff after complete image' },
    rollback: { file: 'rollback.bin', bytes: extent, sha256: sha(rollback), captureSha256: sha(capture) },
    changedBlocks: blocks,
    adjacentBlocks: [allocationStart - erase, end].map(offset =>
      ({ offset, bytes: erase, sha256: sha(capture.subarray(offset, offset + erase)) })),
    untouched: { prefix: { start: 0, endExclusive: allocationStart },
      suffix: { start: end, endExclusive: capture.length }, noErasureToOldRootfsEnd: true },
    sourcePins: pins,
  };
}
module.exports = { pins, BUILD_ID, BB_SHA256, ADB_SHA256, sha, hashFile, run, json, verify,
  rooted, inventory, compareTrees, elfClosure, proposal };
