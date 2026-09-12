#!/usr/bin/env node
// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT

import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import assert from 'node:assert/strict';
import { crc32 } from 'node:zlib';
import { ACK, sha, layout, checkBundle, inspect, verifyFlash, verifyUsbTarget, capture, flash } from './flash-native-once.mjs';

const root = fs.mkdtempSync(path.join(os.tmpdir(), 'reinvoke-native-flash-test-'));
const report = console.log;
console.log = () => {};
let cases = 0;

function fixture() {
  const header = Buffer.alloc(640);
  header.writeUInt32LE(0xd2ada3f1);
  header.writeUInt32LE(2048, 12);
  header.writeUInt32LE(64, 20);
  header.writeUInt32LE(2048, 24);
  header.writeUInt32LE(9, 28);
  Buffer.from('234130', 'hex').copy(header, 32);
  const payloads = [], records = [], pins = [];
  let offset = header.length;
  layout.forEach(([name, start, blocks, type, pin], index) => {
    const size = name === 'pre-bootloader' ? 131072 : type === 1 ? 2080 : 256;
    const payload = Buffer.alloc(size, index + 1);
    if (!pin) {
      payload.writeUInt32LE(0x73717368);
      payload.writeBigUInt64LE(BigInt(size), 40);
    }
    const at = 64 + index * 64;
    header.write(name, at);
    header.writeBigUInt64LE(BigInt(size), at + 16);
    header.writeUInt32LE(crc32(payload), at + 24);
    header.writeUInt32LE(start, at + 40);
    header.writeUInt32LE(blocks, at + 44);
    header.writeUInt32LE(type, at + 48);
    records.push({ name, start: start * 131072, capacity: blocks * 131072,
      dataType: type, bytes: size, outputOffset: offset, sha256: sha(payload) });
    pins.push([name, start, blocks, type, pin ? sha(payload) : null]);
    payloads.push(payload);
    offset += size;
  });
  const image = Buffer.concat([header, ...payloads]);
  const manifest = {
    source: { sha256: 'b2e12178f98a0c0904cb1e6e2ba933de0c0fef8be7c24e7852bc9933294850e8' },
    image: { file: '83_IMAGE.fixture', bytes: image.length, sha256: sha(image) }, records,
  };
  const length = Buffer.alloc(4);
  length.writeUInt32LE(image.length);
  const bundle = { image, length, hash: sha(image), records: checkBundle(manifest, image, sha(image), pins) };
  return { image, manifest, pins, bundle };
}

function transcript(bundle) {
  let text = `do_l2nand, loadimg image 0x83, image size ${bundle.image.length}\n`;
  text += 'Bad block found @ 0x00C000000.\nBad block found @ 0x00C020000.\n2046 blocks erased.\n';
  for (const r of bundle.records) {
    text += `burn ${r.name}, image size ${r.bytes}, partition start:${r.start / 131072}, size ${r.blocks}, reserve 0, data type: ${r.dataType ? 'oob' : 'normal'}, crc 0x${r.crc.toString(16)}\n`;
    for (const action of ['writing', 'reading']) {
      for (let block = 0; block < r.writtenBlocks; block++) {
        text += `${action} NAND at address 0x${(r.start + block * 131072).toString(16)}\r`;
      }
    }
    text += '\n';
  }
  return text + 'Congratulations! u2nand succeed!\nMV88D';
}

function runtime(bundle, directory, mode = 'ok') {
  const log = path.join(directory, 'uboot-console.log');
  const helper = path.join(directory, 'usbboot.log');
  fs.writeFileSync(log, 'Old prompt and stale success must not satisfy a new flash.\n');
  fs.writeFileSync(helper, 'Old transport output\n');
  const commands = [];
  let stopped = false;
  return {
    commands,
    wasStopped: () => stopped,
    live: {
      consoleLog: log, helperLog: helper,
      send(command) {
        commands.push(command);
        if (command.includes('l2nand 83')) {
          const text = transcript(bundle);
          fs.appendFileSync(log, mode === 'missing-read'
            ? text.replace('reading NAND at address 0x0\r', '') : text);
          if (mode !== 'missing-transfer') {
            fs.appendFileSync(helper, `Image transfer complete: 83_IMAGE (${bundle.image.length} bytes sent)\n`);
          }
        } else {
          assert(!/\b(reset|bootm|bootd|go|saveenv|setenv)\b/.test(command));
          const nonce = command.match(/echo (RI_[a-f0-9]+)/)[1];
          let text = `\n${nonce}\nU-Boot 2013.04 (fixture)\n`;
          if (command.includes('nandinit')) {
            text += 'NAND chip id 98DA90157616FFFF\n';
            text += `Chip size ${mode === 'wrong-geometry' ? '536870912' : '268435456'}B, block size: 131072B, page size: 2048B, oob size: 32B, ecc: 48bits/2kB\n`;
          }
          fs.appendFileSync(log, `${text}${nonce}_END\nMV88DE3100|> `);
        }
      },
      async stop() {
        assert(commands.some(c => c.includes('l2nand 83')));
        if (mode === 'stop-failure') throw new Error('mock helper cleanup failed');
        stopped = true;
      },
    },
  };
}

