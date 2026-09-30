// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT
'use strict';

const fs = require('node:fs');
const path = require('node:path');
const cp = require('node:child_process');
const crypto = require('node:crypto');

const VOICE_LIGHTS_ROOT = 'extracted/phase3/stockroot/rootfs/usr/share/lights';
const VOICE_LIGHTS = Object.freeze([
  ['L_101_c_listening.bin', '70c1d5aed13d730864d8dbc61cc7e066937a249a732b642072969004042990a0'],
  ['L_104_c_thinking.bin', '17ad3d8d31bf5160b6b5eccc9020be3aadbd89913e8de52043aeeb113bb2e15a'],
  ['L_105_c_cortanaspeaking.bin', '2efffad28d87ac56a240c4c81f9be5cfa9f2b4759e64a1a9feaa3c76ff71d8c4'],
]);
// The installed configuration path.
//
// NOT etc/reinvoke: the bootstrap bind mounts its own immutable /etc/reinvoke
// over the runtime copy before chroot, so anything written there is invisible
// at runtime. bluedroid-config.js records the same trap after candidate 05
// crash-looped on it, and the first 2.3.0 flash repeated it: the packaged
// configuration was masked and startup recorded "voice configuration is
// missing; voice remains stopped".
//
// This is a real file in its own /etc directory, matching the
// etc/reinvoke-wifi/station-seed.json that is read successfully on hardware.
// No symlink is involved.
const VOICE_CONFIG_TARGET = 'etc/reinvoke-voice/voice.json';

// The base image already ships /etc/hosts as a link to this RAM file, because
// the runtime must be able to rewrite hosts. This build seeds that file; it
// does not create or replace the link.
const RAM_HOSTS_LINK = '/etc/tmpfs/hosts';

const VOICE_CUES_ROOT = 'extracted/phase3/stockroot/rootfs/usr/share/sounds/cortana';
const VOICE_CUES = Object.freeze([
  ['listening.wav', 'a52622b5b3c2f31a8d66c58a20c5c343b026c565e36e02d6e392eea77be13f10', 'S_101_c_listening.wav'],
  ['processing.wav', 'da6705e903468e5e018dc9aebb46c8c84469c5a8ada8cc0f3396285c5cbfed8f', 'S_104_c_thinking.wav'],
]);

function regularFile(file, label, privateMode = false) {
  const info = fs.lstatSync(file);
  if (!info.isFile() || info.isSymbolicLink() || info.size === 0)
    throw new Error(`${label} must be a nonempty regular file`);
  if (privateMode && (info.mode & 0o777) !== 0o600)
    throw new Error(`${label} must use mode 0600`);
  return info;
}

function readVoiceInputs({ donorBundle = '', config = '', hosts = '' } = {}) {
  if (Boolean(donorBundle) !== Boolean(config))
    throw new Error('voice donor bundle and private voice config must be supplied together');
  if (donorBundle) {
    if (!fs.statSync(donorBundle).isDirectory())
      throw new Error('voice donor bundle must be a directory');
    regularFile(config, 'private voice config', true);
  }
  if (hosts) regularFile(hosts, 'hosts seed');
  return { donorBundle, config, hosts };
}

function voiceInputsFromEnvironment(env = process.env) {
  return readVoiceInputs({
    donorBundle: env.PILOT_VOICE_DONOR_BUNDLE,
    config: env.PILOT_VOICE_CONFIG,
    hosts: env.PILOT_HOSTS_FILE,
  });
}

function installFile(source, target, mode) {
  fs.mkdirSync(path.dirname(target), { recursive: true, mode: 0o755 });
  fs.rmSync(target, { force: true });
  fs.copyFileSync(source, target);
  fs.chmodSync(target, mode);
}

