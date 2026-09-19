#!/usr/bin/env node
// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT
//
// Prove the seize path keeps its promises, without hardware.
//
// Two promises. Seizing and writing are separate, so a failed entry costs
// nothing and the window after a successful one is indefinite. And the prompt
// is found by probing rather than by matching text, because the console is a
// stream of USB transfers chopped at arbitrary boundaries.
//
// Both keep getting optimised back into one timed motion. A test that fails
// when they are is harder to argue with than a comment asking for restraint.
'use strict';
const assert = require('node:assert');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { execFileSync, spawn, spawnSync } = require('node:child_process');

const here = __dirname;
const seize = path.join(here, 'arm-seize.sh');
const flash = path.join(here, 'seize-then-flash.sh');

for (const script of [seize, flash]) {
  assert(fs.existsSync(script), `${path.basename(script)} is missing`);
  const syntax = spawnSync('bash', ['-n', script], { encoding: 'utf8' });
  assert.equal(syntax.status, 0,
    `${path.basename(script)} has a syntax error:\n${syntax.stderr}`);
}

const seizeText = fs.readFileSync(seize, 'utf8');
const flashText = fs.readFileSync(flash, 'utf8');
// Comments explain why these scripts are shaped this way, and naming the
// thing they refuse to do is part of that. Only code is checked.
const code = text => text.split('\n')
  .filter(line => !/^\s*#/.test(line)).join('\n');
const seizeCode = code(seizeText);

// Seizing must send nothing. A rig that writes on its own has not split
// anything, whatever it is called.
assert(!/l2nand/.test(seizeCode),
  'arm-seize.sh references l2nand; seizing must not write NAND');
assert(/uboot-console\.py/.test(seizeCode),
  'arm-seize.sh no longer uses the holding relay');
assert(/fast-poll/.test(seizeCode),
  'arm-seize.sh no longer defaults to the faster-polling helper');
assert(/while true/.test(seizeCode),
  'arm-seize.sh does not restart its helper; one failed entry would end it');

// The write must come after a confirmation that the prompt executes.
const versionAt = flashText.indexOf('send "version"');
const writeAt = flashText.indexOf('send "l2nand 83"');
assert(versionAt > 0 && writeAt > 0, 'the flash step lost version or l2nand');
assert(versionAt < writeAt,
  'seize-then-flash.sh writes before confirming the prompt answers');

// The prompt must not be detected by matching its text. It stalled at "MV88D"
// for about fifty seconds on the 05.8.12 write while the device was live.
const detector = code(flashText.slice(0, versionAt));
assert(!/MV88DE3100"/.test(detector),
  'the prompt is being detected by matching text again; it arrives chunked');
assert(/poke/.test(detector), 'the prompt detector no longer probes');

const work = fs.mkdtempSync(path.join(os.tmpdir(), 'seize-'));
const required = ['06_IMAGE', '07_IMAGE', '09_IMAGE', '79_IMAGE', '81_IMAGE',
  '82_IMAGE', 'bcm_erom.bin.usb', 'bootloader.img', 'drm_erom.img',
  'sysinit.img'];
const staging = path.join(work, 'staging');
fs.mkdirSync(staging);
for (const name of required) fs.writeFileSync(path.join(staging, name), 'x');

// Staging that can answer 0x08 must be refused: answering it helps a device
// that has not entered recovery resume its normal boot.
fs.writeFileSync(path.join(staging, '08_IMAGE'), 'x');
let outcome = spawnSync('bash', [seize, staging, path.join(work, 'e1')],
  { encoding: 'utf8', timeout: 15000 });
assert(/08_IMAGE/.test(outcome.stderr), 'serving 08_IMAGE was accepted');
fs.rmSync(path.join(staging, '08_IMAGE'));

// And staging missing something the device will ask for.
fs.rmSync(path.join(staging, '09_IMAGE'));
outcome = spawnSync('bash', [seize, staging, path.join(work, 'e2')],
  { encoding: 'utf8', timeout: 15000 });
assert(/missing 09_IMAGE/.test(outcome.stderr),
  'incomplete staging was accepted');

// Drive the flash step against a fake console and FIFO. This is the part that
// cannot be reasoned about: what it sends, in what order, and whether a dead
// prompt stops it.
const consolePath = '/tmp/uboot.log';
const fifoPath = '/tmp/uboot_cmd';

function startStep(name) {
  for (const p of [consolePath, fifoPath])
    if (fs.existsSync(p)) fs.rmSync(p, { force: true });
  execFileSync('mkfifo', [fifoPath]);
  fs.writeFileSync(consolePath, '');
  const dir = path.join(work, name);
  fs.mkdirSync(dir, { recursive: true });
  const sentPath = path.join(dir, 'sent.txt');
  fs.writeFileSync(sentPath, '');
  const reader = spawn('bash', ['-c',
    `while true; do cat ${fifoPath} >> ${sentPath}; done`]);
  const child = spawn('bash', [flash, dir]);
  return { sentPath, reader, child };
}
const wait = ms => execFileSync('sleep', [String(ms / 1000)]);
const sentSoFar = run => fs.readFileSync(run.sentPath, 'utf8');
const stop = run => { run.child.kill('SIGKILL'); run.reader.kill('SIGKILL'); };

// A prompt truncated mid-string is still a prompt and must be found. This is
// exactly what happened on the 05.8.12 write: 1348 bytes, ending "MV88D",
// frozen, with the device live and waiting.
{
  const run = startStep('truncated');
  fs.appendFileSync(consolePath,
    'all done.\nexecute_cmd_from_script, 380\nMV88D');
  let answered = 0;
  const responder = setInterval(() => {
    const sent = sentSoFar(run);
    if (sent.length > answered) {
      answered = sent.length;
      // Answer as a live prompt does, without ever completing the string the
      // old detector waited for.
      fs.appendFileSync(consolePath, '\nMV88D');
    }
  }, 200);
  wait(16000);
  clearInterval(responder);
  const sent = sentSoFar(run);
  stop(run);
  assert(/version/.test(sent),
    'a live but truncated prompt was never probed into answering');
}

// A console that never answers must never be written to.
{
  const run = startStep('dead');
  fs.appendFileSync(consolePath, 'booting...\n');
  wait(14000);
  const sent = sentSoFar(run);
  stop(run);
  assert(!/l2nand/.test(sent),
    'NAND was written to a prompt that never answered');
}

fs.rmSync(work, { recursive: true, force: true });
for (const p of [consolePath, fifoPath])
  if (fs.existsSync(p)) fs.rmSync(p, { force: true });

console.log(
  'PASS: seizing sends nothing and restarts across failed entries, the ' +
  'prompt is found by probing rather than matching a chunked stream, a ' +
  'truncated prompt is still found, a dead console is never written to, and ' +
  'staging that can answer 0x08 is refused.');
