# Azure Archive Restore Runbook

This runbook restores the cold archive without placing bulk artifacts in Git.
Use Microsoft Entra authentication through Azure CLI or an equivalent managed
identity. Do not copy storage keys, SAS URLs, or credentials into this
repository.

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

Use the repository metadata sidecars as the integrity authority. For each
restored artifact, compare the recorded size and SHA-256:

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
