// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT
//
// The donor's voice output stage.
//
// Four of the five softvol devices in asound-product.conf are a plain control
// over a dmix slave. `voice` is not: it routes through a LADSPA equaliser
// first.
//
//   voice -> mbeq -> plugmbeq -> LADSPA mbeq id 1197 -> volmix_ladspa -> dmix
//
//   pcm.plugmbeq {
//       type ladspa
//       path "/usr/lib/ladspa"
//       plugins [{ label mbeq  id 1197
//           controls [0 0 -0.25 -0.5 0 0 0 0 0.5 0.5 0 0 0 0 0] }]
//   }
//
// Those fifteen numbers are a presence curve: a dip around 156 to 220 Hz and
// a lift around 1.25 to 1.75 kHz, which is the tuning the donor used for the
// speaker's own voice rather than for music.
//
// Without the plugin ALSA cannot instantiate plugmbeq, so opening `voice`
// fails with ENOENT. That is why the donor's own alsa-init.sh ends its device
// loop in `|| true`, and why this runtime's priming logs one failure and
// carries on.
//
// Everything here comes from the donor rootfs the build already pins, and
// each file is recorded by hash so a changed asset is visible in the manifest
// rather than silent. Nothing is compiled and nothing is fetched.
'use strict';
const fs = require('fs');
const path = require('path');
const { hashFile } = require('./build-lib');

// The plugin, and the one library it needs that this runtime does not
// already carry. librt, libc and libm are present. libfftw3f is 1.5 MB and
// is the whole cost of this feature.
//
// Both keep the donor's own paths. asound-product.conf is adopted verbatim
// and names an absolute `path "/usr/lib/ladspa"`, so the plugin goes there
// rather than the config being edited, and the library goes beside it where
// the donor had it.
//
// Splitting them was tried first, with the library in /opt/reinvoke/lib
// where the runtime's own objects live. The build refused it: elfClosure
// classifies an object by where it sits, and a plugin under /usr/lib is
// resolved against /lib and /usr/lib, not against the runtime's private
// directory. Keeping the donor's layout satisfies that check and the loader
// alike, since /usr/lib is searched after LD_LIBRARY_PATH either way.
const VOICE_FILES = [
  ['usr/lib/ladspa/mbeq_1197.so', 'usr/lib/ladspa/mbeq_1197.so'],
  ['usr/lib/libfftw3f.so.3.4.4', 'usr/lib/libfftw3f.so.3'],
];

function validateVoice(value) {
  if (value === undefined || value === null) return { enabled: false };
  if (typeof value !== 'object' || Array.isArray(value))
    throw new Error('voiceOutput must be an object');
  const enabled = value.enabled === true;
  for (const key of Object.keys(value)) {
    if (key !== 'enabled') throw new Error(`unknown voiceOutput key ${key}`);
  }
  return { enabled };
}

// installVoiceOutput places the LADSPA plugin and its library at the paths
// the donor used, which asound-product.conf names absolutely.
function installVoiceOutput(privateConfig, root, stockRoot) {
  const config = validateVoice(privateConfig.voiceOutput);
  if (!config.enabled) return { enabled: false };

  const installed = [];
  for (const [source, destination] of VOICE_FILES) {
    const from = path.join(stockRoot, source);
    if (!fs.existsSync(from))
      throw new Error(`voice output needs the donor ${source}`);
    const to = path.join(root, destination);
    fs.mkdirSync(path.dirname(to), { recursive: true, mode: 0o755 });
    fs.writeFileSync(to, fs.readFileSync(from), { mode: 0o644 });
    installed.push({
      path: destination,
      sha256: hashFile(to),
      bytes: fs.statSync(to).size,
    });
  }
  return { enabled: true, files: installed };
}

module.exports = { validateVoice, installVoiceOutput, VOICE_FILES };
