// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT
//
// Check what the documentation claims against what is actually there.
//
// This project has repeatedly discovered that a document described something
// that had stopped being true: BlueZ and BlueALSA as the current audio path
// after they were replaced, a link to a source file that had been deleted, a
// service count from an older build, a claim that the donor unmutes after
// initialisation when its binary contains no such unmute. Each one was found
// by accident, late, while chasing something else.
//
// The checks here are deliberately narrow. A claim is only reported when it
// can be settled against something real: a file on disk, a binary in the
// built image, a procedure the running device registered, a flag the service
// defines. Anything requiring judgement is listed separately for a person
// rather than guessed at, because a checker that guesses produces a list
// nobody trusts, and an untrusted list gets ignored.
//
// Usage: node audit.js [--json]
'use strict';
const fs = require('node:fs');
const path = require('node:path');

const repo = path.resolve(__dirname, '../..');
const archive = process.env.REINVOKE_ARCHIVE ||
  path.resolve(repo, '../reinvoke-archive');
const runtime = process.env.AUDIT_RUNTIME_TREE || path.join(archive,
  'build/artifacts/reinvoke-native-05.8.11-20260918/main/build-a/runtime');
const donor = process.env.AUDIT_DONOR_ROOTFS ||
  path.join(archive, 'extracted/phase3/stockroot/rootfs');

const findings = [];
function finding(kind, file, line, claim, detail) {
  findings.push({ kind, file, line, claim, detail });
}

function markdownFiles() {
  const out = [];
  const walk = dir => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      if (entry.name === '.git' || entry.name === 'node_modules') continue;
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (entry.name.endsWith('.md')) out.push(full);
    }
  };
  walk(path.join(repo, 'docs'));
  for (const entry of fs.readdirSync(repo))
    if (entry.endsWith('.md')) out.push(path.join(repo, entry));
  return out;
}

// Everything the built image actually contains, by basename. A document that
// names a binary as part of this runtime is checked against this, not against
// the donor rootfs, because shipping is the question.
function imageContents() {
  const names = new Set();
  if (!fs.existsSync(runtime)) return names;
  const walk = dir => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name);
      names.add(entry.name);
      if (entry.isDirectory()) { try { walk(full); } catch { /* skip */ } }
    }
  };
  walk(runtime);
  return names;
}

function repoPaths() {
  const names = new Set();
  const walk = dir => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      if (entry.name === '.git' || entry.name === 'node_modules') continue;
      const full = path.join(dir, entry.name);
      names.add(path.relative(repo, full));
      if (entry.isDirectory()) walk(full);
    }
  };
  walk(repo);
  return names;
}

// Flags each Go service defines, so a document naming one can be settled.
function definedFlags() {
  const flags = new Set();
  const walk = dir => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) { walk(full); continue; }
      const text = fs.readFileSync(full, 'utf8');
      if (entry.name.endsWith('.go')) {
        const pattern =
          /flag\.(?:String|Int|Bool|Duration|Float64)(?:Var)?\(\s*(?:&[\w.]+\s*,\s*)?"([a-z0-9-]+)"/g;
        for (const match of text.matchAll(pattern)) flags.add(match[1]);
      } else if (/\.(js|mjs|sh)$/.test(entry.name)) {
        // The host reference tools and shell helpers take flags too, and the
        // documents cite them alongside the Go services.
        for (const match of text.matchAll(/'--([a-z0-9-]{3,})'|"--([a-z0-9-]{3,})"/g))
          flags.add(match[1] || match[2]);
      }
    }
  };
  walk(path.join(repo, 'tools'));
  return flags;
}

const image = imageContents();
const paths = repoPaths();
const flags = definedFlags();

// Components named as removed. Each was genuinely part of this runtime once,
// so a document mentioning one is not automatically wrong; it is wrong when it
// presents one as current. The distinction needs a reader, so these are
// reported for review rather than as failures.
const removed = new Map([
  ['bluealsa', 'replaced by the donor Bluedroid stack in 225183e'],
  ['bluez', 'replaced by the donor Bluedroid stack in 225183e'],
  ['bluetoothd', 'replaced by the donor Bluedroid stack in 225183e'],
  ['playback_policy.go', 'deleted with the withdrawn amplifier mute policy'],
]);

// Records of what happened are not claims about what is true now. A journal
// entry saying BlueALSA was used in September is correct and must stay
// correct; the contract saying BlueALSA is the audio path is not. Only the
// documents that describe the current system are checked for this, so the
// output stays small enough to act on.
const historical = [
  'docs/journal.md',
  'docs/acquisition/',
  'docs/corpus/',
  'docs/nand-write-decision.md',
  'docs/revival-roadmap.md',
  'docs/release-validation.md',
];
// A document may also declare itself superseded in its own opening lines.
// That is better than a list here, because the declaration travels with the
// file and a reader sees it.
const declaresSuperseded = text =>
  /describes a design that is no longer shipped|superseded|no longer shipped/i
    .test(text.slice(0, 1200));
const isHistorical = (file, text) =>
  historical.some(prefix => file.startsWith(prefix)) ||
  (text !== undefined && declaresSuperseded(text));

