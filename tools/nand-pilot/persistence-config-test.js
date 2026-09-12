// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT
'use strict';

const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const cp = require('node:child_process');
const test = require('node:test');
const { installPersistence, hooks } = require('./persistence-config');

function elf(marker) {
  const data = Buffer.alloc(96);
  data.write('7f454c46', 0, 'hex');
  data[4] = 1;
  data[5] = 1;
  data.writeUInt16LE(2, 16);
  data.writeUInt16LE(40, 18);
  data.writeUInt32LE(52, 28);
  data.writeUInt16LE(32, 42);
  data.writeUInt16LE(1, 44);
  data.writeUInt32LE(1, 52);
  data[95] = marker;
  return data;
}

function fixture(t) {
  const directory = fs.mkdtempSync(path.join(__dirname, '.persistence-config-'));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const root = path.join(directory, 'runtime');
  for (const relative of ['bin', 'usr/sbin', 'opt/reinvoke/bin'])
    fs.mkdirSync(path.join(root, relative), { recursive: true });
  fs.writeFileSync(path.join(root, 'usr/sbin/reinvoke-wifi-applyd'), elf(1));
  fs.writeFileSync(path.join(root, 'opt/reinvoke/bin/reinvoke-mcu-interface'), elf(2));
  fs.writeFileSync(path.join(root, 'opt/reinvoke/bin/unchanged'), 'baseline');
  const config = {};
  for (const [index, key] of ['persist', 'applyd', 'mcu'].entries()) {
    const file = path.join(directory, key);
    const data = elf(index + 3);
    fs.writeFileSync(file, data);
    config[key] = { path: file, sha256: crypto.createHash('sha256').update(data).digest('hex') };
  }
  return { directory, root, config };
}

test('one installer replaces exactly two baseline binaries and adds gated helper', t => {
  const { root, config } = fixture(t);
  const report = installPersistence(config, root);
  assert.deepEqual(report.changedBaselineComponents, [
    'usr/sbin/reinvoke-wifi-applyd', 'opt/reinvoke/bin/reinvoke-mcu-interface',
  ]);
  assert.equal(report.components.length, 3);
  assert.equal(report.filesystem, 'yaffs2');
  assert.equal(report.runtimeGateRequired, true);
  assert.equal(report.seed.installed, false);
  assert.equal(fs.existsSync(path.join(root, 'etc/reinvoke-wifi/station-seed.json')), false);
  assert.equal(fs.readFileSync(path.join(root, 'opt/reinvoke/bin/unchanged'), 'utf8'), 'baseline');
  assert.equal(fs.statSync(path.join(root, 'persist')).mode & 0o777, 0o700);
  assert.deepEqual(fs.readdirSync(path.join(root, 'persist')), []);
  for (const item of report.components) {
    const file = path.join(root, item.path);
    assert.equal(fs.statSync(file).mode & 0o777, 0o755);
    assert.equal(crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex'), item.sha256);
  }
  assert.throws(() => installPersistence(config, root), /only once/);
});

test('wrong pin and non-ARM/dynamic artifacts fail before runtime replacement', t => {
  for (const kind of ['hash', 'machine', 'interpreter']) {
    const { root, config } = fixture(t);
    if (kind === 'hash') config.persist.sha256 = '0'.repeat(64);
    else {
      const data = fs.readFileSync(config.persist.path);
      if (kind === 'machine') data.writeUInt16LE(62, 18);
      else data.writeUInt32LE(3, 52);
      fs.writeFileSync(config.persist.path, data);
      config.persist.sha256 = crypto.createHash('sha256').update(data).digest('hex');
    }
    assert.throws(() => installPersistence(config, root));
    assert.deepEqual(fs.readFileSync(path.join(root, 'usr/sbin/reinvoke-wifi-applyd')), elf(1));
    assert.equal(fs.existsSync(path.join(root, 'bin/reinvoke-persist')), false);
  }
});

test('symlink targets, absent baseline and nonempty persist are rejected', t => {
  for (const kind of ['symlink', 'dangling-helper', 'absent', 'nonempty']) {
    const { root, config, directory } = fixture(t);
    const applyd = path.join(root, 'usr/sbin/reinvoke-wifi-applyd');
    if (kind === 'symlink') {
      fs.unlinkSync(applyd);
      fs.symlinkSync(path.join(directory, 'applyd'), applyd);
    } else if (kind === 'dangling-helper') {
      fs.symlinkSync(path.join(directory, 'never-create'), path.join(root, 'bin/reinvoke-persist'));
    } else if (kind === 'absent') fs.unlinkSync(applyd);
    else {
      fs.mkdirSync(path.join(root, 'persist'));
      fs.writeFileSync(path.join(root, 'persist/no-touch'), 'vendor-state');
    }
    assert.throws(() => installPersistence(config, root));
    assert.equal(fs.existsSync(path.join(root, 'bin/reinvoke-persist')), false);
  }
});

test('hook contract uses actual runtime locations and bounded independent startup', () => {
  assert.deepEqual(hooks.prepare.argv, ['/bin/reinvoke-persist', 'prepare']);
  assert.deepEqual(hooks.serve.argv, ['/bin/reinvoke-persist', 'serve']);
  assert.equal(hooks.resume.argv[0], '/usr/sbin/reinvoke-wifi-applyd');
  assert.equal(hooks.resume.timeoutSeconds, 30);
  assert.match(hooks.resume.failure, /do not gate Bluetooth or SSH/);
  assert.deepEqual(hooks.mcuArguments, ['--music-volume-state', '/run/reinvoke/music-volume']);
  assert.equal(hooks.shutdown.signal, 'TERM');
  assert.equal(hooks.status.storage, '/run/reinvoke/persistence-status.json');
  assert.equal(hooks.status.storageMaxBytes, 512);
  assert.equal(hooks.status.wifi, '/run/reinvoke/wifi-persistence-status');
  assert.equal(hooks.status.wifiMaxBytes, 128);
});

function privateSeed(t, content) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'reinvoke-seed-input-'));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const file = path.join(directory, 'synthetic-seed.json');
  fs.writeFileSync(file, content, { mode: 0o600 });
  return { path: file, sha256: crypto.createHash('sha256').update(content).digest('hex') };
}

