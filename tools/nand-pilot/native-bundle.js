// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
'use strict';

const fs = require('fs');
const path = require('path');
const assert = require('assert');
const { crc32 } = require('zlib');
const lib = require('./build-lib');

assert(process.argv.length === 6 || process.argv.length === 7,
  'usage: node native-bundle.js ARCHIVE PILOT_ARTIFACT COMPACT_BSL_ARTIFACT NEW_OUTPUT [KERNEL_81_IMAGE]');
const [archive, pilot, bsl, output] = process.argv.slice(2, 6).map(value => path.resolve(value));
// Replacing the kernel is opt-in. Every candidate through 05.8 left bootimgs
// byte-identical to the vendor payload, so a build that changes it has to say
// so explicitly rather than inherit it from a path that happens to exist.
const kernelPath = process.argv[6] ? path.resolve(process.argv[6]) : null;
// Offset of the loader-consumed image inside the bootimgs record. Measured on
// this unit: the record opens with a 128-byte descriptor and an embedded
// cmdline, and the vendor image begins one 128 KiB erase block in.
const IMAGE_OFFSET = 0x20000;
assert(!fs.existsSync(output), 'output already exists');
assert(!output.startsWith(path.resolve(__dirname, '../..') + path.sep),
  'firmware output must remain outside the repository');
const vendorPath = path.join(archive, 'extracted/ota2/OTA2/83_IMAGE');
lib.verify(vendorPath, {
  bytes: 69481562,
  sha256: 'b2e12178f98a0c0904cb1e6e2ba933de0c0fef8be7c24e7852bc9933294850e8',
});
const mainManifest = JSON.parse(fs.readFileSync(path.join(pilot, 'PROPOSAL.json'), 'utf8'));
const bslManifest = JSON.parse(fs.readFileSync(path.join(bsl, 'MANIFEST.json'), 'utf8'));
assert.equal(mainManifest.buildId, lib.BUILD_ID, 'main candidate does not match this builder');
assert.equal(bslManifest.buildId, lib.BUILD_ID, 'BSL candidate does not match this builder');
const rootfsPath = path.join(pilot, 'rootfs.squashfs');
const bslPath = path.join(bsl, 'bsl.squashfs');
lib.verify(rootfsPath, mainManifest.image);
lib.verify(bslPath, bslManifest.filesystem);
assert.equal(mainManifest.extent.start, 0x02920000);
assert.equal(bslManifest.target.start, 0x01a20000);
const bootstrapManifest = JSON.parse(fs.readFileSync(path.join(pilot, 'bootstrap-manifest.json'), 'utf8'));
const init = bootstrapManifest.find(entry => entry.path === 'init');
const runtime = bootstrapManifest.find(entry => entry.path === 'payload/runtime.cpio.gz');
assert(init && runtime, 'main bootstrap identity is missing');
const pairedHashes = lib.run('unsquashfs',
  ['-processors', '1', '-cat', bslPath, 'etc/reinvoke-bsl-target.sha256']);
assert.equal(pairedHashes,
  `${init.sha256}  /nand-root/init\n${runtime.sha256}  /nand-root/payload/runtime.cpio.gz\n`,
  'BSL does not target this exact main bootstrap/runtime');

const vendor = fs.readFileSync(vendorPath);
assert.equal(vendor.readUInt32LE(0), 0xd2ada3f1);
assert.equal(vendor.readUInt32LE(12), 2048);
assert.equal(vendor.readUInt32LE(20), 64);
assert.equal(vendor.readUInt32LE(24), 2048);
assert.equal(vendor.readUInt32LE(28), 9);
const names = ['block0', 'pre-bootloader', 'post-bootloader', 'tz_en',
  'bootimgs', 'bootimgs_B', 'rootfs', 'app', 'bsl'];