try {
  const sys = path.join(root, 'usb-devices');
  fs.mkdirSync(sys);
  function usb(name, number) {
    const dir = path.join(sys, name);
    fs.mkdirSync(dir);
    for (const [file, value] of Object.entries({ idVendor: '1286', idProduct: '8174', busnum: '1', devnum: number })) {
      fs.writeFileSync(path.join(dir, file), value + '\n');
    }
  }
  usb('1-2', '7');
  assert.equal(verifyUsbTarget(sys, '1-2', ['/dev/bus/usb/001/007']), '7');
  assert.throws(() => verifyUsbTarget(sys, '1-2', ['/dev/bus/usb/001/008']), /helper does not hold/);
  assert.throws(() => verifyUsbTarget(sys, '1-3', ['/dev/bus/usb/001/007']), /different physical path/);
  usb('1-3', '8');
  assert.throws(() => verifyUsbTarget(sys, '1-2', ['/dev/bus/usb/001/007']), /exactly one/);
  cases += 4;

  const growingLog = path.join(root, 'growing.log');
  fs.writeFileSync(growingLog, 'prefix\nsnapshot\n');
  const readSync = fs.readSync;
  try {
    fs.readSync = (...args) => {
      fs.readSync = readSync;
      fs.appendFileSync(growingLog, 'later output\n');
      return readSync(...args);
    };
    assert.equal(capture(growingLog, 7), 'snapshot\n', 'appends must not invalidate captured logs');
  } finally { fs.readSync = readSync; }
  assert.equal(capture(growingLog, 7), 'snapshot\nlater output\n');
  assert.throws(() => capture(growingLog, 1000), /truncated/);
  cases += 3;

  const { image, manifest, pins, bundle } = fixture();
  assert.equal(bundle.records.length, 9);
  cases++;
  assert.throws(() => checkBundle(manifest, image, '0'.repeat(64), pins), /hash/);
  cases++;
  for (const change of ['crc', 'allocation', 'vendor', 'trailer', 'descriptor', 'platform', 'partition-type']) {
    let damaged = Buffer.from(image);
    const metadata = structuredClone(manifest);
    if (change === 'crc') damaged[manifest.records[6].outputOffset + 100] ^= 1;
    if (change === 'allocation') damaged.writeUInt32LE(400, 64 + 6 * 64 + 40);
    if (change === 'vendor') {
      damaged[manifest.records[2].outputOffset + 100] ^= 1;
      damaged.writeUInt32LE(crc32(damaged.subarray(manifest.records[2].outputOffset,
        manifest.records[2].outputOffset + manifest.records[2].bytes)), 64 + 2 * 64 + 24);
    }
    if (change === 'trailer') damaged = Buffer.concat([damaged, Buffer.from('extra')]);
    if (change === 'descriptor') metadata.records[6].outputOffset++;
    if (change === 'platform') damaged[32] ^= 1;
    if (change === 'partition-type') damaged.writeUInt32LE(1, 64 + 6 * 64 + 52);
    metadata.image.sha256 = sha(damaged);
    metadata.image.bytes = damaged.length;
    assert.throws(() => checkBundle(metadata, damaged, sha(damaged), pins), undefined, change);
    cases++;
  }
  const transport = `Image transfer complete: 83_IMAGE (${image.length} bytes sent)`;
  const consoleText = transcript(bundle);
  assert.equal(verifyFlash(consoleText, transport, bundle).length, 9);
  cases++;
  for (const [label, text, log] of [
    ['no success', consoleText.replace('Congratulations! u2nand succeed!', ''), transport],
    ['incomplete read', consoleText.replace('reading NAND at address 0x0\r', ''), transport],
    ['false image size', consoleText.replace(`image size ${image.length}`, 'image size 1'), transport],
    ['ECC failure', consoleText + '\nECC uncorrectable error\n', transport],
    ['erase inventory', consoleText.replace('2046 blocks', '2045 blocks'), transport],
    ['missing transport', consoleText, ''],
  ]) {
    assert.throws(() => verifyFlash(text, log, bundle), undefined, label);
    cases++;
  }

  for (const mode of ['ok', 'wrong-geometry', 'missing-read', 'missing-transfer', 'stop-failure']) {
    const directory = path.join(root, mode);
    fs.mkdirSync(directory);
    const firmware = path.join(directory, 'firmware');
    const artifacts = path.join(directory, 'bundle');
    fs.mkdirSync(firmware); fs.mkdirSync(artifacts);
    const previousLength = Buffer.from([1, 2, 3, 4]);
    fs.writeFileSync(path.join(firmware, '07_IMAGE'), previousLength);
    const adapter = runtime(bundle, directory, mode);
    const options = {
      bundle: artifacts, firmware, evidence: path.join(directory, 'evidence'),
      usbPath: '1-2', approval: 'mock-only approval reference', expectedSHA: bundle.hash, confirm: ACK,
    };
    const deps = { inspect: () => bundle, session: () => adapter.live };
    if (mode === 'ok') {
      const result = await flash(options, deps);
      assert.equal(result.nativeBootAccepted, false);
      assert.equal(result.independentLinuxReadback, false);
      assert.match(result.next, /passive USB observation with the helper off/);
      assert.match(result.next, /owner-controlled normal boot; no automatic reboot/);
      assert.match(result.next, /Disconnect USB only for a separately chosen host-independence test/);
      assert(result.helperStopped && result.activeImageRemoved);
      assert(adapter.wasStopped());
      assert(!fs.existsSync(path.join(firmware, '83_IMAGE')));
      assert(fs.readFileSync(path.join(firmware, '07_IMAGE')).equals(previousLength));
      assert(!fs.existsSync(path.join(firmware, '.native-flash-lock.json')));
    } else {
      await assert.rejects(flash(options, deps), undefined, mode);
      assert(!adapter.wasStopped(), 'failure must not stop a possibly active programmer');
      assert(fs.existsSync(path.join(firmware, '.native-flash-lock.json')));
    }
    const commands = adapter.commands.filter(c => c.includes('l2nand 83'));
    assert.equal(commands.length, mode === 'wrong-geometry' ? 0 : 1);
    if (commands.length) {
      const oldCount = adapter.commands.length;
      await assert.rejects(flash({ ...options, evidence: path.join(directory, 'another-attempt') }, deps),
        /prior flash intent/);
      assert.equal(adapter.commands.length, oldCount);
    }
    cases++;
  }
  await assert.rejects(flash({ confirm: 'wrong' }), /acknowledgment/);
  await assert.rejects(flash({ confirm: ACK }), /approval/);
  cases += 2;

  const realBundle = process.env.REINVOKE_NATIVE_BUNDLE;
  if (realBundle) {
    const verified = inspect(fs.realpathSync(realBundle));
    assert.equal(verified.records.length, 9);
    const evidence = process.env.REINVOKE_NATIVE_FLASH_EVIDENCE;
    if (evidence) {
      const consoleLog = fs.readFileSync(path.join(evidence, 'session-reattach/uboot-console.log'), 'utf8');
      const start = consoleLog.indexOf('MV88DE3100|> l2nand 83');
      assert(start >= 0);
      const helperLog = fs.readFileSync(path.join(evidence, 'session-reattach/usbboot.log'), 'utf8');
      assert.equal(verifyFlash(consoleLog.slice(start), helperLog, verified).length, 9);
    }
    report('PASS: archived bundle and optional real completed-flash transcript, read-only.');
  } else report('SKIP: optional archived artifact/transcript not requested.');
  report(`PASS ${cases} focused offline cases; mock transport only, no device/helper/NAND access.`);
} finally {
  console.log = report;
  fs.rmSync(root, { recursive: true });
}
