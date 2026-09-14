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
  const known = new Set(['payload', 'identifiers', 'hciUp', 'identityHex', 'deviceName']);
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
    hciUp: readPinned(config.hciUp, 'hciUp'),
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

  // The donor tarball is root-relative (lib/, system/lib/, usr/bin/, etc/):
  // it was built to extract at /. Candidate 05 extracted the whole thing into
  // opt/bluedroid, which breaks the Android HAL loader.
  //
  // libhardware.so resolves HAL modules through hardcoded absolute paths
  // (/system/lib/hw and /vendor/lib/hw). It cannot see a relocated copy, so
  // hw_get_module fails, the adapter is never enabled and the radio stays
  // dark. Observed live on 05.3: "ERROR: failed to load BT HAL module!".
  //
  // Extracting everything at / is not the answer either: the donor ships its
  // own glibc (libc.so.6, liblog.so, libglibc_bridge.so and six more) that
  // collides with the runtime's. Those must stay private and be reached only
  // through the launcher's --library-path.
  //
  // So the install is a deliberate split, measured against the runtime:
  //   system/lib/*  -> /system/lib   (15 files, no collisions, REQUIRED there)
  //   everything else -> private stack root
  const stackRoot = path.join(absoluteRoot, 'opt/bluedroid');
  fs.mkdirSync(stackRoot, { recursive: true, mode: 0o755 });
  cp.execFileSync('tar', ['-xzf', config.payload.path, '-C', stackRoot]);

  const halSource = path.join(stackRoot, 'system/lib');
  const halTarget = path.join(absoluteRoot, 'system/lib');
  if (!fs.existsSync(halSource))
    throw new Error('donor payload has no system/lib; the HAL loader would fail');
  fs.mkdirSync(halTarget, { recursive: true, mode: 0o755 });
  const halInstalled = [];
  for (const entry of fs.readdirSync(halSource, { withFileTypes: true })) {
    const from = path.join(halSource, entry.name);
    const to = path.join(halTarget, entry.name);
    if (fs.existsSync(to))
      throw new Error(`donor HAL would overwrite runtime file: system/lib/${entry.name}`);
    if (entry.isDirectory()) {
      fs.cpSync(from, to, { recursive: true });
    } else {
      fs.copyFileSync(from, to);
      fs.chmodSync(to, 0o755);
    }
    halInstalled.push(`system/lib/${entry.name}`);
  }
  // Remove the relocated copy so there is exactly one HAL tree and no doubt
  // about which one the loader used.
  fs.rmSync(halSource, { recursive: true, force: true });
  if (!fs.existsSync(path.join(halTarget, 'hw/bluetooth.default.so')))
    throw new Error('system/lib/hw/bluetooth.default.so is missing after install');

  // Same class of defect as the HAL, found the same way. The donor reads
  // /etc/bluetooth/bt_stack.conf by absolute path at startup. The payload
  // carries it as etc/bluetooth_orig/bt_stack.conf, renamed to avoid colliding
  // with BlueZ's main.conf and rfcomm.conf, and nothing ever put it back. With
  // the file absent config_new returns NULL and the first section lookup
  // dereferences it, which is the SIGSEGV observed in stack_manager on 05.4.
  // A real file, not a link: the stack reads it through its own loader and a
  // dangling link is indistinguishable from the missing file it replaces.
  const stackConfSource = path.join(stackRoot, 'etc/bluetooth_orig/bt_stack.conf');
  if (!fs.existsSync(stackConfSource))
    throw new Error('donor payload has no etc/bluetooth_orig/bt_stack.conf');
  const stackConfDir = path.join(absoluteRoot, 'etc/bluetooth');
  fs.mkdirSync(stackConfDir, { recursive: true, mode: 0o755 });
  const stackConfTarget = path.join(stackConfDir, 'bt_stack.conf');
  if (fs.existsSync(stackConfTarget))
    throw new Error('etc/bluetooth/bt_stack.conf already exists; refusing to overwrite');
  fs.copyFileSync(stackConfSource, stackConfTarget);
  fs.chmodSync(stackConfTarget, 0o644);

  const launcherTarget = path.join(stackRoot, 'start.sh');
  fs.copyFileSync(launcher, launcherTarget);
  fs.chmodSync(launcherTarget, 0o755);

  const identifiersTarget = path.join(absoluteRoot, 'bin/reinvoke-identifiers');
  fs.copyFileSync(config.identifiers.path, identifiersTarget);
  fs.chmodSync(identifiersTarget, 0o755);

  // Nothing else in this runtime issues HCIDEVUP. BlueZ is gone and the donor
  // stack assumes the adapter is already open, so the launcher runs this first.
  const hciUpTarget = path.join(absoluteRoot, 'bin/reinvoke-hci-up');
  fs.copyFileSync(config.hciUp.path, hciUpTarget);
  fs.chmodSync(hciUpTarget, 0o755);

  // The init reads these rather than embedding private values in a patch.
  // They live under the Bluedroid stack root, NOT etc/nand-pilot: the
  // bootstrap bind mounts its own immutable /etc/nand-pilot over the
  // runtime's copy before chroot, so anything written there is invisible at
  // runtime. Candidate 05 shipped them to etc/nand-pilot and the identity
  // provider consequently read an empty value and crash-looped on hardware.
  const settings = path.join(stackRoot, 'etc');
  fs.mkdirSync(settings, { recursive: true, mode: 0o755 });
  fs.writeFileSync(path.join(settings, 'identity-hex'), `${config.identityHex}\n`, { mode: 0o444 });
  fs.writeFileSync(path.join(settings, 'device-name'), `${config.deviceName}\n`, { mode: 0o444 });

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
