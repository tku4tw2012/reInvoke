// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT
'use strict';

const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');

const components = Object.freeze([
  { key: 'persist', target: 'bin/reinvoke-persist', baseline: false,
    purpose: 'verified app/YAFFS2 storage, private snapshots and profile IPC' },
  { key: 'applyd', target: 'usr/sbin/reinvoke-wifi-applyd', baseline: true,
    purpose: 'save after COMPLETED and resume a validated derived station profile' },
  { key: 'mcu', target: 'opt/reinvoke/bin/reinvoke-mcu-interface', baseline: true,
    purpose: 'private RAM music-volume preference without weakening safe reconnect ceiling' },
]);
const seedTarget = 'etc/reinvoke-wifi/station-seed.json';

const hooks = Object.freeze({
  prepare: {
    argv: ['/bin/reinvoke-persist', 'prepare'],
    timeoutSeconds: 12,
    order: 'after private RAM directories exist; before BlueZ/MCU/core consumers',
    failure: 'record named failure; continue Bluetooth/setup volatile',
  },
  serve: {
    argv: ['/bin/reinvoke-persist', 'serve'],
    order: 'only following successful prepare; supervise independently',
    readySocket: '/run/reinvoke/persistence.sock',
    readinessTimeoutSeconds: 3,
  },
  resume: {
    argv: ['/usr/sbin/reinvoke-wifi-applyd', '--resume', '--connect-timeout', '20s'],
    timeoutSeconds: 30,
    order: 'once per boot after interface readiness; serialize before provisioning windowd',
    failure: 'continue physical onboarding; do not gate Bluetooth or SSH',
  },
  mcuArguments: ['--music-volume-state', '/run/reinvoke/music-volume'],
  shutdown: {
    signal: 'TERM',
    order: 'after stopping BlueZ/MCU writers; before syslogd and unmount',
    waitSeconds: 5,
    successMarker: 'PERSIST_FLUSHED',
  },
  status: {
    storage: '/run/reinvoke/persistence-status.json',
    storageMaxBytes: 512,
    wifi: '/run/reinvoke/wifi-persistence-status',
    wifiMaxBytes: 128,
    privateMode: '0600',
    interpretation: 'last operation only; verify supervised process identity separately; association is not DHCP',
  },
});

const sha = value => crypto.createHash('sha256').update(value).digest('hex');

function inspect(file) {
  try { return fs.lstatSync(file); }
  catch (error) {
    if (error.code === 'ENOENT') return null;
    throw error;
  }
}

function staticARM(data) {
  if (data.length < 52 || data.length > 16 * 1024 * 1024 ||
      data.subarray(0, 4).toString('hex') !== '7f454c46' ||
      data[4] !== 1 || data[5] !== 1 ||
      data.readUInt16LE(16) !== 2 || data.readUInt16LE(18) !== 40)
    throw new Error('persistence runtime input must be a static 32-bit ARM executable');
  const offset = data.readUInt32LE(28);
  const size = data.readUInt16LE(42);
  const count = data.readUInt16LE(44);
  if (offset < 52 || size !== 32 || count < 1 || count > 64 ||
      offset + size * count > data.length)
    throw new Error('persistence runtime ELF program headers are invalid');
  for (let index = 0; index < count; index++) {
    if (data.readUInt32LE(offset + index * size) === 3)
      throw new Error('persistence runtime executable must not require an ELF interpreter');
  }
}

function readInputs(config) {
  if (!config || typeof config !== 'object' || Array.isArray(config) ||
      !['applyd,mcu,persist', 'applyd,mcu,persist,seedProfile'].includes(Object.keys(config).sort().join(',')))
    throw new Error('persistence config requires persist, applyd, mcu and optional private seedProfile');
  return components.map(component => {
    const value = config[component.key];
    if (!value || typeof value.path !== 'string' || !path.isAbsolute(value.path) ||
        !/^[a-f0-9]{64}$/.test(value.sha256 || '') ||
        Object.keys(value).sort().join(',') !== 'path,sha256')
      throw new Error('persistence artifacts require absolute paths and explicit SHA256 pins');
    const info = fs.lstatSync(value.path);
    if (!info.isFile() || info.isSymbolicLink())
      throw new Error('persistence runtime artifact must be a non-symlink regular file');
    const data = fs.readFileSync(value.path);
    if (sha(data) !== value.sha256)
      throw new Error('persistence runtime artifact SHA256 mismatch');
    staticARM(data);
    return { ...component, data, sha256: value.sha256 };
  });
}

