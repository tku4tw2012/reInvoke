// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT
'use strict';

const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const cp = require('node:child_process');
const test = require('node:test');
const { readVoiceInputs, buildVoiceRuntime, installVoiceSettings, validatePayload,
  installVoiceLights, VOICE_LIGHTS_ROOT, VOICE_LIGHTS,
  installVoiceCues, VOICE_CUES_ROOT, VOICE_CUES, VOICE_CONFIG_TARGET } =
  require('./voice-build');
const { patchRuntime } = require('../nand-pilot/patch-runtime');
const lib = require('../nand-pilot/build-lib');

let sequence = 0;
function fixture(t) {
  const root = path.join(__dirname, `.voice-build-test-${process.pid}-${sequence++}`);
  fs.mkdirSync(root, { mode: 0o700 });
  t.after(() => fs.rmSync(root, { force: true, recursive: true }));
  const write = (name, contents, mode = 0o644) => {
    const file = path.join(root, name);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, contents, { mode });
    return file;
  };
  return { root, write };
}

function armELF() {
  const data = Buffer.alloc(84);
  data.write('\x7fELF', 0, 'binary');
  data[4] = 1; data[5] = 1; data[6] = 1;
  data.writeUInt16LE(2, 16); data.writeUInt16LE(40, 18);
  data.writeUInt32LE(1, 20); data.writeUInt32LE(52, 28);
  data.writeUInt16LE(52, 40); data.writeUInt16LE(32, 42);
  data.writeUInt16LE(1, 44); data.writeUInt32LE(1, 52);
  return data;
}

// The base image ships /etc/hosts as a link to the RAM copy. The voice build
// verifies that link instead of creating one, so fixtures must provide it.
function baseImageHosts(root) {
  fs.mkdirSync(path.join(root, 'etc'), { recursive: true });
  const link = path.join(root, 'etc/hosts');
  fs.rmSync(link, { force: true });
  fs.symlinkSync('/etc/tmpfs/hosts', link);
  return root;
}

function payload(t) {
  const f = fixture(t);
  f.write('donor/bin/reinvoke-voice', armELF(), 0o755);
  f.write('donor/voice/bin/cortana', 'synthetic unchanged detector\n', 0o755);
  f.write('donor/voice/lib/ld-linux-armhf.so.3', 'synthetic loader\n', 0o755);
  f.write('donor/voice/lib/unit-link.so', 'synthetic owned shim\n');
  f.write('donor/voice/lib/libc.so.6', 'synthetic isolated libc\n');
  f.write('donor/voice/share/handoff-original.table', 'synthetic unchanged model\n');
  const config = f.write('private/voice.json', 'opaque synthetic private token\n', 0o600);
  const hosts = f.write('private/hosts', '127.0.0.1 localhost reinvoke-host\n');
  const builder = f.write('builder.sh', `#!/bin/sh
set -eu
test "$1" = --output-dir
test "$3" = --donor-bundle
test "\${GO111MODULE-unset}" = unset
test "\${GOFLAGS-unset}" = unset
test "\${GOWORK-unset}" = unset
mkdir "$2"
cp -R "$4/." "$2/"
`, 0o755);
  return { ...f, builder, inputs: { donorBundle: path.join(f.root, 'donor'), config, hosts } };
}

test('voice input pairing is explicit; config is opaque and private; hosts can stand alone', t => {
  const f = payload(t);
  assert.deepEqual(readVoiceInputs(), { donorBundle: '', config: '', hosts: '' });
  assert.throws(() => readVoiceInputs({ donorBundle: f.inputs.donorBundle }), /together/);
  assert.throws(() => readVoiceInputs({ config: f.inputs.config }), /together/);
  assert.deepEqual(readVoiceInputs(f.inputs), f.inputs);
  assert.equal(readVoiceInputs({ hosts: f.inputs.hosts }).hosts, f.inputs.hosts);
  fs.chmodSync(f.inputs.config, 0o644);
  assert.throws(() => readVoiceInputs(f.inputs), /0600/);
  fs.chmodSync(f.inputs.config, 0o600);
  const linked = path.join(f.root, 'linked-config');
  fs.symlinkSync(f.inputs.config, linked);
  assert.throws(() => readVoiceInputs({ ...f.inputs, config: linked }), /regular file/);
});

