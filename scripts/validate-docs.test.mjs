// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync, spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { loadPrivateRules, parseDocument, validateRepository } from "./validate-docs.mjs";

const script = fileURLToPath(new URL("./validate-docs.mjs", import.meta.url));
const header = "---\ntitle: Fixture\ndescription: Synthetic validation fixture\n---\n\n";
const document = body => header + body + "\n";

function fixture(t) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "reinvoke-docs-check-"));
  const root = path.join(directory, "checkout");
  fs.mkdirSync(root);
  execFileSync("git", ["init", "--quiet", root]);
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  function write(name, contents, tracked = true) {
    const destination = path.resolve(root, name);
    fs.mkdirSync(path.dirname(destination), { recursive: true });
    fs.writeFileSync(destination, contents);
    if (tracked) execFileSync("git", ["add", "--", name], { cwd: root });
    return destination;
  }
  function privateRules(pattern = "SYNTHETIC_PRIVATE_MARKER") {
    const destination = path.join(directory, "private-patterns.json");
    fs.writeFileSync(destination, JSON.stringify([{ pattern }]));
    return destination;
  }
  return { directory, root, write, privateRules };
}

test("accepts tracked inline, reference, image, directory and explicit-anchor links", t => {
  const f = fixture(t);
  f.write("README.md", document([
    "[Inline](docs/guide.md#heading-with-code)",
    "[Reference][guide] and [guide][] and [guide]",
    "[HTML](docs/guide.md#stable) and [directory](docs/)",
    "![Image](pixel.dat) and [external](https://example.org/)",
    "",
    "[guide]: docs/guide.md#heading-with-code",
  ].join("\n")));
  f.write("docs/guide.md", document('## Heading with `code`\n\n<a id="stable"></a>'));
  f.write("pixel.dat", Buffer.from([0, 1, 2, 3]));
  const result = validateRepository(f.root);
  assert.deepEqual(result.issues, []);
  assert.equal(result.markdownFiles, 2);
  assert.equal(result.binaryFiles, 1);
  assert.equal(result.localLinks, 7);
  assert.equal(result.fragmentLinks, 5);
});

test("uses GitHub heading slugs, duplicate suffixes and formatted heading text", () => {
  const result = parseDocument(document([
    "## Plain *text* and [link](https://example.org)",
    "## Repeated",
    "## Repeated",
    "## A & B",
    "<a name=\"old-anchor\"></a>",
  ].join("\n")));
  assert.deepEqual([...result.anchors], [
    "plain-text-and-link", "repeated", "repeated-1", "a--b", "old-anchor",
  ]);
});

test("ignores link examples in code and reports Mermaid for separate rendering", () => {
  const result = parseDocument(document([
    "`[Not a link](missing.md)`",
    "```text",
    "[Not a link](missing.md)",
    "```",
    "```mermaid",
    'flowchart LR\n    A["Start"] --> B["End"]',
    "```",
  ].join("\n")));
  assert.deepEqual(result.issues, []);
  assert.deepEqual(result.links, []);
  assert.equal(result.diagrams.length, 1);
  assert.match(result.diagrams[0].source, /flowchart LR/);
});

test("rejects missing frontmatter, required fields, H1 and unclosed fences", () => {
  for (const content of [
    "## No frontmatter\n",
    "---\ntitle: Missing end\n",
    "---\ntitle: Only title\n---\n",
    document("# Duplicate title"),
    document("```text\nUnclosed"),
  ]) {
    assert.ok(parseDocument(content).issues.length > 0);
  }
});

test("validates YAML syntax and string values, not just field-name substrings", () => {
  for (const frontmatter of [
    "title: [unclosed\ndescription: Fixture",
    'title: "" # empty\ndescription: Fixture',
    "title: 42\ndescription: Fixture",
    "title: First\ntitle: Duplicate\ndescription: Fixture",
    "title: Fixture\ndescription: >-\n",
  ]) {
    const result = parseDocument(`---\n${frontmatter}\n---\n`);
    assert.ok(result.issues.some(issue => issue.code === "FRONTMATTER"));
  }
  assert.deepEqual(parseDocument("---\ntitle: Fixture\ndescription: >-\n  Folded text\n---\n").issues, []);
});

test("does not invent anchors or links from HTML comments or quoted attribute text", () => {
  const result = parseDocument(document([
    '<!-- <a id="comment" href="missing.md"></a> -->',
    '<a title="id=\'not-an-anchor\' href=\'missing.md\'" id="real"></a>',
  ].join("\n")));
  assert.deepEqual([...result.anchors], ["real"]);
  assert.deepEqual(result.links, []);
});

test("handles encoded spaces, parentheses and link titles", t => {
  const f = fixture(t);
  f.write("README.md", document('[Target](<docs/a (b).md#target> "A title")'));
  f.write("docs/a (b).md", document("## Target"));
  assert.deepEqual(validateRepository(f.root).issues, []);
});