function installVoiceAssetSet(archive, runtime, sourceRoot, targetDirectory, assets, label) {
  const files = assets.map(([name, sha256, sourceName = name]) => {
    const source = path.join(archive, sourceRoot, sourceName);
    regularFile(source, `${label} ${name}`);
    const data = fs.readFileSync(source);
    if (crypto.createHash('sha256').update(data).digest('hex') !== sha256)
      throw new Error(`${label} checksum mismatch: ${name}`);
    return { name, data, sha256 };
  });
  const target = path.join(runtime, targetDirectory);
  fs.mkdirSync(target, { recursive: true, mode: 0o755 });
  return files.map(({ name, data, sha256 }) => {
    const file = path.join(target, name);
    fs.rmSync(file, { force: true });
    fs.writeFileSync(file, data, { mode: 0o644, flag: 'wx' });
    return { path: `${targetDirectory}/${name}`, bytes: data.length, sha256 };
  });
}

function installVoiceLights(archive, runtime) {
  return installVoiceAssetSet(archive, runtime, VOICE_LIGHTS_ROOT, 'share/lights',
    VOICE_LIGHTS, 'voice light');
}

function installVoiceCues(archive, runtime) {
  return installVoiceAssetSet(archive, runtime, VOICE_CUES_ROOT, 'share/cues',
    VOICE_CUES, 'voice cue');
}

function validatePayload(root) {
  for (const name of ['bin/reinvoke-voice', 'voice/bin/cortana',
    'voice/lib/ld-linux-armhf.so.3', 'voice/lib/unit-link.so',
    'voice/share/handoff-original.table'])
    regularFile(path.join(root, name), `voice payload ${name}`);
  for (const name of ['voice/bin/cortana', 'voice/lib/ld-linux-armhf.so.3'])
    if (!(fs.statSync(path.join(root, name)).mode & 0o111))
      throw new Error(`voice payload ${name} must be executable`);
  const data = fs.readFileSync(path.join(root, 'bin/reinvoke-voice'));
  if (data.length < 52 || data.subarray(0, 4).toString('hex') !== '7f454c46' ||
      data[4] !== 1 || data[5] !== 1 || data.readUInt16LE(16) !== 2 ||
      data.readUInt16LE(18) !== 40)
    throw new Error('voice supervisor must be a static ARM32 executable');
  const offset = data.readUInt32LE(28), size = data.readUInt16LE(42);
  const count = data.readUInt16LE(44);
  if (offset < 52 || size !== 32 || count < 1 || count > 64 ||
      offset + size * count > data.length)
    throw new Error('invalid voice supervisor program headers');
  for (let index = 0; index < count; index++) {
    const type = data.readUInt32LE(offset + index * size);
    if (type === 2 || type === 3)
      throw new Error('voice supervisor must not need a dynamic loader');
  }
}

function copyVoiceTree(source, target) {
  const info = fs.lstatSync(source);
  if (info.isDirectory()) {
    fs.mkdirSync(target, { mode: 0o755 });
    for (const name of fs.readdirSync(source).sort())
      copyVoiceTree(path.join(source, name), path.join(target, name));
  } else if (info.isFile()) {
    installFile(source, target, info.mode & 0o111 ? 0o755 : 0o644);
  } else {
    throw new Error('voice payload may contain only regular files and directories');
  }
}

function buildVoiceRuntime(inputs, runtime, builder = path.join(__dirname, '../voice-endpoint/build.sh')) {
  readVoiceInputs(inputs);
  if (!inputs.donorBundle && !inputs.hosts) return;
  fs.mkdirSync(path.join(runtime, 'etc'), { recursive: true, mode: 0o755 });
  if (inputs.donorBundle) {
    const work = `${runtime}.voice-build`;
    if (fs.existsSync(work)) throw new Error('stale voice build output exists');
    try {
      const env = { ...process.env };
      for (const name of ['GO111MODULE', 'GOFLAGS', 'GOWORK']) delete env[name];
      cp.execFileSync('bash', [builder, '--output-dir', work, '--donor-bundle',
        path.resolve(inputs.donorBundle)], { stdio: 'inherit', env });
      validatePayload(work);
      installFile(path.join(work, 'bin/reinvoke-voice'),
        path.join(runtime, 'bin/reinvoke-voice'), 0o755);
      copyVoiceTree(path.join(work, 'voice'), path.join(runtime, 'voice'));
      installFile(inputs.config, path.join(runtime, 'etc/voice.json'), 0o600);
    } finally {
      fs.rmSync(work, { recursive: true, force: true });
    }
  }
  if (inputs.hosts)
    installFile(inputs.hosts, path.join(runtime, 'etc/hosts'), 0o644);
  installFile(path.join(__dirname, 'voice-start.sh'),
    path.join(runtime, 'etc/voice-start.sh'), 0o644);
}