function readSeed(config) {
  if (config.seedProfile === undefined) return null;
  const seed = config.seedProfile;
  if (!seed || typeof seed.path !== 'string' || !path.isAbsolute(seed.path) ||
      !/^[a-f0-9]{64}$/.test(seed.sha256 || '') ||
      Object.keys(seed).sort().join(',') !== 'path,sha256')
    throw new Error('private station seed requires an absolute path and SHA256 pin');
  const info = fs.lstatSync(seed.path);
  // Node statx reports real ownership while fakeroot overrides getuid().
  const uid = /^Uid:\s+\d+\s+\d+\s+\d+\s+(\d+)\s*$/m.exec(
    fs.readFileSync('/proc/self/status', 'utf8'));
  if (!uid) throw new Error('builder filesystem identity is unavailable');
  if (!info.isFile() || info.isSymbolicLink() || info.nlink !== 1 ||
      (info.mode & 0o777) !== 0o600 || info.size < 1 || info.size > 4096 ||
      (info.uid !== Number(uid[1]) && info.uid !== 0))
    throw new Error('private station seed must be a private single-link regular file');
  const resolved = fs.realpathSync(seed.path);
  const repository = path.resolve(__dirname, '../..');
  if (resolved === repository || resolved.startsWith(repository + path.sep))
    throw new Error('private station seed input must remain outside the worktree');
  const data = fs.readFileSync(seed.path);
  if (sha(data) !== seed.sha256) throw new Error('private station seed SHA256 mismatch');
  const text = data.toString('utf8');
  if (!Buffer.from(text, 'utf8').equals(data)) throw new Error('private station seed UTF-8 is invalid');
  let value;
  try { value = JSON.parse(text); }
  catch { throw new Error('private station seed is not valid JSON'); }
  const seen = new Set();
  for (const match of text.matchAll(/"(?:\\.|[^"\\])*"/g)) {
    if (!/^\s*:/.test(text.slice(match.index + match[0].length))) continue;
    const key = JSON.parse(match[0]);
    if (seen.has(key)) throw new Error('private station seed contains duplicate fields');
    seen.add(key);
  }
  if (!value || typeof value !== 'object' || Array.isArray(value) ||
      !['psk,security,ssid', 'hidden,psk,security,ssid',
        'passphrase,security,ssid', 'hidden,passphrase,security,ssid'].includes(Object.keys(value).sort().join(',')) ||
      typeof value.ssid !== 'string' || Buffer.byteLength(value.ssid) < 1 ||
      Buffer.byteLength(value.ssid) > 32 || /[\0\r\n]/.test(value.ssid) ||
      Buffer.from(value.ssid).toString('utf8') !== value.ssid ||
      value.security !== 'wpa2-psk' || (value.hidden !== undefined && typeof value.hidden !== 'boolean'))
    throw new Error('private station seed profile is invalid');
  let psk;
  if (Object.hasOwn(value, 'passphrase')) {
    if (typeof value.passphrase !== 'string' || Buffer.byteLength(value.passphrase) < 8 ||
        Buffer.byteLength(value.passphrase) > 63 || /[\0\r\n]/.test(value.passphrase) ||
        Buffer.from(value.passphrase).toString('utf8') !== value.passphrase)
      throw new Error('private station seed request is invalid');
    psk = crypto.pbkdf2Sync(value.passphrase, value.ssid, 4096, 32, 'sha1').toString('hex');
  } else {
    if (typeof value.psk !== 'string' || !/^[a-f0-9]{64}$/i.test(value.psk))
      throw new Error('private station seed derived key is invalid');
    psk = value.psk.toLowerCase();
  }
  return Buffer.from(JSON.stringify({
    ssid: value.ssid, psk, security: value.security, hidden: value.hidden === true,
  }) + '\n');
}

