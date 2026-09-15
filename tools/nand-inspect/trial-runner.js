// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
'use strict';

const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const { spawn, spawnSync } = require('child_process');

const pins = Object.freeze({
  'UPDATE-PLAN.json': '3e1952eeda2c50167ddba35d7ef76d45f89c2e7b707c04cf7de54409bb6f6908',
  'target-blocks-data-oob32.bin': '3af718605f1989acfc65e72e8506d17750a53297c528b91ee794b02ff0e07507',
  'nand-stockroot-update-armv7': '522658e3b5bd57daf40c678ce334000eb95530dd09d2329755a973566b9e9054',
  'nand-stockroot-update-host': 'bc2e72f05d9cf9ef2aad4ad33ac98d402b2cd6878578fce0292b7ac754f00de3',
});
const ack = 'APPLY-STOCKROOT-NATIVE-01';
const usb = '3-1.2';
// The existing native init mounts /dev as tmpfs; /run is on the initramfs root.
const ramRoot = '/dev';
const banner = 'Linux version 3.8.13-reinvoke-audio-sd8887 (reinvoke@reinvoke) (gcc version 4.9 20140827 (prerelease) (GCC) ) #1-mtd-cleanup SMP PREEMPT Thu Jan 1 00:00:00 UTC 1970';
const inputs = Object.keys(pins).slice(0, 3);
const adbBase = ['-H', '127.0.0.1', '-P', '5037'];
const quote = value => `'${String(value).replace(/'/g, "'\\''")}'`;
const requireThat = (condition, message) => { if (!condition) throw new Error(message); };

function execute(file, args) {
  return spawnSync(file, args, { encoding: 'utf8', maxBuffer: 32 * 1024 * 1024 });
}

function checked(result, description) {
  requireThat(!result.error && result.status === 0 && !result.signal,
    `${description} failed: ${result.error || result.signal || result.status}; ${result.stderr || ''}`);
  return result.stdout;
}

