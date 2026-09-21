// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT
//
// Prove the criteria can fail. A check that cannot fail is the same as no
// check, which is how the prose version of this passed while describing a
// build that no longer existed.
'use strict';
const assert = require('node:assert');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

const here = __dirname;
const criteria = JSON.parse(
  fs.readFileSync(path.join(here, 'criteria.json'), 'utf8'));

for (const group of ['boot', 'device', 'flash', 'listener']) {
  assert(Array.isArray(criteria[group]) && criteria[group].length > 0,
    `criteria.json lost its ${group} group`);
}
// Every criterion says why it is there. A criterion without a reason cannot be
// judged when it fails, and gets deleted by whoever it inconveniences.
for (const group of ['boot', 'device', 'flash', 'listener'])
  for (const entry of criteria[group])
    assert(typeof entry.why === 'string' && entry.why.length > 20,
      `criterion ${entry.id} has no usable reason`);

// The chime criterion must keep saying that the log line is not proof of
// sound. That lesson cost weeks: CUE_PLAYED was logged the whole time.
const chime = criteria.boot.find(e => e.id === 'startup-chime-rendered');
assert(/not proof|necessary, not sufficient/i.test(chime.why),
  'the chime criterion stopped saying the log line is not proof of sound');
assert(criteria.listener.some(e => e.id === 'chime-audible'),
  'the listener check for an audible chime was removed');

const work = fs.mkdtempSync(path.join(os.tmpdir(), 'criteria-'));
const run = (...args) =>
  spawnSync('node', [path.join(here, 'check.js'), ...args],
    { encoding: 'utf8' });

// A log with everything present and nothing forbidden passes.
const good = criteria.boot
  .filter(e => e.expect === 'present').map(e => e.pattern).join('\n');
const goodLog = path.join(work, 'good.log');
fs.writeFileSync(goodLog, good + '\n');
let result = run('boot', goodLog);
assert.equal(result.status, 0, `a complete log failed:\n${result.stdout}`);

// Removing any single required line must fail, and must name that criterion.
for (const entry of criteria.boot.filter(e => e.expect === 'present')) {
  const missing = criteria.boot
    .filter(e => e.expect === 'present' && e.id !== entry.id)
    .map(e => e.pattern).join('\n');
  const logPath = path.join(work, `missing-${entry.id}.log`);
  fs.writeFileSync(logPath, missing + '\n');
  const outcome = run('boot', logPath);
  assert.notEqual(outcome.status, 0,
    `removing ${entry.pattern} did not fail the boot check`);
  assert(outcome.stdout.includes(`FAIL  ${entry.id}`),
    `removing ${entry.pattern} failed without naming ${entry.id}`);
}

// A criterion bound to a window must ignore the same line outside it, or the
// bound is decorative. This is the case that actually shipped: RING_ARC three
// seconds after the chime failed a boot check, and it was the ring working.
for (const entry of criteria.boot.filter(e => e.expect === 'absent' && e.until)) {
  const logPath = path.join(work, `after-${entry.id}.log`);
  fs.writeFileSync(logPath, good + '\n' + entry.pattern + '\n');
  const outcome = run('boot', logPath);
  assert.equal(outcome.status, 0,
    `${entry.pattern} after ${entry.until} failed ${entry.id}; the window is not honoured`);
}

// A forbidden line present must fail. These are the regressions that shipped:
// a cue skipped, a ring arc drawn at boot, a missing identifiers binary.
for (const entry of criteria.boot.filter(e => e.expect === 'absent')) {
  const logPath = path.join(work, `present-${entry.id}.log`);
  // Put the forbidden line where it would actually be a regression. A
  // criterion bound to the startup sequence is not violated by the same line
  // appearing afterwards, which is the whole reason it is bound: RING_ARC
  // minutes after boot is the ring working. Appending blindly tested the
  // opposite of what the criterion says.
  let text;
  if (entry.until) {
    const end = good.indexOf(entry.until);
    assert(end >= 0,
      `the good log has no ${entry.until} to place ${entry.pattern} before`);
    const lineStart = good.lastIndexOf('\n', end) + 1;
    text = good.slice(0, lineStart) + entry.pattern + '\n' + good.slice(lineStart);
  } else {
    text = good + '\n' + entry.pattern;
  }
  fs.writeFileSync(logPath, text + '\n');
  const outcome = run('boot', logPath);
  assert.notEqual(outcome.status, 0,
    `${entry.pattern} present did not fail the boot check`);
  assert(outcome.stdout.includes(`FAIL  ${entry.id}`),
    `${entry.pattern} present failed without naming ${entry.id}`);
}

// Flash: a staging directory that serves 08_IMAGE must be rejected.
const staging = path.join(work, 'staging');
fs.mkdirSync(staging);
fs.writeFileSync(path.join(staging, '83_IMAGE'), Buffer.alloc(64));
const size = Buffer.alloc(4);
size.writeUInt32LE(64, 0);
fs.writeFileSync(path.join(staging, '07_IMAGE'), size);
const evidence = path.join(work, 'evidence');
fs.mkdirSync(evidence);
fs.writeFileSync(path.join(evidence, 'arm.log'),
  'sent l2nand 83\nu2nand succeed\n');
fs.writeFileSync(path.join(evidence, 'console.raw'),
  '!!!USB_Boot!!!\nMV88DE3100|>\n');
result = run('flash', evidence, staging);
assert.equal(result.status, 0, `a good flash failed:\n${result.stdout}`);

fs.writeFileSync(path.join(staging, '08_IMAGE'), Buffer.alloc(16));
result = run('flash', evidence, staging);
assert.notEqual(result.status, 0, 'serving 08_IMAGE was accepted');
assert(result.stdout.includes('FAIL  staging-withholds-08'));
fs.rmSync(path.join(staging, '08_IMAGE'));

// A size record that does not describe 83_IMAGE must be rejected: the
// known-good 07_IMAGE carries the size of 82_IMAGE.
size.writeUInt32LE(34557374, 0);
fs.writeFileSync(path.join(staging, '07_IMAGE'), size);
result = run('flash', evidence, staging);
assert.notEqual(result.status, 0, 'a mismatched size record was accepted');
assert(result.stdout.includes('FAIL  size-record-matches'));

fs.rmSync(work, { recursive: true, force: true });
console.log(
  'PASS: criteria carry reasons, every required line proven to fail when ' +
  'absent, every forbidden line proven to fail when present, and the flash ' +
  'staging rejections hold.');
