// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
//
// Install the donor Bluedroid stack and remove the BlueZ stack it replaces.
//
// The donor radio userspace is shipped as a pinned archive built by
// tools/bluedroid/build-payload.sh. The kernel module and controller firmware
// already ship in the runtime, so only the glibc-linked userspace, its
// launcher, and the one WAMP procedure the donor requires are added here.

'use strict';

const crypto = require('node:crypto');
const cp = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');

// BlueZ and BlueALSA are removed rather than left dormant: shipping two radio
// stacks would double the attack surface and the audio ownership rules for no
// benefit, and only one of them can hold hci0.
const replaced = Object.freeze([
  'opt/reinvoke/bin/bluetoothd',
  'opt/reinvoke/bin/bluealsa',
  'opt/reinvoke/bin/bluealsa-aplay',
  'opt/reinvoke/bin/bluealsa-cli',
  'opt/reinvoke/bin/bluez-pairing-agent',
  'opt/reinvoke/bin/bluez-media-control',
  'opt/reinvoke/bin/hci-init',
  'opt/reinvoke/etc/bluez-main.conf',
]);

const identityPattern = /^[0-9a-f]{12}$/;
const namePattern = /^[A-Za-z0-9._-]{1,32}$/;

function digest(file) {
  return crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
}

function readPinned(entry, label) {
  if (!entry || typeof entry !== 'object')
    throw new Error(`${label} must be an object`);
  const { path: file, sha256 } = entry;
  if (typeof file !== 'string' || !path.isAbsolute(file))
    throw new Error(`${label}.path must be an absolute path`);
  if (typeof sha256 !== 'string' || !/^[0-9a-f]{64}$/.test(sha256))
    throw new Error(`${label}.sha256 must be a lowercase digest`);
  const info = fs.lstatSync(file);
  if (!info.isFile())
    throw new Error(`${label}.path must be a regular file`);
  const actual = digest(file);
  if (actual !== sha256)
    throw new Error(`${label} digest mismatch: ${actual}`);
  return { path: file, sha256, bytes: info.size };
}

function readBluedroidConfig(file) {
  if (!file) return null;
  const config = JSON.parse(fs.readFileSync(file, 'utf8'));
  const known = new Set(['payload', 'identifiers', 'identityHex', 'deviceName']);
  for (const key of Object.keys(config))
    if (!known.has(key)) throw new Error(`unknown Bluedroid configuration field: ${key}`);
  const identityHex = String(config.identityHex || '').toLowerCase();
  if (!identityPattern.test(identityHex))
    throw new Error('identityHex must be twelve lowercase hex digits');
  const deviceName = String(config.deviceName || 'reInvoke');
  if (!namePattern.test(deviceName))
    throw new Error('deviceName must be a short unreserved token');
  return {
    payload: readPinned(config.payload, 'payload'),
    identifiers: readPinned(config.identifiers, 'identifiers'),
    identityHex,
    deviceName,
  };
}

function installBluedroid(config, root, launcher) {
  if (!config) return null;
  const absoluteRoot = path.resolve(root);
  const info = fs.lstatSync(absoluteRoot);
  if (!info.isDirectory() || info.isSymbolicLink())
    throw new Error('Bluedroid installation root must be a directory');

  const removed = [];
  for (const relative of replaced) {
    const target = path.join(absoluteRoot, relative);
    let existing = null;
    try {
      existing = fs.lstatSync(target);
    } catch (error) {
      if (error.code !== 'ENOENT') throw error;
    }
    if (!existing) continue;
    if (!existing.isFile() || existing.isSymbolicLink())
      throw new Error(`refusing to remove an unexpected object: ${relative}`);
    removed.push({ path: relative, bytes: existing.size, sha256: digest(target) });
    fs.rmSync(target);
  }
  if (removed.length === 0)
    throw new Error('no BlueZ components were found to replace');

  const stackRoot = path.join(absoluteRoot, 'opt/bluedroid');
  fs.mkdirSync(stackRoot, { recursive: true, mode: 0o755 });
  cp.execFileSync('tar', ['-xzf', config.payload.path, '-C', stackRoot]);

  const launcherTarget = path.join(stackRoot, 'start.sh');
  fs.copyFileSync(launcher, launcherTarget);
  fs.chmodSync(launcherTarget, 0o755);

  const identifiersTarget = path.join(absoluteRoot, 'bin/reinvoke-identifiers');
  fs.copyFileSync(config.identifiers.path, identifiersTarget);
  fs.chmodSync(identifiersTarget, 0o755);

  // The init reads these rather than embedding private values in a patch.
  const settings = path.join(absoluteRoot, 'etc/nand-pilot');
  fs.mkdirSync(settings, { recursive: true, mode: 0o755 });
  fs.writeFileSync(path.join(settings, 'bluedroid-identity'), `${config.identityHex}\n`, { mode: 0o444 });
  fs.writeFileSync(path.join(settings, 'bluedroid-name'), `${config.deviceName}\n`, { mode: 0o444 });

  return {
    stack: 'bluedroid',
    payload: config.payload,
    identifiers: config.identifiers,
    identityHex: config.identityHex,
    deviceName: config.deviceName,
    installedRoot: 'opt/bluedroid',
    launcher: 'opt/bluedroid/start.sh',
    removedBlueZ: removed,
    status: 'INSTALLED_NOT_RADIO_VERIFIED',
    limit: 'the donor control layer is exercised; controller attachment and audio are unproven',
  };
}

module.exports = { readBluedroidConfig, installBluedroid, replaced };