function verifyFile(file, expected) {
  requireThat(fs.lstatSync(file).isFile(), `not a regular, non-symlink input: ${file}`);
  const fd = fs.openSync(file, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
  try {
    const hash = crypto.createHash('sha256');
    const buffer = Buffer.alloc(1024 * 1024);
    let size = 0;
    let n;
    while ((n = fs.readSync(fd, buffer, 0, buffer.length, null)) !== 0) {
      hash.update(buffer.subarray(0, n));
      size += n;
    }
    requireThat(hash.digest('hex') === expected, `SHA-256 mismatch: ${file}`);
    return size;
  } finally {
    fs.closeSync(fd);
  }
}

function inspect(directory) {
  let bytes = 0;
  for (const [name, hash] of Object.entries(pins)) {
    const size = verifyFile(path.join(directory, name), hash);
    if (inputs.includes(name)) bytes += size;
  }
  const report = JSON.parse(checked(execute(path.join(directory, 'nand-stockroot-update-host'),
    ['plan', '--manifest', path.join(directory, inputs[0]), '--capsule', path.join(directory, inputs[1])]), 'offline sealed plan'));
  requireThat(report.manifestSHA256 === pins[inputs[0]] && report.applyConfirmation === ack &&
    report.plan.payload.sha256 === pins[inputs[1]], 'unexpected sealed engine profile');
  return { plan: report.plan, bytes };
}

function syncDirectory(directory) {
  const fd = fs.openSync(directory, 'r');
  try { fs.fsyncSync(fd); } finally { fs.closeSync(fd); }
}

function durable(file, value) {
  const fd = fs.openSync(file, 'wx', 0o600);
  try {
    fs.writeFileSync(fd, typeof value === 'string' ? value : `${JSON.stringify(value, null, 2)}\n`);
    fs.fsyncSync(fd);
  } finally { fs.closeSync(fd); }
  syncDirectory(path.dirname(file));
}

function selectTransport(text) {
  const matches = text.split(/\r?\n/).filter(line => line.split(/\s+/).includes(`usb:${usb}`));
  requireThat(matches.length === 1, `expected exactly one physical USB ${usb} transport`);
  const words = matches[0].trim().split(/\s+/);
  const id = words.find(word => /^transport_id:[1-9][0-9]*$/.test(word));
  requireThat(words[0] === '0123456789ABCDEF' && words[1] === 'device' && id,
    'physical target is not the expected ready RAM ADB transport');
  return id.split(':')[1];
}

function wrapRemote(command, token) {
  const framed = `set -e\n/bin/busybox printf '\\nREINVOKE_BEGIN_${token}\\n'\n${command}`;
  return `${quote('/bin/busybox')} sh -c ${quote(framed)}; rc=$?; /bin/busybox printf '\\nREINVOKE_EXIT_${token}=%s\\n' "$rc"; exit "$rc"`;
}

function parseRemote(text, token) {
  const normalized = text.replace(/\r\n/g, '\n');
  const lines = normalized.split('\n');
  const begin = `REINVOKE_BEGIN_${token}`;
  const marker = `REINVOKE_EXIT_${token}=`;
  const starts = lines.filter(line => line === begin);
  const matches = lines.filter(line => line.startsWith(marker));
  requireThat(matches.length === 1 && /^0$/.test(matches[0].slice(marker.length)) &&
    normalized.trimEnd().endsWith(matches[0]), 'missing, failed or ambiguous remote exit; do not retry');
  const start = lines.indexOf(begin);
  const end = lines.indexOf(matches[0]);
  requireThat(starts.length === 1 && start < end, 'missing, misplaced or ambiguous remote begin; do not retry');
  return lines.slice(start + 1, end).join('\n').trim();
}

function verifyJournal(text, plan) {
  const events = text.split('\n').map(line => JSON.parse(line));
  const completed = events.filter(event => event.stage === 'phase-complete');
  const blocks = events.filter(event => event.stage === 'after-block').map(event => event.block);
  requireThat(JSON.stringify(blocks) === JSON.stringify(plan.writeOrder), 'incomplete/out-of-order block verification');
  requireThat(completed.length === 1 && completed[0].detail.startsWith(
    `apply main=${plan.targetMainSHA256} OOB=${plan.targetOobSHA256}; unchanged blocks verified; cleanup pending; `),
  'missing writer full-target/unchanged-block verification');
  requireThat(events.slice(-3).map(event => event.stage).join(',') === 'phase-complete,before-cleanup,cleanup-complete-no-reboot' &&
    events.slice(-2).every(event => event.detail === '<nil>') &&
    !events.some(event => /failed/.test(event.stage)), 'engine cleanup failed or is unproven');
}

// No timeout or detach: the engine owns safe stopping after a block has been erased.
function foreground(file, args, output, errors) {
  return new Promise(resolve => {
    const out = fs.openSync(output, 'wx', 0o600);
    const err = fs.openSync(errors, 'wx', 0o600);
    const child = spawn(file, args, { stdio: ['inherit', 'pipe', 'pipe'] });
    const forward = signal => child.kill(signal);
    const handlers = ['SIGINT', 'SIGTERM', 'SIGHUP'].map(signal => {
      const handler = () => forward(signal);
      process.on(signal, handler);
      return [signal, handler];
    });
    let failure;
    const displayError = error => {
      failure = error;
      child.stdout.destroy();
      child.stderr.destroy();
    };
    process.stdout.on('error', displayError);
    process.stderr.on('error', displayError);
    const capture = (stream, fd, display) => {
      stream.on('data', chunk => {
        try {
          fs.writeFileSync(fd, chunk);
          if (!display.write(chunk)) {
            stream.pause();
            display.once('drain', () => stream.resume());
          }
        } catch (error) {
          failure = error;
          stream.destroy();
        }
      });
    };
    capture(child.stdout, out, process.stdout);
    capture(child.stderr, err, process.stderr);
    child.on('error', error => { failure = error; });
    child.on('close', (status, signal) => {
      for (const [name, handler] of handlers) process.removeListener(name, handler);
      process.stdout.removeListener('error', displayError);
      process.stderr.removeListener('error', displayError);
      try {
        fs.fsyncSync(out);
        fs.fsyncSync(err);
      } catch (error) { failure = error; }
      fs.closeSync(out);
      fs.closeSync(err);
      resolve({ status, signal, error: failure && String(failure),
        stdout: fs.readFileSync(output, 'utf8'), stderr: fs.readFileSync(errors, 'utf8') });
    });
  });
}

const usage = `Usage: node tools/nand-inspect/trial-runner.js ARTIFACT_DIR [inspect|apply]
  apply requires --evidence NEW_HOST_DIR --confirm ${ack} --approval-ref TEXT
Default: offline inspect; no ADB, staging or NAND access.
Software acknowledgment is NOT human approval. Fresh external owner approval is required.
No retry, rollback, boot helper, reboot or power control. See trial-runner.md.`;

async function run(argv, dependencies = {}) {
  if (argv.length === 0 || argv[0] === '--help') { console.log(usage); return; }
  const directory = fs.realpathSync(argv[0]);
  const action = argv[1] || 'inspect';
  requireThat(['inspect', 'apply'].includes(action), 'only inspect or explicit apply is supported');
  const options = {};
  for (let i = 2; i < argv.length; i += 2) {
    requireThat(['--evidence', '--confirm', '--approval-ref'].includes(argv[i]) &&
      argv[i + 1] && !options[argv[i]], 'unknown, missing or duplicate argument');
    options[argv[i]] = argv[i + 1];
  }
  requireThat(action === 'apply' || Object.keys(options).length === 0, 'inspect accepts no write options');
  if (action === 'apply') {
    requireThat(options['--confirm'] === ack, 'wrong or missing phase acknowledgment');
    requireThat(options['--approval-ref'] && options['--approval-ref'].trim() && options['--evidence'],
      'external owner approval reference and new host evidence directory are required');
  }
  const bundle = (dependencies.inspect || inspect)(directory);
  console.log(JSON.stringify({ action, manifestSHA256: pins[inputs[0]], blocks: bundle.plan.writeOrder.length,
    targetMainSHA256: bundle.plan.targetMainSHA256, approval: 'external/manual, not conferred by this tool' }, null, 2));
  if (action === 'inspect') return;

  const evidence = path.resolve(options['--evidence']);
  const intent = path.join(directory, `.trial-${usb}-${pins[inputs[0]]}.intent.json`);
  requireThat(!fs.existsSync(intent), `prior intent exists; no reissue: ${intent}`);
  fs.mkdirSync(evidence, { mode: 0o700 });
  syncDirectory(path.dirname(evidence));
  const exec = dependencies.execute || execute;
  const live = dependencies.foreground || foreground;
  const discovery = checked(exec('adb', [...adbBase, 'devices', '-l']), 'ADB discovery');
  durable(path.join(evidence, 'devices.txt'), discovery);
  const transport = selectTransport(discovery);
  const selected = [...adbBase, '-t', transport];
  const token = crypto.randomBytes(16).toString('hex');
  const remote = `${ramRoot}/reinvoke-nand-trial-${token}`;
  const wrap = command => wrapRemote(command, token);
  const shell = (command, name) => {
    const result = exec('adb', [...selected, 'shell', wrap(command)]);
    durable(path.join(evidence, `${name}.json`), result);
    return parseRemote(checked(result, name), token);
  };
  const identity = shell(`set -eu
/bin/busybox id -u
/bin/busybox uname -m
/bin/busybox cat /proc/version
/bin/busybox test -c /dev/reinvoke-nand-ro
/bin/busybox test ! -L /dev/reinvoke-nand-ro
/bin/busybox stat -c '%t:%T:%a' /dev/reinvoke-nand-ro
/bin/busybox stat -f -c '%t' ${ramRoot}
/bin/busybox df -Pk ${ramRoot} | /bin/busybox awk 'END {print $4}'
/bin/busybox awk '/^MemAvailable:/ {available=1; a=$2} /^MemFree:/ {f=$2} /^Buffers:/ {b=$2} /^Cached:/ {c=$2} /^Shmem:/ {s=$2} END {print available ? a : f+b+c-s}' /proc/meminfo`, 'ram-identity').split('\n');
  requireThat(identity.length === 7 && identity[0] === '0' && identity[1] === 'armv7l' &&
    identity[2] === banner && identity[3] === '5a:3:400' && identity[4] === '1021994',
  'wrong RAM kernel, read-only node or tmpfs identity');
  const inputKB = Math.ceil(bundle.bytes / 1024);
  requireThat(identity.slice(5).every(value => /^[0-9]+$/.test(value)) &&
    Number(identity[5]) >= inputKB + 16 * 1024 && Number(identity[6]) >= inputKB + 32 * 1024,
  'insufficient tmpfs space (inputs + 16 MiB) or RAM (inputs + 32 MiB)');
  shell(`set -eu; umask 077; /bin/busybox mkdir ${quote(remote)}`, 'stage-create');
  for (const name of inputs) {
    const result = exec('adb', [...selected, 'push', path.join(directory, name), `${remote}/${name}`]);
    durable(path.join(evidence, `push-${name}.json`), result);
    checked(result, `push ${name}`);
  }
  const sums = inputs.map(name => `${pins[name]}  ${remote}/${name}`).join('\n');
  shell(`set -eu; /bin/busybox printf '%s\\n' ${quote(sums)} | /bin/busybox sha256sum -c -; /bin/busybox chmod 0700 ${quote(`${remote}/${inputs[2]}`)}`, 'stage-hashes');
  const finalDiscovery = checked(exec('adb', [...adbBase, 'devices', '-l']), 'final ADB identity');
  durable(path.join(evidence, 'devices-before-apply.txt'), finalDiscovery);
  requireThat(selectTransport(finalDiscovery) === transport,
    'physical transport changed during staging');
  const command = `${quote(`${remote}/${inputs[2]}`)} apply --manifest ${quote(`${remote}/${inputs[0]}`)} --capsule ${quote(`${remote}/${inputs[1]}`)} --evidence ${quote(`${remote}/evidence`)} --confirm ${quote(ack)}`;
  const request = { time: new Date().toISOString(), action, pins, usb, transport, remote, evidence,
    approvalReference: options['--approval-ref'], acknowledgment: ack, command,
    warning: 'Intent is permanent even on failure. No automatic reissue. Software acknowledgment is not human approval.' };
  durable(path.join(evidence, 'intent.json'), request);
  durable(intent, request);
  console.log(`One foreground apply; keep the transport live. Evidence: ${evidence}`);
  const result = await live('adb', [...selected, 'shell', wrap(command)],
    path.join(evidence, 'apply.stdout'), path.join(evidence, 'apply.stderr'));
  durable(path.join(evidence, 'transport-result.json'), { status: result.status, signal: result.signal, error: result.error });
  const journal = parseRemote(checked(result, 'foreground apply'), token);
  durable(path.join(evidence, 'events.jsonl'), `${journal}\n`);
  verifyJournal(journal, bundle.plan);
  // Delete only our three staged inputs, and only after verified engine cleanup and durable host evidence.
  shell(`set -eu; /bin/busybox rm ${inputs.map(name => quote(`${remote}/${name}`)).join(' ')}`, 'staged-input-cleanup');
  durable(path.join(evidence, 'result.json'), { result: 'writer-verified-target-and-cleanup',
    independentReadback: false, nativeBootAcceptance: false, remoteEvidence: `${remote}/evidence`,
    next: 'STOP at manual observer/helper-off/power gate; fresh owner acknowledgment required' });
  console.log('Writer full-target verification and cleanup passed; NOT independent readback or native boot acceptance.');
  console.log('STOP: manual helper-off, calibrated observer and owner power gate. No automatic power action.');
}

if (require.main === module) {
  run(process.argv.slice(2)).catch(error => {
    console.error(`STOP: ${error.message}\nPreserve evidence and RAM state. No retry/rollback/reboot.`);
    process.exitCode = 1;
  });
}

module.exports = { run, inspect, foreground, verifyFile, selectTransport, wrapRemote, parseRemote, verifyJournal, pins, ack, banner };
