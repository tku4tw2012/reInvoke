---
title: Private Azure archive restore runbook
description: Operator-only restore boundary and verification of retained acquisition and native-build artifacts
---

## Scope

This runbook restores the cold archive without placing bulk artifacts in Git.
Use Microsoft Entra authentication through Azure CLI or an equivalent managed
identity. Do not copy storage keys, SAS URLs, or credentials into this
repository.
This requires the owner's existing private archive and permissions; a public
clone supplies neither. It is not a firmware download or native-image
installation recipe.

## Restore

Confirm the target is the sibling archive directory, then download the
private archive container:

```sh
STORAGE_ACCOUNT="<storage-account>"
CONTAINER="<container>"

az storage blob download-batch \
  --account-name "${STORAGE_ACCOUNT}" \
  --source "${CONTAINER}" \
  --destination ~/<workspace>/reinvoke-archive \
  --auth-mode login \
  --overwrite false
```

If the archive is being restored to a different location, replace only the
destination path. Do not use `--overwrite true` until the existing files have
been independently checked.

## Verify

Use the matching repository acquisition sidecar for each acquired original.
For later native builds and captures, use the associated private build or
evidence manifest; public metadata does not inventory the whole archive.
Compare the recorded size and SHA-256:

```sh
sha256sum ~/<workspace>/reinvoke-archive/originals/harman/invoke/83_IMAGE
```

The expected hash for that artifact is recorded in `metadata/`. Repeat for
all restored originals and any extracted artifacts required for analysis.
Investigate mismatches before using an artifact; do not repair a mismatch by
overwriting the preserved original.

Resolve the placeholders from private operator configuration. Do not add live
Azure resource coordinates to this public repository. Azure RBAC must grant
only the read permissions needed for a restore.