function readPersistenceConfig(file) {
  const config = JSON.parse(fs.readFileSync(file, 'utf8'));
  readInputs(config);
  readSeed(config);
  return config;
}

function safeTarget(root, relative, createParents) {
  const parts = relative.split('/');
  if (parts.some(part => !part || part === '.' || part === '..'))
    throw new Error('invalid persistence installation path');
  let current = root;
  for (const part of parts.slice(0, -1)) {
    current = path.join(current, part);
    if (!fs.existsSync(current)) {
      if (!createParents) throw new Error('baseline persistence component is absent');
      fs.mkdirSync(current, { mode: 0o755 });
    }
    const info = fs.lstatSync(current);
    if (!info.isDirectory() || info.isSymbolicLink())
      throw new Error('persistence installation directory must not be a symlink');
  }
  return path.join(root, relative);
}

// Call once after private-config.installConfig(config, root). Runtime startup
// composition stays in patch-runtime.js; the returned hooks define its contract.
function installPersistence(config, root) {
  const absoluteRoot = path.resolve(root);
  const rootInfo = fs.lstatSync(absoluteRoot);
  if (!rootInfo.isDirectory() || rootInfo.isSymbolicLink())
    throw new Error('persistence installation root must be a directory');
  const inputs = readInputs(config);
  const seed = readSeed(config);
  const planned = inputs.map(component => {
    const target = safeTarget(absoluteRoot, component.target, !component.baseline);
    let beforeSHA256 = null;
    const info = inspect(target);
    if (info) {
      if (!info.isFile() || info.isSymbolicLink() || info.nlink !== 1)
        throw new Error('persistence installation target must be a single-link regular file');
      if (!component.baseline)
        throw new Error('persistence installer must run only once on a fresh runtime');
      beforeSHA256 = sha(fs.readFileSync(target));
    } else if (component.baseline) {
      throw new Error('baseline persistence component is absent');
    }
    return { ...component, targetPath: target, beforeSHA256 };
  });
  const mount = path.join(absoluteRoot, 'persist');
  const mountInfo = inspect(mount);
  if (mountInfo) {
    const info = mountInfo;
    if (!info.isDirectory() || info.isSymbolicLink() || fs.readdirSync(mount).length !== 0)
      throw new Error('persistence mountpoint must be an empty non-symlink directory');
  }
  let seedPath = null;
  if (seed) {
    seedPath = safeTarget(absoluteRoot, seedTarget, true);
    if (inspect(seedPath)) throw new Error('refusing to overwrite an existing private station seed');
    fs.chmodSync(path.dirname(seedPath), 0o700);
  }
  for (const component of planned) {
    fs.writeFileSync(component.targetPath, component.data, { mode: 0o755 });
    fs.chmodSync(component.targetPath, 0o755);
  }
  fs.mkdirSync(mount, { recursive: true, mode: 0o700 });
  fs.chmodSync(mount, 0o700);
  if (seedPath) {
    fs.writeFileSync(seedPath, seed, { mode: 0o600, flag: 'wx' });
    fs.chmodSync(seedPath, 0o600);
  }
  return {
    schema: 1,
    storage: '/persist/reinvoke',
    filesystem: 'yaffs2',
    runtimeGateRequired: true,
    reflashesRetainState: false,
    seed: {
      installed: seed !== null,
      path: seedTarget,
      policy: 'only when verified saved profile is absent; commit only after COMPLETED',
    },
    changedBaselineComponents: planned.filter(item => item.baseline).map(item => item.target),
    components: planned.map(item => ({
      path: item.target, beforeSHA256: item.beforeSHA256, sha256: item.sha256,
      bytes: item.data.length, baseline: item.baseline, purpose: item.purpose,
    })),
    hooks: JSON.parse(JSON.stringify(hooks)),
  };
}

module.exports = { readPersistenceConfig, installPersistence, hooks };