for (const file of markdownFiles()) {
  const relative = path.relative(repo, file);
  const whole = fs.readFileSync(file, 'utf8');
  const lines = whole.split('\n');
  lines.forEach((text, index) => {
    const number = index + 1;

    // Paths this repository owns. Vendor and kernel source is named all over
    // these documents and is not supposed to be here, so only paths that
    // look like ours are checked: a repository-rooted directory, or a bare
    // Go or JavaScript file, which this project writes and the vendor does
    // not.
    for (const match of text.matchAll(/`([a-zA-Z0-9_./-]+\.(?:go|js|sh|json))`/g)) {
      const claim = match[1];
      if (claim.startsWith('/') || claim.includes('..')) continue;
      const ours = /^(tools|docs|\.github)\//.test(claim) ||
        (!claim.includes('/') && /\.(go|js)$/.test(claim));
      if (!ours) continue;
      const known = [...paths].some(p => p === claim || p.endsWith('/' + claim));
      if (!known)
        finding('missing-path', relative, number, claim,
          'named as a file but no such path exists in the repository');
    }

    // Command-line flags. Options belonging to donor binaries are excluded:
    // bonefish is the vendor's router and its switches are documented here as
    // facts about it, not as things this runtime implements.
    const foreignFlags = new Set(['no-json', 'no-msgpack']);
    for (const match of text.matchAll(/`--([a-z0-9-]{3,})`/g)) {
      if (foreignFlags.has(match[1])) continue;
      if (!flags.has(match[1]))
        finding('undefined-flag', relative, number, '--' + match[1],
          'named as a flag but no service defines it');
    }

    // Components that were removed from the runtime.
    // A removed component may be named correctly, as something that used to
    // be here, or incorrectly, as something that still is. The difference is
    // in the wording, so the wording is what decides: a mention framed as
    // retired is accepted, and a bare mention is reported.
    //
    // This is a heuristic and it can be fooled by a sentence that happens to
    // contain one of these words. It is still worth having: without it every
    // licence line and every history note is reported forever, and a list
    // that is mostly noise is one nobody reads.
    const retired =
      /no longer|replaced|kept for|retain|former|removed|withdrawn|historical|used to|earlier|until|superseded|previously|reversed|no such|not (?:in|present)/i;
    const context = [lines[index - 1] || '', text, lines[index + 1] || ''].join(' ');
    const lowered = text.toLowerCase();
    for (const [name, why] of removed) {
      if (isHistorical(relative, whole)) continue;
      if (!lowered.includes(name)) continue;
      if (retired.test(context)) continue;
      const shipped = image.has(name) || image.has(name + '.ko');
      if (!shipped)
        finding('removed-component', relative, number, name,
          `${why}; not present in the built image, and not described as past`);
    }
  });
}

// Links and anchors. A link to a file that was deleted is the cheapest kind
// of documentation rot to detect and the most annoying to hit: this project
// shipped a pointer to playback_policy.go for weeks after deleting it.
function anchorsOf(text) {
  const out = new Set();
  for (const line of text.split('\n')) {
    const heading = /^#{1,6}\s+(.*)/.exec(line);
    if (!heading) continue;
    out.add(heading[1].trim().toLowerCase()
      .replace(/[^\w\s-]/g, '').replace(/\s+/g, '-'));
  }
  return out;
}
for (const file of markdownFiles()) {
  const relative = path.relative(repo, file);
  const text = fs.readFileSync(file, 'utf8');
  text.split('\n').forEach((line, index) => {
    for (const match of line.matchAll(/\[[^\]]*\]\(([^)]+)\)/g)) {
      const target = match[1];
      if (/^(https?:|mailto:|#)/.test(target)) continue;
      const [rawPath, fragment] = target.split('#');
      const resolved = rawPath
        ? path.resolve(path.dirname(file), decodeURIComponent(rawPath))
        : file;
      if (!fs.existsSync(resolved)) {
        finding('broken-link', relative, index + 1, target,
          'link target does not exist');
        continue;
      }
      if (fragment && resolved.endsWith('.md') &&
          !anchorsOf(fs.readFileSync(resolved, 'utf8')).has(fragment)) {
        finding('broken-anchor', relative, index + 1, target,
          'link target exists but has no such heading');
      }
    }
  });
}

const byKind = {};
for (const item of findings) byKind[item.kind] = (byKind[item.kind] || 0) + 1;

if (process.argv.includes('--json')) {
  console.log(JSON.stringify({ findings, counts: byKind }, null, 2));
} else {
  console.log(`ground truth:`);
  console.log(`  built image entries : ${image.size}`);
  console.log(`  repository paths    : ${paths.size}`);
  console.log(`  flags defined       : ${flags.size}`);
  console.log(`\nfindings: ${findings.length}`);
  for (const [kind, count] of Object.entries(byKind))
    console.log(`  ${kind.padEnd(20)} ${count}`);
  let current = '';
  for (const item of findings.sort((a, b) =>
    a.file.localeCompare(b.file) || a.line - b.line)) {
    if (item.file !== current) { current = item.file; console.log(`\n${current}`); }
    console.log(`  ${String(item.line).padStart(4)}  ${item.kind}: ${item.claim}`);
    console.log(`        ${item.detail}`);
  }
}
process.exit(0);