test("rejects nonexistent inline and reference-style destinations", t => {
  const f = fixture(t);
  f.write("README.md", document("[Inline](missing.md)\n\n[Ref][missing]\n\n[missing]: absent.md"));
  const issues = validateRepository(f.root).issues;
  assert.equal(issues.length, 2);
  assert.ok(issues.every(issue => issue.code === "LINK_MISSING"));
});

test("rejects missing same-file and cross-file fragments", t => {
  const f = fixture(t);
  f.write("README.md", document("[Self](#absent) and [target](target.md#absent)"));
  f.write("target.md", document("## Present"));
  const issues = validateRepository(f.root).issues;
  assert.equal(issues.length, 2);
  assert.ok(issues.every(issue => issue.code === "LINK_ANCHOR"));
});

test("rejects empty Markdown link placeholders", t => {
  const f = fixture(t);
  f.write("README.md", document("[Empty]() and [Placeholder](#)"));
  assert.deepEqual(validateRepository(f.root).issues.map(issue => issue.code),
    ["LINK_EMPTY", "LINK_EMPTY"]);
});

test("rejects a link to an existing untracked file rather than inspecting it", t => {
  const f = fixture(t);
  f.write("README.md", document("[Private](private.md)"));
  f.write("private.md", "SYNTHETIC_PRIVATE_MARKER", false);
  const result = validateRepository(f.root, { privatePatternsFile: f.privateRules() });
  assert.equal(result.markdownFiles, 1);
  assert.deepEqual(result.issues.map(issue => issue.code), ["LINK_UNTRACKED"]);
});

test("rejects an existing untracked directory and a deleted tracked file", t => {
  const f = fixture(t);
  f.write("README.md", document("[Directory](private/)"));
  f.write("private/data.txt", "private fixture", false);
  f.write("deleted.md", document("## Deleted"));
  fs.unlinkSync(path.join(f.root, "deleted.md"));
  assert.deepEqual(validateRepository(f.root).issues.map(issue => issue.code).sort(),
    ["LINK_MISSING", "LINK_UNTRACKED"]);
});

test("rejects an existing outside target for inline and reference links", t => {
  const f = fixture(t);
  f.write("../outside.md", "SYNTHETIC_PRIVATE_MARKER", false);
  f.write("README.md", document("[Inline](../outside.md)\n\n[Ref][r]\n\n[r]: ../outside.md"));
  const result = validateRepository(f.root, { privatePatternsFile: f.privateRules() });
  assert.deepEqual(result.issues.map(issue => issue.code), ["LINK_OUTSIDE", "LINK_OUTSIDE"]);
});

test("rejects tracked symlinks outside the repository without scanning their contents", t => {
  const f = fixture(t);
  const outside = f.write("../outside.md", "SYNTHETIC_PRIVATE_MARKER", false);
  fs.symlinkSync(outside, path.join(f.root, "escape.md"));
  execFileSync("git", ["add", "--", "escape.md"], { cwd: f.root });
  f.write("README.md", document("[Escape](escape.md)"));
  const result = validateRepository(f.root, { privatePatternsFile: f.privateRules() });
  assert.equal(result.issues.length, 2);
  assert.ok(result.issues.every(issue => issue.code === "LINK_OUTSIDE"));
});

test("rejects links through directory symlinks to outside files", t => {
  const f = fixture(t);
  f.write("../outside/doc.md", document("## External"), false);
  fs.symlinkSync(path.join(f.directory, "outside"), path.join(f.root, "external"));
  f.write("README.md", document("[Escape](external/doc.md)"));
  assert.equal(validateRepository(f.root).issues[0].code, "LINK_OUTSIDE");
});

test("accepts a symlink only when its resolved target is tracked and internal", t => {
  const f = fixture(t);
  f.write("target.md", document("## Target"));
  fs.symlinkSync("target.md", path.join(f.root, "alias.md"));
  execFileSync("git", ["add", "--", "alias.md"], { cwd: f.root });
  f.write("README.md", document("[Alias](alias.md#target)"));
  assert.deepEqual(validateRepository(f.root).issues, []);
});

test("checks HTML href and src attributes, not only Markdown links", t => {
  const f = fixture(t);
  f.write("README.md", document('<a href="missing.md">Link</a>\n\n<img src="absent.png">'));
  assert.equal(validateRepository(f.root).issues.length, 2);
});

test("rejects local absolute paths, filesystem URLs and unsafe URI schemes", t => {
  const f = fixture(t);
  f.write("README.md", document([
    "[Absolute](/tmp/local.md)",
    "[File](file:///tmp/local.md)",
    "[Unsafe](javascript:alert%281%29)",
  ].join("\n")));
  assert.deepEqual(validateRepository(f.root).issues.map(issue => issue.code),
    ["LINK_SCHEME", "LINK_SCHEME", "LINK_SCHEME"]);
});

