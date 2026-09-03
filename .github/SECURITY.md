---
title: Security policy
description: Private vulnerability reporting guidance for the public reInvoke repository
ms.date: 2026-09-03
ms.topic: reference
---

## Reporting a vulnerability

Do not open a public issue for a suspected credential, private identifier, or
security vulnerability. Use the repository's **Security** tab to submit a
private vulnerability report.

Include the affected file or commit, the impact, and enough reproduction detail
to validate the report. Redact live credentials and personal identifiers.

## Repository data policy

Only source, documentation, generic configuration examples, hashes, and
sanitized metadata belong in Git. Keep credentials, Azure resource coordinates,
hardware identifiers, bond data, packet captures, and proprietary payloads in
private external storage.
