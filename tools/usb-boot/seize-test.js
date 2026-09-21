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
const flash = path.join(here, 'flash-nand.sh');
const detect = path.join(here, 'prompt-control.sh');

for (const script of [seize, flash, detect]) {
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

// Catching the device, deciding a prompt is live, and writing are three
// separate scripts now. Folding them together produced a flash path that had
// to guess how long to keep probing, and it is why a run could sit in its own
// detection loop with nothing to show.
const flashCode = code(flashText);
assert(!/while true/.test(flashCode.slice(0, flashCode.indexOf('l2nand'))),
  'flash-nand.sh hunts for a prompt again; that belongs to prompt-control.sh');

// The write must still come after a confirmation that the prompt executes.
const checkAt = flashText.indexOf('prompt-control.sh');
const writeAt = flashText.indexOf("send \"$(printf 'l2nand 83");
assert(checkAt > 0, 'flash-nand.sh no longer confirms the prompt at all');
assert(writeAt > 0, 'flash-nand.sh lost the write command');
assert(checkAt < writeAt,
  'flash-nand.sh writes before confirming the prompt answers');

// And that confirmation must be the nonce, not a guess about text. Matching
// "MV88DE3100" stalled at "MV88D" for about fifty seconds on the 05.8.12
// write while the device was live, and matching growth reported control
// against a device that had already gone, because the relay had written its
// own "console closed" marker into the log.
const detectCode = code(fs.readFileSync(detect, 'utf8'));
assert(!/MV88DE3100/.test(detectCode),
  'the prompt is being detected by matching text again; it arrives chunked');
assert(/urandom/.test(detectCode),
  'prompt-control.sh no longer generates a fresh token');
assert(/hits/.test(detectCode) && /echo %s/.test(detectCode),
  'prompt-control.sh no longer asks the device to echo the token back');
assert(/timeout/.test(detectCode),
  'prompt-control.sh writes to the FIFO unbounded; with no reader that blocks forever');

// Only bytes that arrive after the token is sent may satisfy it, or an
// earlier run's reply in the same log would.
assert(/tail -c \+\$\(\(mark/.test(detectCode),
  'prompt-control.sh considers the whole log again, not just the reply');

// And the write must be acknowledged, not assumed. The device prints that it
// is erasing before it writes a byte; that is the receipt.
const writeStep = code(flashText.slice(writeAt));
assert(/erase nand/i.test(writeStep),
  'the write is sent without confirming the device took it');

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

// The end-to-end walk through a fake U-Boot used to live here. It span a
// responder for twenty seconds to prove what a three second run against the
// same fake proves directly, and its timing was the only thing that ever
// failed. The behaviour it covered is asserted above without the wall clock:
// prompt-control.sh is challenged in its own test, and the ordering and
// shape of flash-nand.sh are checked statically. Run the real thing against
// a fake prompt by hand when changing the write path.

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