function installVoiceSettings(runtime, root) {
  const config = path.join(runtime, 'etc/voice.json');
  const binary = path.join(runtime, 'bin/reinvoke-voice');
  const voice = path.join(runtime, 'voice');
  const hosts = path.join(runtime, 'etc/hosts');
  const enabled = fs.existsSync(config);
  if (enabled !== fs.existsSync(binary) || enabled !== fs.existsSync(voice))
    throw new Error('incomplete configured voice runtime');
  if (!enabled && !fs.existsSync(hosts)) return;
  regularFile(path.join(runtime, 'etc/voice-start.sh'), 'voice startup helper');
  if (enabled) {
    validatePayload(runtime);
    regularFile(config, 'private voice config', true);
    const target = path.join(root, VOICE_CONFIG_TARGET);
    fs.mkdirSync(path.dirname(target), { recursive: true, mode: 0o700 });
    fs.chmodSync(path.dirname(target), 0o700);
    installFile(config, target, 0o600);
  }
  const ramEtc = path.join(root, 'etc/tmpfs');
  fs.mkdirSync(ramEtc, { recursive: true, mode: 0o755 });
  if (!fs.lstatSync(ramEtc).isDirectory())
    throw new Error('/etc/tmpfs must be a RAM runtime directory');
  if (fs.existsSync(hosts)) {
    regularFile(hosts, 'hosts seed');
    installFile(hosts, path.join(ramEtc, 'hosts'), 0o644);
  } else if (!fs.existsSync(path.join(ramEtc, 'hosts'))) {
    fs.writeFileSync(path.join(ramEtc, 'hosts'),
      '127.0.0.1 localhost\n::1 localhost\n', { mode: 0o644 });
  }
  // Verify the base image's link rather than replacing it. Creating our own
  // here would silently paper over a changed base image instead of failing.
  // existsSync follows the link and would report false while /etc/tmpfs/hosts
  // is still absent, so inspect the link itself.
  const link = path.join(root, 'etc/hosts');
  let info = null;
  try {
    info = fs.lstatSync(link);
  } catch {
    throw new Error(`/etc/hosts is missing; it must link to ${RAM_HOSTS_LINK}`);
  }
  if (!info.isSymbolicLink() || fs.readlinkSync(link) !== RAM_HOSTS_LINK)
    throw new Error(`/etc/hosts must already link to ${RAM_HOSTS_LINK}`);
}

if (require.main === module) {
  try {
    const [operation, ...args] = process.argv.slice(2);
    if (operation === 'install' && args.length === 2)
      installVoiceSettings(...args);
    else if (operation === 'lights' && args.length === 2)
      installVoiceLights(...args);
    else if (operation === 'cues' && args.length === 2)
      installVoiceCues(...args);
    else if ((operation === 'build' && args.length === 4) ||
             (operation === 'validate' && args.length === 3)) {
      const inputs = readVoiceInputs({ donorBundle: args[0], config: args[1], hosts: args[2] });
      if (operation === 'build') buildVoiceRuntime(inputs, args[3]);
    } else {
      throw new Error('usage: voice-build.js validate DONOR CONFIG HOSTS | build DONOR CONFIG HOSTS RUNTIME | install RUNTIME ROOT | lights|cues ARCHIVE RUNTIME');
    }
  } catch (error) {
    console.error(`ERROR: ${error.message}`);
    process.exitCode = 1;
  }
}

module.exports = { readVoiceInputs, voiceInputsFromEnvironment, buildVoiceRuntime,
  installVoiceSettings, validatePayload, installVoiceLights, VOICE_LIGHTS_ROOT, VOICE_LIGHTS,
  installVoiceCues, VOICE_CUES_ROOT, VOICE_CUES, VOICE_CONFIG_TARGET };
