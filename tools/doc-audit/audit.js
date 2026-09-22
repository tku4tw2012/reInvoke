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
const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');

const repo = path.resolve(__dirname, '../..');
const archive = process.env.REINVOKE_ARCHIVE ||
  path.resolve(repo, '../reinvoke-archive');
// The newest built runtime, resolved rather than named.
//
// This was a hardcoded path to one build, and it went stale the moment the
// next one was made: the audit spent two releases reporting no findings
// while checking a runtime two builds old, which is the exact failure it
// exists to catch. A build that removes a component would still have been
// judged against an image that still had it.
function newestRuntime() {
  const root = path.join(archive, 'build/artifacts');
  if (!fs.existsSync(root)) return '';
  // Ordered by the build date in the artifact name, not by mtime. Directory
  // timestamps move when anything touches them, and sorting on those picked
  // a build from ten days earlier.
  const candidates = fs.readdirSync(root)
    .map(name => ({ name, date: (/(\d{8})/.exec(name) || [])[1] }))
    .filter(entry => entry.date)
    .map(entry => ({
      ...entry,
      runtime: path.join(root, entry.name, 'main/build-a/runtime'),
    }))
    .filter(entry => fs.existsSync(entry.runtime))
    .sort((a, b) => b.date.localeCompare(a.date) || b.name.localeCompare(a.name));
  return candidates.length ? candidates[0].runtime : '';
}
const runtime = process.env.AUDIT_RUNTIME_TREE || newestRuntime();
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
  walk(path.join(repo, 'tools'));
  for (const entry of fs.readdirSync(repo))
    if (entry.endsWith('.md')) out.push(path.join(repo, entry));
  return out;
}

// Everything the built image actually contains, by basename. A document that
// names a binary as part of this runtime is checked against this, not against
// the donor rootfs, because shipping is the question.
function imageContents() {
  const names = new Set();
  if (!fs.existsSync(runtime)) {
    // Failing open here is worse than not running. Without the image every
    // shipped-component check silently passes, and the run reports the same
    // "findings: 0" as a real audit. A reviewer cannot tell the two apart.
    console.error(`ground truth missing: no built runtime at ${runtime}`);
    console.error('set AUDIT_RUNTIME_TREE to a build-a/runtime directory');
    process.exit(2);
  }
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
      } else if (/\.(js|mjs|sh|c)$/.test(entry.name) || !entry.name.includes('.')) {
        // Host tools and shell helpers take flags too, and the documents cite
        // them alongside the Go services. Shell writes them bare, in case
        // arms and comparisons, not only quoted; matching only quoted forms
        // reported thirteen flags as undefined that were defined a few lines
        // away in a case statement.
        for (const match of text.matchAll(/--([a-z0-9][a-z0-9-]{2,})/g))
          flags.add(match[1]);
      }
    }
  };
  walk(path.join(repo, 'tools'));
  return flags;
}

const reviewedPath = path.join(__dirname, 'reviewed.json');
const reviewed = fs.existsSync(reviewedPath)
  ? JSON.parse(fs.readFileSync(reviewedPath, 'utf8')) : {};

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
// correct; the contract saying BlueALSA is the audio path is not.
const historical = [
  'docs/journal.md',
  'docs/acquisition/',
  'docs/corpus/',
  'docs/nand-write-decision.md',
  'docs/revival-roadmap.md',
];

// A document is exempt only when it says so in structured front matter:
// `status: superseded` or `status: historical`.
//
// This replaces a regular expression that scanned the surrounding prose for
// words like "replaced" or "no longer". That was unsound in both directions.
// It accepted "BlueZ replaced Bluedroid and is the current stack" and
// "BlueALSA remains current until migration completes", because the magic
// word appears; and it reported "Before commit 225183e, audio ran through
// BlueZ", because none does. A checker that a sentence asserting the opposite
// of the fact can satisfy proves nothing, and reporting zero findings from it
// was misleading. Front matter cannot be satisfied by accident.
function declaredStatus(text) {
  const matter = /^---\n([\s\S]*?)\n---/.exec(text);
  if (!matter) return '';
  const status = /^status:\s*(\S+)/m.exec(matter[1]);
  return status ? status[1].toLowerCase() : '';
}
const isHistorical = (file, text) =>
  historical.some(prefix => file.startsWith(prefix)) ||
  (text !== undefined &&
    ['superseded', 'historical'].includes(declaredStatus(text)));

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
    // Mentions of a removed component, in documents that have not declared
    // themselves historical.
    const lowered = text.toLowerCase();
    for (const [name, why] of removed) {
      if (isHistorical(relative, whole)) continue;
      if (!lowered.includes(name)) continue;
      // A README under tools/<name>/ describes that tool, not the device
      // image. tools/control is host reference tooling whose backend really
      // does drive a BlueALSA CLI, so its README naming BlueALSA is accurate
      // and checking it against the firmware image is a category error.
      const owner = /^tools\/([^/]+)\//.exec(relative);
      if (owner) {
        const dir = path.join(repo, 'tools', owner[1]);
        let usedByTool = false;
        const scan = d => {
          for (const e of fs.readdirSync(d, { withFileTypes: true })) {
            if (usedByTool) return;
            const full = path.join(d, e.name);
            if (e.isDirectory()) { scan(full); continue; }
            if (e.name.endsWith('.md') || e.name === 'node_modules') continue;
            try {
              if (fs.readFileSync(full, 'utf8').toLowerCase().includes(name))
                usedByTool = true;
            } catch { /* binary or unreadable */ }
          }
        };
        try { scan(dir); } catch { /* missing */ }
        if (usedByTool) continue;
      }
      const shipped = image.has(name) || image.has(name + '.ko');
      if (shipped) continue;
      // A mention may be a correction, a licence obligation or a history
      // note. The checker cannot tell, so it does not guess: it reports, and
      // a reviewed mention is recorded in reviewed.json against the exact
      // text that was read. Change the line and the review lapses.
      const digest = crypto.createHash('sha256')
        .update(`${relative}|${name}|${text.trim()}`).digest('hex').slice(0, 16);
      if (reviewed[digest]) continue;
      finding('removed-component', relative, number, name,
        `${why}; not in the built image. If correct, record ${digest} in ` +
        'tools/doc-audit/reviewed.json with a reason.');
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

// The index is the only route a reader has to most of these pages. Nine of
// them were unreachable from it at once, including the flash procedure, so a
// page that exists but is not listed is treated as a finding.
{
  const indexPath = path.join(repo, 'docs/README.md');
  const index = fs.readFileSync(indexPath, 'utf8');
  const walk = dir => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) { walk(full); continue; }
      if (!entry.name.endsWith('.md')) continue;
      const relative = path.relative(repo, full);
      if (relative === 'docs/README.md') continue;
      if (!index.includes(path.relative(path.join(repo, 'docs'), full)))
        finding('unindexed-doc', 'docs/README.md', 1, relative,
          'page exists but the documentation index does not link it');
    }
  };
  walk(path.join(repo, 'docs'));
}

const byKind = {};
for (const item of findings) byKind[item.kind] = (byKind[item.kind] || 0) + 1;

if (process.argv.includes('--json')) {
  console.log(JSON.stringify({ findings, counts: byKind }, null, 2));
} else {
  console.log(`ground truth:`);
  // Name the image. An audit that does not say what it checked against
  // cannot be trusted when it reports nothing.
  console.log(`  runtime             : ${path.relative(archive, runtime) || '(none)'}`);
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
process.exit(findings.length === 0 ? 0 : 1);
