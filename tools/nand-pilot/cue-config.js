// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT
//
// Device cues: the short sounds the speaker makes about itself, taken from the
// donor image.
//
// The donor played these from audio-ui through its own alert player. Its asset
// table pairs each one with the state that triggers it, and those pairings are
// what this runtime reproduces. Only the device family is installed: the
// Cortana and telephony cues have no trigger here.
//
// The renderer is the donor's own aplay, because the music PCM this runtime
// otherwise uses is created by the donor stack and only exists while that
// stack runs. A boot cue has to play before that.
'use strict';
const fs = require('fs');
const path = require('path');
const { hashFile } = require('./build-lib');

// The device cue family, with the donor state each one accompanies.
const DEVICE_CUES = [
  ['S_311_d_pluggedin', 'system:booting'],
  ['S_301_d_micon', 'microphone:unmute'],
  ['S_301_d_micoff', 'microphone:mute'],
  ['S_302_d_wifisetup', 'system:wifi-setup'],
  ['S_304_d_exitwifisetup', 'system:wifi-setup ends'],
  ['S_305_d_volumeup', 'volume up'],
  ['S_306_d_volumedown', 'volume down'],
  ['S_307_d_btpairing', 'bluetooth:pairing'],
  ['S_308_d_btconnected', 'bluetooth:connected'],
  ['S_309_d_pinreset', 'factory reset'],
];

const CUE_SOURCE = 'usr/share/sounds/cortana';
const RENDERER_SOURCE = 'usr/bin/aplay';

function validateCues(value) {
  if (value === undefined) return { enabled: false };
  if (value === null || typeof value !== 'object' || Array.isArray(value))
    throw new Error('deviceCues must be an object');
  if (Object.keys(value).some(key => key !== 'enabled'))
    throw new Error('unknown deviceCues setting');
  if (value.enabled !== true && value.enabled !== false)
    throw new Error('deviceCues.enabled must be boolean');
  return { enabled: value.enabled };
}

// installCues copies the renderer and the device cue family into the runtime.
//
// Everything comes from the donor rootfs, which the build already pins as a
// whole, and each file is recorded by hash so a changed asset is visible in
// the manifest rather than silent.
function installCues(privateConfig, root, stockRoot) {
  const config = validateCues(privateConfig.deviceCues);
  if (!config.enabled) return { enabled: false };

  const renderer = path.join(stockRoot, RENDERER_SOURCE);
  if (!fs.existsSync(renderer))
    throw new Error('device cues need the donor aplay');
  const rendererTarget = path.join(root, 'opt/reinvoke/bin/aplay');
  fs.mkdirSync(path.dirname(rendererTarget), { recursive: true });
  fs.writeFileSync(rendererTarget, fs.readFileSync(renderer), { mode: 0o755 });
  fs.chmodSync(rendererTarget, 0o755);

  const target = path.join(root, 'opt/reinvoke/share/cues');
  fs.mkdirSync(target, { recursive: true, mode: 0o755 });
  const installed = [];
  for (const [name] of DEVICE_CUES) {
    const from = path.join(stockRoot, CUE_SOURCE, name + '.wav');
    if (!fs.existsSync(from)) throw new Error(`device cue ${name} is missing`);
    const to = path.join(target, name + '.wav');
    fs.writeFileSync(to, fs.readFileSync(from), { mode: 0o644 });
    installed.push({ name, sha256: hashFile(to), bytes: fs.statSync(to).size });
  }
  return {
    enabled: true,
    renderer: hashFile(rendererTarget),
    cues: installed,
  };
}

module.exports = { validateCues, installCues, DEVICE_CUES };
