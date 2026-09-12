// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";
import GithubSlugger from "github-slugger";
import MarkdownIt from "markdown-it";
import { parseDocument as parseYaml } from "yaml";

const scriptPath = fileURLToPath(import.meta.url);
const defaultRoot = path.resolve(path.dirname(scriptPath), "..");
const markdown = new MarkdownIt({ html: true });
// Inspect unsupported URI schemes too, rather than silently treating them as text.
markdown.validateLink = () => true;

const structuralRules = [
  {
    pattern: /-----BEGIN (?:RSA |EC |DSA |OPENSSH |ENCRYPTED )?PRIVATE KEY-----\r?\n(?:[A-Za-z0-9+/=]{32,}\r?\n)+-----END (?:RSA |EC |DSA |OPENSSH |ENCRYPTED )?PRIVATE KEY-----/,
    message: "Private-key material",
  },
  {
    pattern: /\bhttps?:\/\/[^/:\s<>"']+:[^/@\s<>"']+@/i,
    message: "Credential-bearing URL",
  },
  {
    pattern: /\bAccountKey=[A-Za-z0-9+/]{40,}={0,2}/,
    message: "Storage account key",
  },
  {
    pattern: /[?&]sig=[A-Za-z0-9%+/=_-]{32,}/,
    message: "Signed access URL",
  },
];

function isWithin(root, candidate) {
  const relative = path.relative(root, candidate);
  return relative === "" ||
    (relative !== ".." && !relative.startsWith(`..${path.sep}`) &&
      !path.isAbsolute(relative));
}

function gitPath(name) {
  return name.split(path.sep).join("/");
}

export function loadPrivateRules(root, filename) {
  if (!filename) return [];
  const absolute = path.resolve(filename);
  const resolved = fs.realpathSync(absolute);
  if (isWithin(root, absolute) || isWithin(root, resolved)) {
    throw new Error("Private matching rules must be stored outside the repository");
  }
  let values;
  try {
    values = JSON.parse(fs.readFileSync(resolved, "utf8"));
  } catch (error) {
    if (!(error instanceof SyntaxError)) throw error;
    throw new Error("Private matching rules contain invalid JSON");
  }
  if (!Array.isArray(values) || values.length === 0) {
    throw new Error("Private matching rules must be a nonempty JSON array");
  }
  return values.map((rule, index) => {
    if (rule === null || typeof rule !== "object" ||
        typeof rule.pattern !== "string" || rule.pattern.length === 0 ||
        (rule.flags !== undefined &&
          (typeof rule.flags !== "string" || !/^[imu]*$/.test(rule.flags)))) {
      throw new Error(`Invalid private matching rule ${index + 1}`);
    }
    let pattern;
    try {
      pattern = new RegExp(rule.pattern, rule.flags ?? "");
    } catch (error) {
      if (!(error instanceof SyntaxError)) throw error;
      throw new Error(`Invalid expression in private matching rule ${index + 1}`);
    }
    return { pattern, message: `Private matching rule ${index + 1}` };
  });
}

function headingText(tokens) {
  return tokens.map(token => {
    if (token.children) return headingText(token.children);
    if (["text", "code_inline"].includes(token.type)) return token.content;
    if (["softbreak", "hardbreak"].includes(token.type)) return " ";
    return "";
  }).join("");
}

export function parseDocument(content) {
  const issues = [];
  const anchors = new Set();
  const links = [];
  const diagrams = [];
  const frontmatter = content.match(/^---\r?\n([\s\S]*?)\r?\n---[ \t]*(?:\r?\n|$)/);
  let body = content;
  if (!frontmatter) {
    issues.push({ line: 1, code: "FRONTMATTER", message: "Missing or unclosed frontmatter" });
  } else {
    const metadata = parseYaml(frontmatter[1]);
    if (metadata.errors.length) {
      issues.push({ line: 1, code: "FRONTMATTER", message: "Invalid YAML frontmatter" });
    } else {
      const fields = metadata.toJS();
      for (const key of ["title", "description"]) {
        if (typeof fields?.[key] !== "string" || !fields[key].trim()) {
          issues.push({ line: 1, code: "FRONTMATTER", message: `Missing nonempty ${key}` });
        }
      }
    }
    body = "\n".repeat(frontmatter[0].split("\n").length - 1) +
      content.slice(frontmatter[0].length);
  }

  const tokens = markdown.parse(body, {});
  const slugger = new GithubSlugger();
  const lines = content.split(/\r?\n/);
  function visit(token, parentLine = 1) {
    const line = token.map ? token.map[0] + 1 : parentLine;
    if (token.type === "link_open") {
      links.push({ target: token.attrGet("href"), line });
    } else if (token.type === "image") {
      links.push({ target: token.attrGet("src"), line });
    } else if (["html_block", "html_inline"].includes(token.type)) {
      const html = token.content.replace(/<!--[\s\S]*?-->/g, "");
      for (const tag of html.matchAll(/<[A-Za-z][^>]*>/g)) {
        for (const match of tag[0].matchAll(/\b([A-Za-z_:][A-Za-z0-9_.:-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))/g)) {
          const name = match[1].toLowerCase();
          const value = markdown.utils.unescapeAll(match[2] ?? match[3] ?? match[4]);
          if (["id", "name"].includes(name)) anchors.add(value);
          else if (["href", "src"].includes(name)) links.push({ target: value, line });
        }
      }
    } else if (token.type === "fence") {
      const closing = lines[token.map[1] - 1] ?? "";
      const marker = token.markup[0];
      if (!new RegExp(`^\\s*${marker}{${token.markup.length},}\\s*$`).test(closing)) {
        issues.push({ line, code: "FENCE", message: "Unclosed fenced code block" });
      }
      if (token.info.trim() === "mermaid") {
        diagrams.push({ line, source: token.content });
      }
    }
    for (const child of token.children ?? []) visit(child, line);
  }
  for (let index = 0; index < tokens.length; index++) {
    const token = tokens[index];
    if (token.type === "heading_open") {
      anchors.add(slugger.slug(headingText(tokens[index + 1].children ?? [])));
      if (frontmatter && token.tag === "h1") {
        issues.push({
          line: token.map[0] + 1,
          code: "HEADING",
          message: "Use H2 or below after the frontmatter title",
        });
      }
    }
    visit(token);
  }
  return { issues, anchors, links, diagrams };
}

export function validateRepository(rootDir = defaultRoot, { privatePatternsFile } = {}) {
  const root = fs.realpathSync(rootDir);
  const files = [...new Set(execFileSync("git", ["ls-files", "--cached", "-z"], {
    cwd: root,
    encoding: "utf8",
  }).split("\0").filter(Boolean))].sort();
  const tracked = new Set(files);
  const privateRules = loadPrivateRules(root, privatePatternsFile);
  const documents = new Map();
  const issues = [];
  let textFiles = 0;
  let binaryFiles = 0;
  let localLinks = 0;
  let fragmentLinks = 0;
  const add = (file, line, code, message) => issues.push({ file, line, code, message });

  function publicTarget(absolute, file, line) {
    if (!isWithin(root, absolute)) {
      add(file, line, "LINK_OUTSIDE", "Target leaves the public repository");
      return null;
    }
    if (!fs.existsSync(absolute)) {
      add(file, line, "LINK_MISSING", "Public target does not exist");
      return null;
    }
    const real = fs.realpathSync(absolute);
    if (!isWithin(root, real)) {
      add(file, line, "LINK_OUTSIDE", "Symlink target leaves the public repository");
      return null;
    }
    const relative = gitPath(path.relative(root, real));
    if (fs.statSync(real).isDirectory()) {
      if (!files.some(name => relative === "" || name.startsWith(`${relative}/`))) {
        add(file, line, "LINK_UNTRACKED", "Directory contains no tracked public files");
        return null;
      }
    } else if (!tracked.has(relative)) {
      add(file, line, "LINK_UNTRACKED", "Target is not a tracked public file");
      return null;
    }
    return { real, relative };
  }

  for (const file of files) {
    const target = publicTarget(path.resolve(root, file), file, 1);
    if (!target) continue;
    if (!fs.statSync(target.real).isFile()) {
      add(file, 1, "FILE", "Tracked entry is not a regular file");
      continue;
    }
    const bytes = fs.readFileSync(target.real);
    if (bytes.includes(0)) {
      binaryFiles++;
      if (/\.(?:md|[cm]?js|[cm]?ts|json|ya?ml|sh|go|c|h|py|txt|patch|conf|toml)$/i.test(file)) {
        add(file, 1, "FILE", "Text source contains a NUL byte and requires manual review");
      }
      continue;
    }
    textFiles++;
    const content = bytes.toString("utf8");
    for (const rule of [...structuralRules, ...privateRules]) {
      const match = rule.pattern.exec(content);
      if (match) {
        add(file, content.slice(0, match.index).split("\n").length,
          "PRIVACY", rule.message);
      }
    }
    if (file.toLowerCase().endsWith(".md")) {
      const parsed = parseDocument(content);
      documents.set(file, parsed);
      for (const issue of parsed.issues) issues.push({ file, ...issue });
    }
  }

  for (const [file, document] of documents) {
    for (const { target, line } of document.links) {
      if (!target || target === "#") {
        add(file, line, "LINK_EMPTY", "Replace the empty link placeholder");
        continue;
      }
      if (/^(?:https?:\/\/|mailto:|tel:|ftp:\/\/|git:\/\/|ssh:\/\/|\/\/)/i.test(target)) continue;
      if (/^[a-z][a-z0-9+.-]*:/i.test(target) || path.isAbsolute(target)) {
        add(file, line, "LINK_SCHEME", "Use a public URL or a repository-relative link");
        continue;
      }
      localLinks++;
      const hashIndex = target.indexOf("#");
      const rawPath = (hashIndex < 0 ? target : target.slice(0, hashIndex)).split("?")[0];
      let targetPath;
      let fragment;
      try {
        targetPath = decodeURIComponent(rawPath);
        fragment = hashIndex < 0 ? "" : decodeURIComponent(target.slice(hashIndex + 1));
      } catch (error) {
        if (!(error instanceof URIError)) throw error;
        add(file, line, "LINK_ENCODING", "Malformed URL escape in link");
        continue;
      }
      const absolute = targetPath
        ? path.resolve(root, path.dirname(file), targetPath)
        : path.resolve(root, file);
      const resolved = publicTarget(absolute, file, line);
      if (!resolved || !fragment || !resolved.relative.toLowerCase().endsWith(".md")) continue;
      fragmentLinks++;
      const targetDocument = documents.get(resolved.relative);
      if (!targetDocument?.anchors.has(fragment)) {
        add(file, line, "LINK_ANCHOR", "Fragment does not name a public Markdown anchor");
      }
    }
  }
  return {
    issues,
    trackedFiles: files.length,
    textFiles,
    binaryFiles,
    markdownFiles: documents.size,
    localLinks,
    fragmentLinks,
    diagrams: [...documents].flatMap(([file, document]) =>
      document.diagrams.map(diagram => ({ file, ...diagram }))),
    privateRuleCount: privateRules.length,
  };
}

function main() {
  const { values } = parseArgs({
    options: {
      root: { type: "string" },
      "private-patterns": { type: "string" },
      help: { type: "boolean", short: "h" },
    },
  });
  if (values.help) {
    console.log("node scripts/validate-docs.mjs [--root CHECKOUT] [--private-patterns EXTERNAL_JSON]");
    return;
  }
  const result = validateRepository(values.root ?? defaultRoot, {
    privatePatternsFile: values["private-patterns"],
  });
  for (const issue of result.issues) {
    console.error(`${issue.file}:${issue.line} [${issue.code}] ${issue.message}`);
  }
  console.log(`Checked ${result.markdownFiles} tracked Markdown files, ${result.textFiles} tracked text files, ${result.localLinks} local links and ${result.fragmentLinks} fragments.`);
  console.log(`${result.diagrams.length} Mermaid diagrams require separate parser/render validation; ${result.binaryFiles} binary files were not content-scanned.`);
  console.log(`Privacy: ${structuralRules.length} structural rules and ${result.privateRuleCount} private rules. Git history and unknown private identifiers are not covered.`);
  console.log(`Errors: ${result.issues.length}`);
  if (result.issues.length) process.exitCode = 1;
}

if (process.argv[1] && path.resolve(process.argv[1]) === scriptPath) {
  try {
    main();
  } catch (error) {
    console.error(error instanceof Error ? error.message : "Documentation validation failed");
    process.exitCode = 1;
  }
}
