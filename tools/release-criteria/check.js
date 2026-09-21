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
let unevidenced = 0;

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

// A criterion whose log was never captured is not a criterion that was
// violated. Calling it FAIL says the device misbehaved when the truth is that
// nobody wrote the evidence down, which is what seize-05813-2100 looked like.
// It is still not a pass.
function noEvidence(id, source, why) {
  unevidenced += 1;
  console.log(`  NO-EVIDENCE  ${id}  no ${source} log in this directory`);
  if (why) console.log(`        ${why}`);
}

function matchLog(text, entry) {
  // The same event is worded differently by the single-stage arm and the
  // two-stage seize, so a criterion may name more than one acceptable string.
  // Matching on only the older wording reported the 05.8.13 flash as failing.
  const patterns = [].concat(entry.pattern);
  const hit = patterns.find(pattern => text.includes(pattern));
  const found = hit !== undefined;
  const want = entry.expect === 'present';
  report(entry.id, found === want,
    `${entry.expect} "${found ? hit : patterns.join('" or "')}"` +
      `${found ? ' (found)' : ' (not found)'}`,
    found === want ? null : entry.why);
}

function checkBoot(logPath) {
  const text = fs.readFileSync(logPath, 'utf8');
  console.log(`boot log: ${logPath}`);
  for (const entry of criteria.boot) {
    // "At boot" has to mean during the startup sequence. The runtime log is
    // append-only and lives as long as the unit is up, so an unbounded
    // "absent" criterion eventually fails for ordinary use: a dial turned
    // minutes after boot wrote RING_ARC, which is the ring working.
    let scoped = text;
    if (entry.until) {
      const end = text.indexOf(entry.until);
      if (end < 0) {
        report(entry.id, false, `no "${entry.until}" in the log to bound it`,
          entry.why);
        continue;
      }
      scoped = text.slice(0, end + entry.until.length);
    }
    matchLog(scoped, entry);
  }
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
  const read = names => {
    for (const name of [].concat(names)) {
      try { return fs.readFileSync(path.join(evidenceDir, name), 'latin1'); }
      catch { /* try the next name */ }
    }
    return '';
  };
  // The two-stage seize writes seize.log and flash-report.txt where the older
  // single-stage arm wrote arm.log and console.raw. Naming only the old pair
  // made this report four failures against seize-05813-2100, the run that put
  // the firmware on the unit in hand: a check that fails on success is as
  // misleading as one that passes on failure.
  const sources = {
    arm: read(['arm.log', 'flash-report.txt', 'seize.log']),
    // Only console.raw. seize.log is the tool's own narration -- it says
    // "Marvell 88DE3006" from a USB descriptor, which is not the device
    // saying it -- so accepting it here would pass the console criteria
    // without the console ever having been read.
    console: read('console.raw'),
  };
  if (!sources.arm && !sources.console) {
    console.log('  no readable log in this evidence directory');
  }
  for (const entry of criteria.flash) {
    if (entry.pattern) {
      if (!sources[entry.source]) {
        noEvidence(entry.id, entry.source, entry.why);
      } else {
        matchLog(sources[entry.source], entry);
      }
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

console.log(`\n${passes} passed, ${failures} failed` +
  (unevidenced ? `, ${unevidenced} with no evidence captured` : ''));
if (criteria.listener.length) {
  console.log('\nNot checkable here, a person has to confirm:');
  for (const entry of criteria.listener)
    console.log(`  - ${entry.id}: ${entry.why}`);
}
// Missing evidence is not success. A run that captured no console proves
// nothing about the console, so it must not exit clean.
process.exit(failures === 0 && unevidenced === 0 ? 0 : 1);
