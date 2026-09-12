#!/usr/bin/env node
// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT

import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { crc32 } from 'node:zlib';
import { setTimeout as sleep } from 'node:timers/promises';
import { fileURLToPath } from 'node:url';
import assert from 'node:assert/strict';

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
export const ACK = 'ERASE-AND-FLASH-NATIVE';
const vendorSHA = 'b2e12178f98a0c0904cb1e6e2ba933de0c0fef8be7c24e7852bc9933294850e8';
export const layout = [
  ['block0', 0, 1, 0, '1120644b58c7f00b4f0282a21da1e8cc7abd964d27fef866ebe45ade6f624a5a'],
  ['pre-bootloader', 1, 8, 0, '65a6fb6e4dde1cce287974a29e45f2c0dd84fcba9b587462c17da0561a95cca2'],
  ['post-bootloader', 9, 16, 0, '1d7d1b88c6fe7e831696f400f57b1907d8ba22029aa21e0ca3d0d70f862690ad'],
  ['tz_en', 81, 40, 0, 'dcedec1872687e3b0ec641cb4a7a7cc6fb027fbcbadc247114b140ca421f9105'],
  ['bootimgs', 249, 80, 0, '72d6900d50886e01a3351713fe03a806a3cb0ace73a2dd3c6839ccc8207d0d6c'],
  ['bootimgs_B', 129, 80, 0, '72d6900d50886e01a3351713fe03a806a3cb0ace73a2dd3c6839ccc8207d0d6c'],
  ['rootfs', 329, 720, 0, null],
  ['app', 1049, 984, 1, '5133a72056bedb362d785ca2a9c7cf19e542c7414b27999ab4099009dcecc4d6'],
  ['bsl', 209, 40, 0, null],
];
const recoveryPins = {
  'bcm_erom.bin.usb': 'cae85746505ac8b9c1453e9007a7b9bea5c5be422e4f7129284f34d9d8e4e531',
  '09_IMAGE': 'c666463722d52207a570fc8e2446a686c064609edb3cf6de2506536938296e24',
  'sysinit.img': '687c70659c274be2773202e787a50d5aeb874557ea8eabda5e416788375633e4',
  'bootloader.img': 'd8b917517ff7d00e73cd55c8c4858eba9a2838877632bd33ab9593fd528bf86f',
  'drm_erom.img': '36101ba1ebc913ca2da4a2c025405177dce765a3793c71342ba6ed1b0b9f3c50',
};
export const sha = data => crypto.createHash('sha256').update(data).digest('hex');
const clean = text => text.replace(/\0/g, '').replace(/\r/g, '\n');
function exists(file) {
  try { fs.lstatSync(file); return true; }
  catch (error) { if (error.code === 'ENOENT') return false; throw error; }
}
function regular(file, limit = 160 * 1024 * 1024) {
  const fd = fs.openSync(file, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
  try {
    const stat = fs.fstatSync(fd);
    assert(stat.isFile() && stat.size <= limit, `invalid or oversized regular input: ${file}`);
    const bytes = fs.readFileSync(fd);
    assert.equal(bytes.length, stat.size, 'input changed during read');
    return bytes;
  } finally { fs.closeSync(fd); }
}
function save(file, value) {
  const fd = fs.openSync(file, 'wx', 0o600);
  try {
    fs.writeFileSync(fd, Buffer.isBuffer(value) || typeof value === 'string'
      ? value : JSON.stringify(value, null, 2) + '\n');
    fs.fsyncSync(fd);
  } finally { fs.closeSync(fd); }
  const dir = fs.openSync(path.dirname(file), 'r');
  try { fs.fsyncSync(dir); } finally { fs.closeSync(dir); }
}
function outsideRepo(directory) {
  const relative = path.relative(repo, directory);
  assert(relative && (relative === '..' || relative.startsWith('../')),
    'firmware and evidence must remain outside the repository');
}

// The known native boot payloads stay pinned; only rootfs and BSL are candidates.
export function checkBundle(manifest, image, expectedSHA, expectedLayout = layout) {
  assert(/^[a-f0-9]{64}$/.test(expectedSHA), 'expected image SHA-256 required');
  assert.equal(sha(image), expectedSHA, 'image hash mismatch');
  assert.equal(manifest.image.sha256, expectedSHA);
  assert.equal(manifest.image.bytes, image.length);
  assert.equal(manifest.source.sha256, vendorSHA, 'unsupported native vendor source');
  assert.equal(image.readUInt32LE(0), 0xd2ada3f1);
  assert.equal(image.readUInt32LE(12), 2048);
  assert.equal(image.readUInt32LE(16), 0);
  assert.equal(image.readUInt32LE(20), 64);
  assert.equal(image.readUInt32LE(24), 2048);
  assert.equal(image.readUInt32LE(28), 9);
  assert.equal(image.subarray(32, 35).toString('hex'), '234130', 'unsupported DDR/CPU platform');
  assert(image.subarray(35, 64).every(byte => byte === 0));
  assert.equal(manifest.records.length, 9);
  const records = [];
  let offset = 640;
  for (let index = 0; index < 9; index++) {
    const [name, start, blocks, type, pin] = expectedLayout[index];
    const header = image.subarray(64 + index * 64, 128 + index * 64);
    assert.equal(header.length, 64);
    const nameBytes = header.subarray(0, 16);
    const end = nameBytes.indexOf(0);
    assert(end > 0 && nameBytes.subarray(end).every(byte => byte === 0));
    assert.equal(nameBytes.subarray(0, end).toString('ascii'), name);
    const bytes = Number(header.readBigUInt64LE(16));
    assert(Number.isSafeInteger(bytes) && bytes > 0 && offset + bytes <= image.length);
    assert.equal(header.readUInt32LE(40), start, `${name} start`);
    assert.equal(header.readUInt32LE(44), blocks, `${name} allocation`);
    assert.equal(header.readUInt32LE(48), type, `${name} data type`);
    assert.equal(header.readUInt32LE(36), 0, `${name} reserved blocks`);
    assert.equal(header.readUInt32LE(52), 0, `${name} partition type`);
    assert(header.subarray(56).every(byte => byte === 0));
    if (type === 1) assert.equal(bytes % 2080, 0, 'app OOB page stride');
    const dataBytes = type === 1 ? bytes / 2080 * 2048 : bytes;
    assert(dataBytes <= blocks * 131072);
    if (name === 'pre-bootloader') assert.equal(bytes, 131072);
    const payload = image.subarray(offset, offset + bytes);
    assert.equal(crc32(payload), header.readUInt32LE(24), `${name} CRC mismatch`);
    const hash = sha(payload);
    if (pin) assert.equal(hash, pin, `native vendor payload changed: ${name}`);
    if (!pin) {
      assert.equal(payload.readUInt32LE(0), 0x73717368, `${name} is not SquashFS`);
      const used = Number(payload.readBigUInt64LE(40));
      assert(used >= 96 && used <= bytes, `${name} filesystem exceeds its payload`);
    }
    const advertised = manifest.records[index];
    for (const [key, value] of Object.entries({
      name, start: start * 131072, capacity: blocks * 131072,
      dataType: type, bytes, outputOffset: offset, sha256: hash,
    })) assert.equal(advertised[key], value, `${name} manifest ${key}`);
    records.push({ name, start: start * 131072, blocks, bytes, dataType: type,
      crc: header.readUInt32LE(24), writtenBlocks: name === 'pre-bootloader' ? 8 : Math.ceil(dataBytes / 131072) });
    offset += bytes;
  }
  assert.equal(offset, image.length, 'unexpected trailing data');
  return records;
}

export function inspect(directory, expectedSHA) {
  const manifest = JSON.parse(regular(path.join(directory, 'MANIFEST.json'), 1024 * 1024));
  const file = manifest.image?.file;
  assert(typeof file === 'string' && /^83_IMAGE[.\w-]*$/.test(file) && !file.includes('99'),
    'manifest must name a local 83 image, not image99 or an arbitrary path');
  const image = regular(path.join(directory, file));
  const hash = expectedSHA || manifest.image.sha256;
  const records = checkBundle(manifest, image, hash);
  const length = regular(path.join(directory, '07_IMAGE.for-83'), 4);
  assert.equal(length.length, 4);
  assert.equal(length.readUInt32LE(0), image.length, '07_IMAGE length mismatch');
  return { image, length, records, hash, file };
}

export function verifyFlash(consoleText, helperText, bundle) {
  const text = clean(consoleText);
  assert(!/\b(?:error|failed|failure|uncorrectable|mismatch)\b/i.test(text), 'vendor reported a failure');
  assert(text.includes('Congratulations! u2nand succeed!'), 'vendor success marker missing');
  assert(text.includes(`image size ${bundle.image.length}`), 'reported transfer length mismatch');
  assert(text.includes('2046 blocks erased.'), 'unexpected erased-block count');
  const bad = [...text.matchAll(/Bad block found @ 0x([0-9a-f]+)/gi)].map(match => parseInt(match[1], 16));
  assert.deepEqual(bad, [0x0c000000, 0x0c020000], 'bad-block inventory changed');
  assert(helperText.includes(`Image transfer complete: 83_IMAGE (${bundle.image.length} bytes sent)`),
    'complete image transport not observed');
  const burns = [...text.matchAll(/^burn ([^,]+), image size (\d+), partition start:(\d+), size (\d+), reserve (\d+), data type: (\w+), crc 0x([0-9a-f]+)/gm)];
  assert.equal(burns.length, 9, 'missing or repeated program record');
  return bundle.records.map((record, index) => {
    const burn = burns[index];
    assert.equal(burn[1], record.name);
    assert.equal(Number(burn[2]), record.bytes);
    assert.equal(Number(burn[3]), record.start / 131072);
    assert.equal(Number(burn[4]), record.blocks);
    assert.equal(Number(burn[5]), 0);
    assert.equal(burn[6], record.dataType === 1 ? 'oob' : 'normal');
    assert.equal(parseInt(burn[7], 16), record.crc);
    const section = text.slice(burn.index, burns[index + 1]?.index);
    const addresses = verb => [...new Set([...section.matchAll(
      new RegExp(`${verb} NAND at address 0x([0-9a-f]+)`, 'gi'))].map(match => parseInt(match[1], 16)))];
    const expected = Array.from({ length: record.writtenBlocks }, (_, block) => record.start + block * 131072);
    assert.deepEqual(addresses('writing'), expected, `${record.name} program coverage incomplete`);
    assert.deepEqual(addresses('reading'), expected, `${record.name} readback coverage incomplete`);
    return { name: record.name, blocks: expected.length, programAndReadObserved: true };
  });
}

function processIdentity(pid) {
  assert(Number.isInteger(pid) && pid > 1);
  const stat = fs.readFileSync(`/proc/${pid}/stat`, 'utf8');
  return {
    pid, start: stat.slice(stat.lastIndexOf(')') + 2).split(' ')[19],
    args: fs.readFileSync(`/proc/${pid}/cmdline`).toString().split('\0').filter(Boolean),
  };
}
function sameProcess(identity) {
  try {
    const current = processIdentity(identity.pid);
    return current.start === identity.start && JSON.stringify(current.args) === JSON.stringify(identity.args);
  } catch (error) {
    if (error.code === 'ENOENT' || error.code === 'ESRCH') return false;
    throw error;
  }
}
export function verifyUsbTarget(root, usbPath, descriptors) {
  const matches = [];
  for (const name of fs.readdirSync(root)) {
    if (!/^\d+-\d+(?:\.\d+)*$/.test(name)) continue;
    try {
      const read = attribute => fs.readFileSync(path.join(root, name, attribute), 'utf8').trim();
      if (read('idVendor') === '1286' && read('idProduct') === '8174') {
        matches.push({ name, bus: read('busnum'), number: read('devnum') });
      }
    } catch (error) { if (error.code !== 'ENOENT') throw error; }
  }
  assert.equal(matches.length, 1, 'exactly one recovery device must be attached');
  const device = matches[0];
  assert.equal(device.name, usbPath, 'the sole recovery device is on a different physical path');
  assert(/^\d+$/.test(device.bus) && /^\d+$/.test(device.number));
  const node = `/dev/bus/usb/${device.bus.padStart(3, '0')}/${device.number.padStart(3, '0')}`;
  assert(descriptors.includes(node), 'helper does not hold the selected USB device');
  return device.number;
}
function session(options) {
  const firmware = options.firmware;
  for (const [name, pin] of Object.entries(recoveryPins)) assert.equal(sha(regular(path.join(firmware, name))), pin);
  for (const name of ['08_IMAGE', '83_IMAGE', '99_IMAGE']) assert(!exists(path.join(firmware, name)), `${name} must be absent`);
  assert(regular(path.join(firmware, '79_IMAGE'), 8192).toString().split(/\r?\n/).every(line => !line || line.startsWith('#')),
    '79_IMAGE must be comment-only');
  const usbPath = options.usbPath;
  assert(/^\d+-\d+(?:\.\d+)*$/.test(usbPath), 'explicit physical USB path required');
  const helper = processIdentity(Number(regular(path.join(options.session, 'usb-boot.pid'), 32).toString().trim()));
  const client = processIdentity(Number(regular(path.join(options.session, 'console-client.pid'), 32).toString().trim()));
  assert.equal(sha(regular(fs.realpathSync(`/proc/${helper.pid}/exe`))),
    'da7f22dfa94275b39dd0b7c5cd2743162e4876cf74f9d95bca1955edd7b75238', 'unreviewed USB helper');
  assert.deepEqual(helper.args.slice(1, 3), ['1286', '8174']);
  assert.equal(fs.realpathSync(helper.args[3]), firmware, 'helper uses a different firmware directory');
  assert.equal(helper.args[4], '8141');
  assert(client.args.some(arg => path.basename(arg) === 'uboot-console.py'), 'unrecognized console client');
  const consoleLog = path.join(options.session, 'uboot-console.log');
  const target = () => {
    const descriptors = fs.readdirSync(`/proc/${helper.pid}/fd`).flatMap(fd => {
      try { return [fs.readlinkSync(`/proc/${helper.pid}/fd/${fd}`)]; }
      catch (error) { if (error.code === 'ENOENT') return []; throw error; }
    });
    return verifyUsbTarget('/sys/bus/usb/devices', usbPath, descriptors);
  };
  const deviceNumber = target();
  assert.equal(fs.realpathSync('/tmp/uboot.log'), consoleLog);
  assert(fs.lstatSync('/tmp/uboot_cmd').isFIFO());
  const fifo = fs.statSync('/tmp/uboot_cmd');
  const held = fs.readdirSync(`/proc/${client.pid}/fd`).some(fd => {
    try {
      const stat = fs.statSync(`/proc/${client.pid}/fd/${fd}`);
      return stat.dev === fifo.dev && stat.ino === fifo.ino;
    } catch (error) { if (error.code === 'ENOENT') return false; throw error; }
  });
  assert(held, 'console client does not hold the command FIFO');
  return {
    consoleLog, helperLog: path.join(options.session, 'usbboot.log'),
    send(command) {
      assert(sameProcess(helper) && sameProcess(client), 'recovery session changed');
      assert.equal(target(), deviceNumber,
        'USB target re-enumerated during preparation');
      const fd = fs.openSync('/tmp/uboot_cmd', fs.constants.O_WRONLY | fs.constants.O_NONBLOCK | fs.constants.O_NOFOLLOW);
      try {
        const bytes = Buffer.from(command);
        assert.equal(fs.writeSync(fd, bytes), bytes.length, 'partial command; do not reissue');
      } finally { fs.closeSync(fd); }
    },
    async stop() {
      for (const identity of [client, helper]) {
        if (sameProcess(identity)) process.kill(identity.pid, 'SIGTERM');
      }
      for (let i = 0; i < 30 && [client, helper].some(sameProcess); i++) await sleep(100);
      for (const identity of [client, helper]) {
        if (sameProcess(identity)) process.kill(identity.pid, 'SIGKILL');
      }
      await sleep(200);
      assert(![client, helper].some(sameProcess), 'helper cleanup did not complete');
    },
  };
}

export function capture(file, offset) {
  const fd = fs.openSync(file, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
  try {
    const stat = fs.fstatSync(fd);
    assert(stat.isFile() && stat.size <= 32 * 1024 * 1024, 'invalid or oversized session log');
    assert(stat.size >= offset, 'session log was truncated');
    // Read exactly this snapshot; a healthy logger can append while we read.
    const bytes = Buffer.alloc(stat.size - offset);
    let read = 0;
    while (read < bytes.length) {
      const count = fs.readSync(fd, bytes, read, bytes.length - read, offset + read);
      assert(count > 0, 'session log was truncated during capture');
      read += count;
    }
    return bytes.toString();
  } finally { fs.closeSync(fd); }
}
async function waitUntil(predicate, timeoutMs) {
  const end = Date.now() + timeoutMs;
  while (!predicate()) {
    assert(Date.now() < end, 'observation timeout; preserve power, logs and intent, never reissue automatically');
    await sleep(200);
  }
}
async function challenge(live, geometry) {
  const offset = fs.statSync(live.consoleLog).size;
  const nonce = `RI_${crypto.randomBytes(12).toString('hex')}`;
  live.send(`\necho ${nonce}\nversion\n${geometry ? 'nandinit\n' : ''}echo ${nonce}_END\n\n`);
  const start = new RegExp(`^${nonce}$`, 'm');
  const end = new RegExp(`^${nonce}_END$`, 'm');
  await waitUntil(() => {
    const text = clean(capture(live.consoleLog, offset));
    return start.test(text) && end.test(text) && text.includes('U-Boot 2013.04') &&
      text.slice(text.lastIndexOf(`${nonce}_END`)).includes('MV88D');
  }, 15000);
  const text = capture(live.consoleLog, offset);
  if (geometry) {
    assert(/NAND chip id 98DA90157616/i.test(text), 'unexpected NAND ID');
    assert(text.includes('Chip size 268435456B, block size: 131072B, page size: 2048B, oob size: 32B'),
      'unexpected recovery NAND geometry');
  }
  return text;
}

export async function flash(options, dependencies = {}) {
  const startedUTC = new Date().toISOString();
  assert.equal(options.confirm, ACK, 'explicit whole-good-block erase acknowledgment required');
  assert(options.approval?.trim(), 'external owner approval reference required');
  assert(/^[a-f0-9]{64}$/.test(options.expectedSHA), 'explicit candidate hash required');
  const bundle = (dependencies.inspect || inspect)(options.bundle, options.expectedSHA);
  const evidence = options.evidence;
  outsideRepo(evidence);
  assert(!exists(evidence), 'new evidence directory required');
  assert(![0x01021994, 0x858458f6].includes(fs.statfsSync(path.dirname(evidence)).type),
    'intent and evidence require durable storage, not tmpfs/ramfs');
  const intent = path.join(options.bundle, `.flash-${options.expectedSHA}-${options.usbPath}.intent.json`);
  assert(!exists(intent), 'prior flash intent exists; no automatic reissue');
  const live = (dependencies.session || session)(options);
  fs.mkdirSync(evidence, { mode: 0o700 });
  save(path.join(evidence, 'INPUT.json'), { startedUTC, imageSHA256: bundle.hash,
    imageBytes: bundle.image.length, sessionDirectory: options.session, physicalUSB: options.usbPath });
  const events = [];
  function record(stage, details = {}) {
    const entry = { timeUTC: new Date().toISOString(), stage, ...details };
    events.push(entry);
    const fd = fs.openSync(path.join(evidence, 'events.jsonl'), 'a', 0o600);
    try { fs.writeSync(fd, JSON.stringify(entry) + '\n'); fs.fsyncSync(fd); }
    finally { fs.closeSync(fd); }
    console.log(JSON.stringify(entry));
  }
  record('artifact-verified', { imageSHA256: bundle.hash, imageBytes: bundle.image.length });
  const lock = path.join(options.firmware, '.native-flash-lock.json');
  save(lock, { pid: process.pid, evidence, imageSHA256: bundle.hash });
  const initialChallenge = await challenge(live, true);
  save(path.join(evidence, 'preflight-console.txt'), initialChallenge);
  record('fresh-prompt-and-geometry-verified');
  const active = path.join(options.firmware, '83_IMAGE');
  const lengthFile = path.join(options.firmware, '07_IMAGE');
  const oldLength = exists(lengthFile) ? regular(lengthFile, 4) : null;
  if (oldLength) save(path.join(evidence, '07_IMAGE.before'), oldLength);
  save(active, bundle.image);
  assert.equal(sha(regular(active)), bundle.hash);
  save(`${lengthFile}.native-pending`, bundle.length);
  fs.renameSync(`${lengthFile}.native-pending`, lengthFile);
  assert(regular(lengthFile, 4).equals(bundle.length));
  record('staging-complete');
  const request = { timeUTC: new Date().toISOString(), imageSHA256: bundle.hash,
    imageBytes: bundle.image.length, approvalReference: options.approval,
    command: 'l2nand 83', physicalUSB: options.usbPath, noAutomaticRetry: true };
  save(path.join(evidence, 'FLASH-INTENT.json'), request);
  save(intent, request);
  const consoleOffset = fs.statSync(live.consoleLog).size;
  const helperOffset = fs.statSync(live.helperLog).size;
  live.send('\nl2nand 83\n\n');
  record('flash-command-submitted');
  const seen = new Set();
  await waitUntil(() => {
    const text = clean(capture(live.consoleLog, consoleOffset));
    for (const match of text.matchAll(/^burn ([^,]+),/gm)) {
      if (!seen.has(match[1])) { seen.add(match[1]); record('vendor-record', { name: match[1] }); }
    }
    assert(!/\b(?:error|failed|failure|uncorrectable|mismatch)\b/i.test(text), 'vendor reported an error; no retry');
    return text.includes('Congratulations! u2nand succeed!');
  }, 15 * 60 * 1000);
  const consoleText = capture(live.consoleLog, consoleOffset);
  const helperText = capture(live.helperLog, helperOffset);
  save(path.join(evidence, 'flash-console.txt'), consoleText);
  save(path.join(evidence, 'flash-transport.txt'), helperText);
  const coverage = verifyFlash(consoleText, helperText, bundle);
  record('vendor-program-read-verified', { coverage });
  save(path.join(evidence, 'postflash-console.txt'), await challenge(live, false));
  record('fresh-postflash-prompt-verified');
  await live.stop();
  record('helper-stopped');
  assert.equal(sha(regular(active)), bundle.hash, 'staged image changed before cleanup');
  fs.unlinkSync(active);
  assert(regular(lengthFile, 4).equals(bundle.length), 'staged length changed before cleanup');
  if (oldLength) {
    save(`${lengthFile}.native-restore`, oldLength);
    fs.renameSync(`${lengthFile}.native-restore`, lengthFile);
  } else fs.unlinkSync(lengthFile);
  assert.equal(JSON.parse(regular(lock)).evidence, evidence, 'flash lock ownership changed');
  fs.unlinkSync(lock);
  record('staging-cleanup-complete');
  const result = { status: 'vendor-program-read-and-fresh-prompt-verified', imageSHA256: bundle.hash,
    imageBytes: bundle.image.length, coverage, helperStopped: true, activeImageRemoved: true,
    independentLinuxReadback: false, nativeBootAccepted: false,
    next: 'STOP. Arm passive USB observation with the helper off, then obtain an owner-controlled normal boot; no automatic reboot. Disconnect USB only for a separately chosen host-independence test.',
    startedUTC, completedUTC: new Date().toISOString(),
    elapsedSeconds: (Date.now() - Date.parse(startedUTC)) / 1000,
    phaseSeconds: events.map((event, index) => ({
      stage: event.stage,
      sincePrevious: (Date.parse(event.timeUTC) - Date.parse(events[index - 1]?.timeUTC || startedUTC)) / 1000,
    })) };
  save(path.join(evidence, 'RESULT.json'), result);
  return result;
}

const usage = `Usage: flash-native-once.mjs BUNDLE_DIR [inspect|flash] [options]
Default inspect reads regular bundle files only; it never contacts hardware.
flash requires --expected-sha256 HASH --confirm ${ACK} --approval-ref TEXT
  --firmware-dir DIR --session-dir DIR --usb-path PHYSICAL_PATH --evidence NEW_DIR
Use the existing recovery helper to reach U-Boot first. This tool never resets,
starts a helper, retries a flash, or boots Linux. It erases ALL GOOD NAND BLOCKS
through the vendor programmer, verifies its record/read loops, then stops the helper.
No image99. Software acknowledgment is not external owner approval.`;

export async function main(argv) {
  if (!argv.length || argv[0] === '--help') { console.log(usage); return; }
  const [directory, action = 'inspect', ...args] = argv;
  assert(['inspect', 'flash'].includes(action), usage);
  const keys = {
    '--expected-sha256': 'expectedSHA', '--confirm': 'confirm', '--approval-ref': 'approval',
    '--firmware-dir': 'firmware', '--session-dir': 'session', '--usb-path': 'usbPath', '--evidence': 'evidence',
  };
  const options = { bundle: fs.realpathSync(directory) };
  for (let index = 0; index < args.length; index += 2) {
    const key = keys[args[index]];
    assert(key && args[index + 1] && !options[key], 'unknown, missing or repeated option');
    options[key] = args[index + 1];
  }
  if (action === 'inspect') {
    assert(Object.keys(options).every(key => ['bundle', 'expectedSHA'].includes(key)), 'inspect does not accept write options');
    const bundle = inspect(options.bundle, options.expectedSHA);
    console.log(JSON.stringify({ action, imageSHA256: bundle.hash, bytes: bundle.image.length,
      records: bundle.records, approval: false, nativeBootAccepted: false }, null, 2));
    return;
  }
  for (const key of Object.values(keys)) assert(options[key], `missing ${key}`);
  assert(/^\d+-\d+(?:\.\d+)*$/.test(options.usbPath));
  options.firmware = fs.realpathSync(options.firmware);
  options.session = fs.realpathSync(options.session);
  options.evidence = path.join(fs.realpathSync(path.dirname(path.resolve(options.evidence))), path.basename(options.evidence));
  outsideRepo(options.bundle);
  outsideRepo(options.firmware);
  console.log(JSON.stringify(await flash(options), null, 2));
}
if (process.argv[1] && fs.realpathSync(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).catch(error => {
    console.error(`STOP: ${error.message}\nKeep power and transport unchanged on failure. Preserve logs and intent; do not retry automatically.`);
    process.exitCode = 1;
  });
}