const replacements = new Map([
  ['rootfs', fs.readFileSync(rootfsPath)],
  ['bsl', fs.readFileSync(bslPath)],
]);
if (kernelPath) {
  // The bootimgs record is not a bare image. Candidate 06.0 wrote a plain
  // uImage at offset 0 and did not boot, because that offset holds a 128-byte
  // descriptor (load address, size) followed by an embedded cmdline; the image
  // the loader actually consumes begins one erase block in, at 0x20000.
  //
  // So splice rather than replace: keep the vendor descriptor and cmdline
  // verbatim, and swap only the image that follows. The descriptor is left
  // untouched on purpose. Its size field is larger than our kernel, which
  // makes the loader read past the uImage into zero padding, and a uImage
  // carries its own length, so that is harmless. Changing a field whose
  // semantics are unverified is the larger risk.
  //
  // bootimgs_B keeps the vendor image so a failed boot has somewhere to fall
  // back to. 06.0 overwrote both slots and left no working kernel at all.
  const kernel = fs.readFileSync(kernelPath);
  const index = names.indexOf('bootimgs');
  let start = 640;
  for (let before = 0; before < index; before++) {
    start += Number(vendor.readBigUInt64LE(64 + before * 64 + 16));
  }
  const size = Number(vendor.readBigUInt64LE(64 + index * 64 + 16));
  assert(IMAGE_OFFSET + kernel.length <= size,
    'kernel does not fit after the vendor descriptor');
  const spliced = Buffer.alloc(size);
  vendor.copy(spliced, 0, start, start + IMAGE_OFFSET);
  kernel.copy(spliced, IMAGE_OFFSET);
  replacements.set('bootimgs', spliced);
}
const table = Buffer.from(vendor.subarray(0, 640));
const payloads = [], records = [];
let sourceOffset = 640, outputOffset = 640;
for (let index = 0; index < names.length; index++) {
  const at = 64 + index * 64;
  const descriptor = vendor.subarray(at, at + 64);
  const nameEnd = descriptor.indexOf(0);
  assert(nameEnd >= 0 && nameEnd < 16, 'invalid descriptor name');
  const name = descriptor.subarray(0, nameEnd).toString('ascii');
  assert.equal(name, names[index]);
  const size = Number(descriptor.readBigUInt64LE(16));
  assert(Number.isSafeInteger(size) && size > 0 && sourceOffset + size <= vendor.length);
  const original = vendor.subarray(sourceOffset, sourceOffset + size);
  assert.equal(crc32(original), descriptor.readUInt32LE(24), `source CRC mismatch: ${name}`);
  const payload = replacements.get(name) || original;
  const capacity = descriptor.readUInt32LE(44) * 131072;
  if (descriptor.readUInt32LE(48) === 0) assert(payload.length <= capacity, `${name} exceeds allocation`);
  table.writeBigUInt64LE(BigInt(payload.length), at + 16);
  table.writeUInt32LE(crc32(payload), at + 24);
  records.push({
    name, start: descriptor.readUInt32LE(40) * 131072, capacity,
    dataType: descriptor.readUInt32LE(48), bytes: payload.length,
    sourceOffset, outputOffset, sha256: lib.sha(payload),
    vendorPayloadUnchanged: payload.equals(original),
  });
  payloads.push(payload);
  sourceOffset += size;
  outputOffset += payload.length;
}
const changedRecords = kernelPath
  ? ['bootimgs', 'rootfs', 'bsl']
  : ['rootfs', 'bsl'];
assert.deepEqual(records.filter(record => !record.vendorPayloadUnchanged).map(record => record.name),
  changedRecords, 'only owned records may differ');
assert.equal(records.find(record => record.name === 'app').dataType, 1);
const image = Buffer.concat([table, ...payloads]);
assert.equal(image.length, outputOffset);
fs.mkdirSync(output, { mode: 0o700 });
const imagePath = path.join(output, lib.BUNDLE_NAME);
fs.writeFileSync(imagePath, image, { flag: 'wx', mode: 0o600 });
const length = Buffer.alloc(4);
length.writeUInt32LE(image.length);
fs.writeFileSync(path.join(output, '07_IMAGE.for-83'), length, { flag: 'wx', mode: 0o600 });
lib.json(path.join(output, 'MANIFEST.json'), {
  buildId: lib.BUILD_ID,
  status: 'OFFLINE_CANDIDATE_NOT_NATIVE_BOOT_VERIFIED',
  source: { path: vendorPath, sha256: lib.sha(vendor) },
  image: { file: path.basename(imagePath), bytes: image.length, sha256: lib.sha(image) },
  records, excludedTrailerBytes: vendor.length - sourceOffset,
  ownedMain: { artifact: pilot, rootfsSHA256: mainManifest.image.sha256,
    initSHA256: init.sha256, runtimeSHA256: runtime.sha256 },
  ownedBSL: { artifact: bsl, rootfsSHA256: bslManifest.filesystem.sha256 },
  kernelAndNativeBootPayloads: 'byte-identical to vendor 12.2134.0; no encryption-flag or kernel-envelope changes',
  app: 'complete vendor-default OOB-bearing seed retained; not the unit-specific live app filesystem',
  installation: 'Designed for the vendor unified-image programmer, not a sparse Linux write plan',
  installationRequiresSeparateApproval: true,
  destructiveScope: 'The observed vendor programmer erases all good NAND blocks, including existing app/factory/fw_stat/tail state, before installing listed records',
  omittedState: 'Unlisted boot status is left erased by that procedure; omission does not protect a partition',
  forbidden: ['image99', 'blind raw block0 programming from Linux', 'automatic retry', 'automatic reboot'],
  remainingUnknowns: 'Actual native selection/execution and the exact Harman boot-state policy remain unproved',
});
console.log(JSON.stringify({ image: path.basename(imagePath), bytes: image.length,
  sha256: lib.sha(image), changedRecords, records: records.length,
  flashAuthorizedByBuilder: false }, null, 2));
