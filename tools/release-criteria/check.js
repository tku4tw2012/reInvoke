// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT
//
// Check a boot log, a device and a flash attempt against criteria.json.
//
// The criteria used to live as a table in a document, and it drifted three
// times: it recorded eight services when the build ran thirty-four, the
// startup chime as failed after it had played, and the ring count as
// unexplained after it had been explained. Each drift cost a round of
// rediscovery. Prose about what a good boot looks like has no way to notice
// that it has stopped being true; this does.
//
// Usage:
//   node check.js boot   RUNTIME_LOG
//   node check.js flash  EVIDENCE_DIR STAGING_DIR
//   node check.js device            (requires adb)
'use strict';
const fs = require('node:fs');
const path = require('node:path');
const { execFileSync } = require('node:child_process');

const criteria = JSON.parse(
  fs.readFileSync(path.join(__dirname, 'criteria.json'), 'utf8'));

let failures = 0;
let passes = 0;

function report(id, ok, detail, why) {
  if (ok) {
    passes += 1;
    console.log(`  PASS  ${id}${detail ? '  ' + detail : ''}`);
    return;
  }
  failures += 1;
  console.log(`  FAIL  ${id}${detail ? '  ' + detail : ''}`);
  if (why) console.log(`        ${why}`);
}

function matchLog(text, entry) {
  const found = text.includes(entry.pattern);
  const want = entry.expect === 'present';
  report(entry.id, found === want,
    `${entry.expect} "${entry.pattern}"${found ? ' (found)' : ' (not found)'}`,
    found === want ? null : entry.why);
}

function checkBoot(logPath) {
  const text = fs.readFileSync(logPath, 'utf8');
  console.log(`boot log: ${logPath}`);
  for (const entry of criteria.boot) matchLog(text, entry);
}

function adb(command) {
  return execFileSync('adb', ['shell', command], { encoding: 'utf8' }).trim();
}

function checkDevice() {
  console.log('device: over adb');
  for (const entry of criteria.device) {
    if (entry.check === 'pid-count') {
      const count = Number(adb('ls /run/reinvoke/*.pid 2>/dev/null | wc -l'));
      report(entry.id, count >= entry.minimum,
        `${count} services (minimum ${entry.minimum})`, entry.why);
    } else if (entry.check === 'gadget-string') {
      const value = adb(
        `cat /sys/class/android_usb/android0/${entry.attribute} 2>/dev/null`);
      report(entry.id, value !== '' && value !== entry.reject,
        `${entry.attribute}=${value || '(empty)'}`, entry.why);
    }
  }
}

function checkFlash(evidenceDir, stagingDir) {
  console.log(`flash evidence: ${evidenceDir}`);
  const read = name => {
    try { return fs.readFileSync(path.join(evidenceDir, name), 'latin1'); }
    catch { return ''; }
  };
  const sources = { arm: read('arm.log'), console: read('console.raw') };
  for (const entry of criteria.flash) {
    if (entry.pattern) {
      matchLog(sources[entry.source] || '', entry);
    } else if (entry.check === 'file-absent') {
      const there = fs.existsSync(path.join(stagingDir, entry.path));
      report(entry.id, !there, `${entry.path} ${there ? 'present' : 'absent'}`,
        entry.why);
    } else if (entry.check === '07-matches-83') {
      const size = fs.statSync(path.join(stagingDir, '83_IMAGE')).size;
      const declared =
        fs.readFileSync(path.join(stagingDir, '07_IMAGE')).readUInt32LE(0);
      report(entry.id, size === declared,
        `83_IMAGE=${size} 07_IMAGE=${declared}`, entry.why);
    }
  }
}

const [mode, first, second] = process.argv.slice(2);
if (mode === 'boot' && first) checkBoot(first);
else if (mode === 'device') checkDevice();
else if (mode === 'flash' && first && second) checkFlash(first, second);
else {
  console.error('usage: check.js boot LOG | device | flash EVIDENCE STAGING');
  process.exit(2);
}

console.log(`\n${passes} passed, ${failures} failed`);
if (criteria.listener.length) {
  console.log('\nNot checkable here, a person has to confirm:');
  for (const entry of criteria.listener)
    console.log(`  - ${entry.id}: ${entry.why}`);
}
process.exit(failures === 0 ? 0 : 1);