test("scans tracked JSON and script sources using external private rules", t => {
  const f = fixture(t);
  f.write("README.md", document("## Public"));
  f.write("metadata.json", JSON.stringify({ value: "SYNTHETIC_PRIVATE_MARKER" }));
  f.write("source.mjs", 'const value = "SYNTHETIC_PRIVATE_MARKER";\n');
  const result = validateRepository(f.root, { privatePatternsFile: f.privateRules() });
  assert.equal(result.textFiles, 3);
  assert.equal(result.privateRuleCount, 1);
  assert.deepEqual(result.issues.map(issue => issue.code), ["PRIVACY", "PRIVACY"]);
  assert.doesNotMatch(JSON.stringify(result.issues), /SYNTHETIC_PRIVATE_MARKER/);
});

test("does not scan unrelated untracked Markdown", t => {
  const f = fixture(t);
  f.write("README.md", document("## Public"));
  f.write("local-notes.md", "SYNTHETIC_PRIVATE_MARKER", false);
  const result = validateRepository(f.root, { privatePatternsFile: f.privateRules() });
  assert.deepEqual(result.issues, []);
  assert.equal(result.markdownFiles, 1);
});

test("does not silently omit a NUL-containing document or JSON source", t => {
  const f = fixture(t);
  f.write("README.md", Buffer.from(header + "\0"));
  f.write("metadata.json", Buffer.from("{}\0"));
  assert.deepEqual(validateRepository(f.root).issues.map(issue => issue.code),
    ["FILE", "FILE"]);
});

test("rejects actual private-key-shaped material in a tracked non-Markdown file", t => {
  const f = fixture(t);
  f.write("fixture.txt", [
    "-----BEGIN PRIVATE KEY-----",
    "A".repeat(64),
    "-----END PRIVATE KEY-----",
    "",
  ].join("\n"));
  assert.equal(validateRepository(f.root).issues[0].code, "PRIVACY");
});

test("rejects credential-bearing URLs and signed access tokens", t => {
  const f = fixture(t);
  const url = new URL("https://example.invalid/");
  url.username = "fixture";
  url.password = "not-a-real-password";
  f.write("urls.txt", [
    url.href,
    "https://example.invalid/?sig=" + "a".repeat(40),
    "AccountKey=" + "A".repeat(64),
  ].join("\n"));
  assert.equal(validateRepository(f.root).issues.length, 3);
});

test("refuses private rules stored inside the repository, including ignored files", t => {
  const f = fixture(t);
  const local = f.write("local-rules.json", JSON.stringify([{ pattern: "fixture" }]), false);
  assert.throws(() => loadPrivateRules(f.root, local), /outside the repository/);
});

test("refuses a private-rule symlink whose lexical path is inside the repository", t => {
  const f = fixture(t);
  const outside = f.privateRules();
  const alias = path.join(f.root, "alias.json");
  fs.symlinkSync(outside, alias);
  assert.throws(() => loadPrivateRules(f.root, alias), /outside the repository/);
});

test("reports invalid private JSON and expressions without exposing rule contents", t => {
  const f = fixture(t);
  const privateFile = f.privateRules();
  for (const contents of [
    "{ SYNTHETIC_PRIVATE_MARKER",
    JSON.stringify([{ pattern: "[SYNTHETIC_PRIVATE_MARKER" }]),
    JSON.stringify([{ pattern: "SYNTHETIC_PRIVATE_MARKER", flags: "g" }]),
    JSON.stringify([]),
  ]) {
    fs.writeFileSync(privateFile, contents);
    assert.throws(() => loadPrivateRules(f.root, privateFile), error => {
      assert.doesNotMatch(error.message, /SYNTHETIC_PRIVATE_MARKER/);
      return true;
    });
  }
});

test("CLI returns failure for invalid docs and success after the observed fix", t => {
  const f = fixture(t);
  f.write("README.md", document("[Link](target.md#correct)"));
  const args = [script, "--root", f.root];
  const failed = spawnSync(process.execPath, args, { encoding: "utf8" });
  assert.equal(failed.status, 1);
  assert.match(failed.stderr, /LINK_MISSING/);
  f.write("target.md", document("## Correct"));
  const passed = spawnSync(process.execPath, args, { encoding: "utf8" });
  assert.equal(passed.status, 0, passed.stderr);
  assert.match(passed.stdout, /Errors: 0/);
});

test("CLI privacy failure is redacted and survives changes of working directory", t => {
  const f = fixture(t);
  f.write("metadata.json", JSON.stringify({ value: "SYNTHETIC_PRIVATE_MARKER" }));
  const result = spawnSync(process.execPath, [
    script, "--root", f.root, "--private-patterns", f.privateRules(),
  ], { cwd: f.directory, encoding: "utf8" });
  assert.equal(result.status, 1);
  assert.match(result.stderr, /PRIVACY/);
  assert.doesNotMatch(result.stdout + result.stderr, /SYNTHETIC_PRIVATE_MARKER/);
});