test('private seed validation uses actual filesystem ownership under fakeroot', t => {
  const { directory, config } = fixture(t);
  config.seedProfile = privateSeed(t, JSON.stringify({
    ssid: 'synthetic', passphrase: 'synthetic-password', security: 'wpa2-psk',
  }));
  const file = path.join(directory, 'config.json');
  fs.writeFileSync(file, JSON.stringify(config));
  const result = cp.spawnSync('fakeroot', [process.execPath, '-e',
    'require(process.argv[1]).readPersistenceConfig(process.argv[2]);',
    require.resolve('./persistence-config'), file],
  { encoding: 'utf8', timeout: 15000 });
  assert.equal(result.status, 0, result.stderr);
});

test('optional private derived seed is installed privately without entering reports', t => {
  const { root, config } = fixture(t);
  const seed = {ssid: 'synthetic-first-boot', psk: 'a'.repeat(64), security: 'wpa2-psk', hidden: true};
  config.seedProfile = privateSeed(t, JSON.stringify(seed));
  const report = installPersistence(config, root);
  assert.equal(report.seed.installed, true);
  const file = path.join(root, report.seed.path);
  assert.equal(fs.statSync(file).mode & 0o777, 0o600);
  assert.equal(fs.statSync(path.dirname(file)).mode & 0o777, 0o700);
  assert.deepEqual(JSON.parse(fs.readFileSync(file, 'utf8')), seed);
  assert.equal(JSON.stringify(report).includes(seed.ssid), false);
  assert.equal(JSON.stringify(report).includes(seed.psk), false);
});

test('invalid, public, ambiguous or in-worktree seed fails before component replacement', t => {
  const valid = {ssid: 'synthetic-first-boot', psk: 'a'.repeat(64), security: 'wpa2-psk'};
  for (const kind of ['passphrase', 'duplicate', 'public', 'in-worktree']) {
    const { root, config, directory } = fixture(t);
    let content = JSON.stringify(valid);
    if (kind === 'passphrase') content = JSON.stringify({...valid, passphrase: 'not-accepted'});
    if (kind === 'duplicate') content = content.replace('{', '{"ssid":"ambiguous",');
    config.seedProfile = privateSeed(t, content);
    if (kind === 'public') fs.chmodSync(config.seedProfile.path, 0o644);
    if (kind === 'in-worktree') {
      const file = path.join(directory, 'seed-in-checkout.json');
      fs.writeFileSync(file, content, { mode: 0o600 });
      config.seedProfile.path = file;
    }
    assert.throws(() => installPersistence(config, root));
    assert.deepEqual(fs.readFileSync(path.join(root, 'usr/sbin/reinvoke-wifi-applyd')), elf(1));
    assert.equal(fs.existsSync(path.join(root, 'bin/reinvoke-persist')), false);
  }
});

test('private provisioning request becomes derived-only immutable seed', t => {
  const { root, config } = fixture(t);
  // Standard public WPA2 PBKDF2 test vector, not operator credentials.
  const request = {ssid: 'IEEE', passphrase: 'password', security: 'wpa2-psk', hidden: false};
  config.seedProfile = privateSeed(t, JSON.stringify(request));
  const report = installPersistence(config, root);
  const text = fs.readFileSync(path.join(root, report.seed.path), 'utf8');
  const seed = JSON.parse(text);
  assert.equal(seed.psk, 'f42c6fc52df0ebef9ebb4b90b38a5f902e83fe1b135a70e23aed762e9710a12e');
  assert.equal(seed.ssid, request.ssid);
  assert.equal(Object.hasOwn(seed, 'passphrase'), false);
  assert.equal(text.includes(request.passphrase), false);
  assert.equal(JSON.stringify(report).includes(request.passphrase), false);
});

test('private provisioning seed rejects invalid passphrases and mixed key schemas', t => {
  for (const passphrase of ['short', 'a'.repeat(64), 'invalid\npassword']) {
    const { root, config } = fixture(t);
    config.seedProfile = privateSeed(t, JSON.stringify({
      ssid: 'synthetic', passphrase, security: 'wpa2-psk',
    }));
    assert.throws(() => installPersistence(config, root));
    assert.equal(fs.existsSync(path.join(root, 'bin/reinvoke-persist')), false);
  }
  const { root, config } = fixture(t);
  config.seedProfile = privateSeed(t, JSON.stringify({
    ssid: 'synthetic', passphrase: 'synthetic-password', psk: 'a'.repeat(64), security: 'wpa2-psk',
  }));
  assert.throws(() => installPersistence(config, root));
});