test('only the three original voice animations are selected, and bad inputs install nothing', t => {
  const f = fixture(t);
  assert.deepEqual(VOICE_LIGHTS.map(([name]) => name), [
    'L_101_c_listening.bin', 'L_104_c_thinking.bin', 'L_105_c_cortanaspeaking.bin',
  ]);
  for (const [name] of VOICE_LIGHTS)
    f.write(`archive/${VOICE_LIGHTS_ROOT}/${name}`, 'synthetic corrupt animation\n');
  const existing = f.write('runtime/share/lights/existing.bin', 'unchanged normal animation\n');
  assert.throws(() => installVoiceLights(path.join(f.root, 'archive'),
    path.join(f.root, 'runtime')), /voice light checksum mismatch/);
  assert.deepEqual(fs.readdirSync(path.dirname(existing)), ['existing.bin']);
  assert.equal(fs.readFileSync(existing, 'utf8'), 'unchanged normal animation\n');
  fs.unlinkSync(path.join(f.root, 'archive', VOICE_LIGHTS_ROOT, VOICE_LIGHTS[0][0]));
  assert.throws(() => installVoiceLights(path.join(f.root, 'archive'),
    path.join(f.root, 'runtime')), /ENOENT/);
});

test('only original listening and processing cues are selected; corrupt input changes nothing', t => {
  const f = fixture(t);
  assert.deepEqual(VOICE_CUES.map(([name]) => name), ['listening.wav', 'processing.wav']);
  for (const [, , sourceName] of VOICE_CUES)
    f.write(`archive/${VOICE_CUES_ROOT}/${sourceName}`, 'corrupt cue\n');
  const existing = f.write('runtime/share/cues/Power_On.wav', 'existing boot chime\n');
  assert.throws(() => installVoiceCues(path.join(f.root, 'archive'),
    path.join(f.root, 'runtime')), /voice cue checksum mismatch/);
  assert.deepEqual(fs.readdirSync(path.dirname(existing)), ['Power_On.wav']);
  assert.equal(fs.readFileSync(existing, 'utf8'), 'existing boot chime\n');
  fs.unlinkSync(path.join(f.root, 'archive', VOICE_CUES_ROOT, VOICE_CUES[0][2]));
  assert.throws(() => installVoiceCues(path.join(f.root, 'archive'),
    path.join(f.root, 'runtime')), /ENOENT/);
});

test('composition adds only the isolated voice subtree, settings and helper', t => {
  const f = payload(t);
  const runtime = path.join(f.root, 'runtime/opt/reinvoke');
  const image = path.join(f.root, 'runtime');
  f.write('runtime/opt/reinvoke/lib/libc.so.6', 'existing libc\n');
  f.write('runtime/opt/reinvoke/bin/reinvoke-mic-capture', 'existing capture owner\n', 0o755);
  buildVoiceRuntime(f.inputs, runtime, f.builder);
  baseImageHosts(image);
  installVoiceSettings(runtime, image);
  assert.equal(fs.readFileSync(path.join(runtime, 'lib/libc.so.6'), 'utf8'), 'existing libc\n');
  assert.equal(fs.readFileSync(path.join(runtime, 'bin/reinvoke-mic-capture'), 'utf8'),
    'existing capture owner\n');
  for (const name of ['voice/bin/cortana', 'voice/share/handoff-original.table'])
    assert.deepEqual(fs.readFileSync(path.join(runtime, name)),
      fs.readFileSync(path.join(f.inputs.donorBundle, name)));
  assert.deepEqual(fs.readFileSync(path.join(image, 'etc/reinvoke-voice/voice.json')),
    fs.readFileSync(f.inputs.config));
  for (const file of [path.join(runtime, 'etc/voice.json'), path.join(image, 'etc/reinvoke-voice/voice.json')])
    assert.equal(fs.statSync(file).mode & 0o777, 0o600);
  assert.equal(fs.statSync(path.join(runtime, 'bin/reinvoke-voice')).mode & 0o777, 0o755);
  assert.equal(fs.readlinkSync(path.join(image, 'etc/hosts')), '/etc/tmpfs/hosts');
  assert.deepEqual(fs.readFileSync(path.join(image, 'etc/tmpfs/hosts')), fs.readFileSync(f.inputs.hosts));
  assert.equal(fs.existsSync(`${runtime}.voice-build`), false);
  assert.equal(fs.existsSync(path.join(image, 'persist')), false);
});

