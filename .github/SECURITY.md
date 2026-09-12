---
title: Security policy
description: Private reporting, runtime trust boundaries and repository data checks
ms.date: 2026-09-12
ms.topic: reference
---

## Reporting a vulnerability

Use the repository Security tab for private vulnerability reports. If private
reporting is unavailable, request a private contact through a content-free
issue before sending details.

Include affected commit/candidate/tool, impact, reproduction and evidence
scope: source, offline test, RAM or native execution. Keep credentials,
identifiers and unredacted captures out of public issues.

## Runtime security boundary

reInvoke is experimental, unit-specific firmware. The
[contract](../docs/current-product-contract.md) separates policy from acceptance.

* WAMP is unauthenticated; its network boundary depends on firewall policy.
* Candidate 03 reached pinned SSH negotiation, not successful native login.
* Provisioning trusts a physical setup window and AP-delivered TLS fingerprint,
  not independent out-of-band identity.
* Microphone privacy trusts root and owned software. The polled capture gate
  cannot revoke queued audio and is not an electrical disconnect.
* Observed recovery does not establish recovery from arbitrary boot-chain damage.

## Repository data policy

Public Git contains authored material, generic examples, hashes and sanitized
metadata. Keep credentials, real network/radio identifiers, bonds, operator
SSH keys/pins, private cloud coordinates, captures and proprietary payloads
outside the public tree. See [storage policy](../docs/acquisition/storage-policy.md).

Use dedicated deployment keys; do not disable host-key verification to bypass
authentication failures. Rotate exposed credentials through their owner.
Deleting current-tree content does not remove it from Git history.

## Repository checks

After installing the [checker dependencies](../scripts/package.json), run:

```bash
node scripts/validate-docs.mjs
node scripts/validate-docs.mjs --private-patterns "$PRIVATE_DOC_RULES"
```

`PRIVATE_DOC_RULES` names a JSON file outside the checkout: a nonempty array of
objects containing a regex `pattern` and optional `flags` (`i`, `m`, `u`).
Keep real identifiers there. Diagnostics print rule numbers and locations,
not matched private values.

The checker reads Git-tracked working-tree text, including source/JSON,
checks frontmatter and Markdown fragments, and rejects local links to missing,
untracked or outside targets. Built-in credential patterns are selective.
It does not scan history, private archives or binaries, check external URL
availability, render Mermaid or recognize arbitrary unknown personal data.
External private rules and human review remain necessary.