test('the installed configuration is not masked by the runtime bind mounts', t => {
  // The regression this exists for: 2.3.0's first flash shipped the config to
  // etc/reinvoke, the bootstrap bind mounts an immutable /etc/reinvoke over
  // the runtime copy, and startup recorded "voice configuration is missing".
  // bluedroid-config.js records the same trap from candidate 05.
  const masked = ['etc/reinvoke/', 'etc/reinvoke-release/'];
  for (const prefix of masked)
    assert(!VOICE_CONFIG_TARGET.startsWith(prefix),
      `${VOICE_CONFIG_TARGET} is masked by the read-only ${prefix} mount`);
  assert.match(VOICE_CONFIG_TARGET, /^etc\//, 'the configuration must live under /etc');
  assert(!VOICE_CONFIG_TARGET.includes('tmpfs'), 'it must not depend on a RAM path');

  // And it is a real file at that path, not a link into /opt.
  const f = payload(t);
  const root = path.join(f.root, 'image');
  buildVoiceRuntime(f.inputs, path.join(root, 'opt/reinvoke'), f.builder);
  baseImageHosts(root);
  installVoiceSettings(path.join(root, 'opt/reinvoke'), root);
  const installed = path.join(root, VOICE_CONFIG_TARGET);
  const info = fs.lstatSync(installed);
  assert(info.isFile() && !info.isSymbolicLink(), 'the configuration must be a regular file');
  assert.equal(info.mode & 0o777, 0o600);
  assert.equal(fs.statSync(path.dirname(installed)).mode & 0o777, 0o700);
  assert.deepEqual(fs.readFileSync(installed), fs.readFileSync(f.inputs.config));

  // The startup helper must read that same path, or the file is unreachable.
  const startup = fs.readFileSync(path.join(__dirname, 'voice-start.sh'), 'utf8');
  assert(startup.includes(`/${VOICE_CONFIG_TARGET}`),
    'voice-start.sh does not read the installed configuration path');
});

test('the base image hosts link is verified, never replaced', t => {
  const f = payload(t);
  const root = path.join(f.root, 'image');
  buildVoiceRuntime(f.inputs, path.join(root, 'opt/reinvoke'), f.builder);
  fs.mkdirSync(path.join(root, 'etc'), { recursive: true });
  fs.writeFileSync(path.join(root, 'etc/hosts'), 'a regular file, not the base link\n');
  assert.throws(() => installVoiceSettings(path.join(root, 'opt/reinvoke'), root),
    /must already link to/);
  fs.rmSync(path.join(root, 'etc/hosts'));
  fs.symlinkSync('/somewhere/else', path.join(root, 'etc/hosts'));
  assert.throws(() => installVoiceSettings(path.join(root, 'opt/reinvoke'), root),
    /must already link to/);
});

test('absent options change nothing; hosts-only packages never acquire voice', t => {
  const f = payload(t);
  const runtime = path.join(f.root, 'bare/opt/reinvoke');
  buildVoiceRuntime(readVoiceInputs(), runtime, f.builder);
  installVoiceSettings(runtime, path.join(f.root, 'bare'));
  assert.equal(fs.existsSync(path.join(f.root, 'bare')), false);
  buildVoiceRuntime(readVoiceInputs({ hosts: f.inputs.hosts }), runtime, f.builder);
  baseImageHosts(path.join(f.root, 'bare'));
  installVoiceSettings(runtime, path.join(f.root, 'bare'));
  assert.equal(fs.existsSync(path.join(runtime, 'bin/reinvoke-voice')), false);
  assert.equal(fs.existsSync(path.join(f.root, 'bare/etc/reinvoke-voice/voice.json')), false);
});

test('malformed or incomplete voice payloads and stale build paths fail closed', t => {
  const f = payload(t);
  const runtime = path.join(f.root, 'runtime');
  const exe = path.join(f.inputs.donorBundle, 'bin/reinvoke-voice');
  const bad = armELF();
  bad.writeUInt32LE(3, 52);
  fs.writeFileSync(exe, bad);
  assert.throws(() => buildVoiceRuntime(f.inputs, runtime, f.builder), /dynamic loader/);
  assert.equal(fs.existsSync(`${runtime}.voice-build`), false);
  fs.writeFileSync(exe, armELF());
  fs.mkdirSync(`${runtime}.voice-build`);
  assert.throws(() => buildVoiceRuntime(f.inputs, runtime, f.builder), /stale/);
  fs.rmdirSync(`${runtime}.voice-build`);
  fs.unlinkSync(path.join(f.inputs.donorBundle, 'voice/lib/unit-link.so'));
  assert.throws(() => buildVoiceRuntime(f.inputs, runtime, f.builder), /ENOENT/);
  f.write('runtime/etc/voice.json', 'synthetic\n', 0o600);
  assert.throws(() => installVoiceSettings(runtime, path.join(f.root, 'image')), /incomplete/);
});

test('payload symlinks and non-executable detector/loader are rejected', t => {
  const f = payload(t);
  fs.chmodSync(path.join(f.inputs.donorBundle, 'voice/bin/cortana'), 0o644);
  assert.throws(() => validatePayload(f.inputs.donorBundle), /executable/);
  fs.chmodSync(path.join(f.inputs.donorBundle, 'voice/bin/cortana'), 0o755);
  fs.symlinkSync('/lib/libc.so.6', path.join(f.inputs.donorBundle, 'voice/lib/escape.so'));
  assert.throws(() => buildVoiceRuntime(f.inputs, path.join(f.root, 'runtime'), f.builder),
    /only regular files/);
});

function runStartup(root, command, disabled = '', runner) {
  const startup = fs.readFileSync(path.join(__dirname, 'voice-start.sh'), 'utf8')
    .replaceAll('/opt/reinvoke', `${root}/opt/reinvoke`)
    .replaceAll('/etc/', `${root}/etc/`)
    .replaceAll('/persist/', `${root}/persist/`);
  const script = `
BB="${runner ? `${runner.qemu} ${runner.busybox}` : ''}"
log() { printf 'log:%s\\n' "$*"; }
cmdline_has() { test "$1" = "${disabled}"; }
supervise() { printf 'supervise:%s\\n' "$*"; }
${startup}
${command}
`;
  return runner ?
    cp.execFileSync(runner.qemu, [runner.busybox, 'sh', '-eu', '-c', script], { encoding: 'utf8' }) :
    cp.execFileSync('bash', ['-eu', '-c', script], { encoding: 'utf8' });
}

test('startup supervises only configured voice; disabled owners and incomplete bundles skip it', t => {
  const f = payload(t);
  const root = path.join(f.root, 'image');
  buildVoiceRuntime(f.inputs, path.join(root, 'opt/reinvoke'), f.builder);
  baseImageHosts(root);
  installVoiceSettings(path.join(root, 'opt/reinvoke'), root);
  const output = runStartup(root, 'start_voice_endpoint');
  assert.match(output, /supervise:voice-endpoint .*\/bin\/reinvoke-voice --config .*\/etc\/reinvoke-voice\/voice\.json/);
  assert.match(output, /--bundle .*\/opt\/reinvoke\/voice --router 127\.0\.0\.1:9999 --realm default/);
  assert(!output.includes('opaque synthetic'));
  for (const owner of ['voice', 'router', 'mcu', 'dsp', 'mic_capture'])
    assert(!runStartup(root, 'start_voice_endpoint', `reinvoke.${owner}=off`).includes('supervise:'));
  fs.unlinkSync(path.join(root, 'opt/reinvoke/voice/lib/unit-link.so'));
  assert.match(runStartup(root, 'start_voice_endpoint'), /incomplete/);
  fs.unlinkSync(path.join(root, 'etc/reinvoke-voice/voice.json'));
  assert.match(runStartup(root, 'start_voice_endpoint'), /configuration is missing/);
  fs.unlinkSync(path.join(root, 'opt/reinvoke/bin/reinvoke-voice'));
  assert.equal(runStartup(root, 'start_voice_endpoint'), '');
});

test('hosts selection writes only RAM, retains the seed without a regular override', t => {
  const f = payload(t);
  const root = path.join(f.root, 'image');
  buildVoiceRuntime(f.inputs, path.join(root, 'opt/reinvoke'), f.builder);
  baseImageHosts(root);
  installVoiceSettings(path.join(root, 'opt/reinvoke'), root);
  assert.equal(runStartup(root, 'configure_voice_hosts'), '');
  const override = f.write('image/persist/hosts', '# existing private host mapping\n', 0o600);
  const before = fs.statSync(override);
  assert.match(runStartup(root, 'configure_voice_hosts'), /copied to RAM/);
  assert.equal(fs.readFileSync(path.join(root, 'etc/tmpfs/hosts'), 'utf8'),
    '# existing private host mapping\n');
  const after = fs.statSync(override);
  assert.equal(after.mode, before.mode);
  assert.equal(after.mtimeMs, before.mtimeMs);
  assert.equal(fs.statSync(path.join(root, 'etc/tmpfs/hosts')).mode & 0o777, 0o644);
  fs.unlinkSync(override);
  fs.symlinkSync(f.inputs.hosts, override);
  assert.match(runStartup(root, 'configure_voice_hosts'), /override invalid/);
  assert.equal(fs.readFileSync(path.join(root, 'etc/tmpfs/hosts'), 'utf8'),
    '# existing private host mapping\n');
});

test('the reviewed ARM BusyBox runs the same configured startup and RAM-only hosts hook', t => {
  const f = payload(t);
  const archive = process.env.REINVOKE_ARCHIVE || path.resolve(__dirname, '../../../reinvoke-archive');
  const rc12 = path.join(archive, lib.pins.rc12.path);
  lib.verify(rc12, lib.pins.rc12);
  const binary = cp.execFileSync('bash', ['-o', 'pipefail', '-c',
    'gzip -dc "$1" | cpio -i --to-stdout --quiet bin/busybox', 'fixture', rc12],
  { maxBuffer: 8 * 1024 * 1024 });
  assert.equal(lib.sha(binary), lib.BB_SHA256);
  const runner = { qemu: path.join(archive, 'emulation/qemu-arm-static'),
    busybox: f.write('busybox', binary, 0o755) };
  const root = path.join(f.root, 'image');
  buildVoiceRuntime(f.inputs, path.join(root, 'opt/reinvoke'), f.builder);
  baseImageHosts(root);
  installVoiceSettings(path.join(root, 'opt/reinvoke'), root);
  f.write('image/persist/hosts', '# existing host mapping\n', 0o600);
  const output = runStartup(root, 'configure_voice_hosts; start_voice_endpoint', '', runner);
  assert.match(output, /hosts override copied to RAM/);
  assert.match(output, /supervise:voice-endpoint/);
  assert.equal(fs.readFileSync(path.join(root, 'etc/tmpfs/hosts'), 'utf8'), '# existing host mapping\n');
});

test('NAND patches preserve the baseline by default; voice stops before its owners', () => {
  const archive = process.env.REINVOKE_ARCHIVE || path.resolve(__dirname, '../../../reinvoke-archive');
  const rc12 = path.join(archive, lib.pins.rc12.path);
  lib.verify(rc12, lib.pins.rc12);
  const source = cp.execFileSync('bash', ['-o', 'pipefail', '-c',
    'gzip -dc "$1" | cpio -i --to-stdout --quiet init', 'fixture', rc12]);
  const baseline = patchRuntime(source);
  assert(!baseline.includes('voice-start.sh'));
  const enabled = patchRuntime(source, { voice: true });
  const hostOnly = patchRuntime(source, { hosts: true });
  assert(hostOnly.includes('configure_voice_hosts'));
  assert(!hostOnly.includes('start_voice_endpoint'));
  assert(!hostOnly.includes('stop_service voice-endpoint'));
  assert(enabled.indexOf('pilot_persistence_start ||') < enabled.indexOf('  configure_voice_hosts\n'));
  assert(enabled.indexOf('supervise mic-capture') < enabled.indexOf('  start_voice_endpoint\n'));
  assert(enabled.indexOf('wait_service_stop voice-endpoint') < enabled.indexOf('stop_service mcu-interface'));
  assert(enabled.includes('stop_service persistence'));
  const native = fs.readFileSync(path.join(__dirname, 'native-ram-init'), 'utf8');
  assert(native.indexOf('wait_service_stop voice-endpoint') < native.indexOf('stop_service mcu-interface'));
  assert(native.indexOf('supervise mic-capture') < native.indexOf('    start_voice_endpoint\n'));
  cp.execFileSync('sh', ['-n'], { input: enabled });
});

test('candidate identity override does not relabel the default installed build', () => {
  const file = path.resolve(__dirname, '../nand-pilot/build-lib.js');
  const run = id => cp.spawnSync(process.execPath, ['-e',
    'const b=require(process.argv[1]);console.log(b.BUILD_ID,b.CANDIDATE,b.BUNDLE_NAME)', file],
  { encoding: 'utf8', env: { ...process.env, PILOT_BUILD_ID: id } });
  assert.match(run('').stdout, /reInvoke-2\.2\.11-20260921 2\.2\.11 83_IMAGE\.reinvoke-2\.2\.11/);
  assert.match(run('reInvoke-2.3.0-20260928').stdout, /reInvoke-2\.3\.0-20260928 2\.3\.0 83_IMAGE\.reinvoke-2\.3\.0/);
  for (const id of ['reInvoke-2.3.0-20260230', 'reInvoke-2.3.0-20261301', 'accepted-2.3.0', 'reInvoke-02.3.0-20260928'])
    assert.notEqual(run(id).status, 0);
});

test('voice ELF dependencies resolve only against their isolated library family', t => {
  const f = fixture(t);
  f.write('opt/reinvoke/voice/bin/cortana', armELF());
  const privateLib = f.write('opt/reinvoke/voice/lib/libboundary.so', 'voice library\n');
  f.write('opt/reinvoke/lib/libboundary.so', 'wrong runtime library\n');
  const original = cp.execFileSync;
  cp.execFileSync = (command, args, options) => command === 'readelf' ?
    ' 0x00000001 (NEEDED) Shared library: [libboundary.so]\n' :
    original(command, args, options);
  try {
    const entry = lib.elfClosure(f.root)[0];
    assert.deepEqual(entry.loaderSearch, ['/opt/reinvoke/voice/lib']);
    assert.equal(entry.dependencies[0].resolved, 'opt/reinvoke/voice/lib/libboundary.so');
    fs.unlinkSync(privateLib);
    assert.throws(() => lib.elfClosure(f.root), /missing ELF dependency libboundary/);
  } finally {
    cp.execFileSync = original;
  }
});

test('runtime CLI rejects partial voice options before building any baseline artifacts', t => {
  const f = payload(t);
  const script = path.join(__dirname, 'build-native-runtime.sh');
  for (const args of [['--voice-config', f.inputs.config],
    ['--voice-donor-bundle', f.inputs.donorBundle]]) {
    const result = cp.spawnSync('bash', [script, ...args], { encoding: 'utf8' });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /must be supplied together/);
    assert(!result.stderr.includes('opaque synthetic'));
  }
});
