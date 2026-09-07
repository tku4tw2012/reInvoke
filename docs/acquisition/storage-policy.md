# Storage Policy

How this project decides what lives in Git, what lives in cold storage, and why.

## The problem

The acquired working set is ~4.9 GB. The research value is concentrated in a very
small fraction of those bytes. Committing the bulk to Git would make it permanent in
history, bloat every clone forever, and buy nothing.

## Measured facts

These were tested directly, not assumed.

| Test | Result | Implication |
|---|---|---|
| `zstd -19` on 30 MB of `Flashing.zip` | 30.0 MB → 30.0 MB | Already deflate-compressed; recompression is pointless |
| `gzip -1` on first 20 MB of `83_IMAGE` | 97% of input | Image is internally compressed / high entropy |
| Binary delta between the two `83_IMAGE` variants | 83,800,608 of 107,934,810 bytes differ (77.64%) | Delta encoding is not viable |
| Commit `83_IMAGE` to Git, then `git gc --aggressive` | 108 MB → 99 M pack | Git barely helps, and the cost is permanent |
| `Mrvl_WinUSB_Driver_040114/` in both bundles | Byte-identical, 27 MB each | Dedup possible, but rejected: it would break byte-for-byte originals |

**Conclusion:** the payload is incompressible by design. The fix is architectural,
not compression.

## Content distribution

| Bundle | Small files (<128 KB) | Large blobs |
|---|---|---|
| `Harman.Kardon.INVOKE.Flashing.zip` | 24 files, 321 KB | 29 files, 612.6 MB |
| `Harman.Kardon.INVOKE.Driver.OTA2.zip` | 24 files, 322 KB | 29 files, 539.3 MB |

Roughly 0.05% of the bytes carry nearly all of the human-readable engineering content.

The decisive example: `gen-cmd.sh` is 596 bytes and yields the complete NAND partition
map, the serial console configuration, and a recovery boot path. Its partition sizes sum
to exactly 512 MB, establishing the flash size by derivation.

## The three tiers

### Tier 1 — Git (this repository, ~480 KB)

Research corpus, acquisition manifest, retention ranking, this policy, provenance
sidecars with SHA-256 values, acquisition tooling, the extracted text layer, and full
`unzip -l` listings of both bundles.

The complete bundle structure is therefore documented and greppable in Git without
the bytes being present.

### Tier 2 — Firmware bundles (569 MB)

`Harman.Kardon.INVOKE.Flashing.zip`, `Harman.Kardon.INVOKE.Driver.OTA2.zip`, and the
standalone `83_IMAGE`.

All three exceed GitHub's hard 100 MB limit for tracked files and must never be
committed. They are published instead as **GitHub Releases**, which allows up to 2 GB
per file, consumes no LFS quota, and can be removed later without rewriting history.

This also mirrors the failure mode being insured against: release assets are precisely
the layer that no external archive preserves, so republishing them here creates a second
independent custodian. Attribution to the originating project is recorded in the README.

**Git LFS is the wrong tool here.** The free tier provides 1 GB of storage *and*
1 GB/month of bandwidth; a 569 MB payload would exhaust the bandwidth allowance almost
immediately. Release assets consume no LFS quota.

### Tier 3 — Cold storage (full 4.9 GB working set)

Azure Blob Storage, including the Git mirrors.

| Setting | Value |
|---|---|
| Resource group | `<resource-group>` |
| Storage account | `<storage-account>` |
| Container | `<container>` |
| Region | `<azure-region>` |
| Redundancy | Standard LRS |
| Access tier | Cool |
| Public blob access | Disabled |
| Transport | HTTPS only, TLS 1.2 minimum |
| Authentication | Microsoft Entra ID (no shared keys) |

Blobs are indexed by the SHA-256 values already recorded in [`../../metadata/`](../../metadata),
so Tier 1 remains the authoritative catalogue of everything held in Tier 3.

### Why Cool, and not Archive

Measured example cost for the 4.9 GB working set:

| Tier | Minimum retention | Access | Annual cost |
|---|---|---|---|
| Hot | none | instant | $1.00 |
| **Cool** | **30 days** | **instant** | **$0.59** |
| Cold | 90 days | instant | $0.21 |
| Archive | 180 days | offline, up to 15 h to rehydrate | $0.06 |

The entire spread is under $1/year. Archive would save roughly $0.53/year while
imposing a 180-day retention commitment and a rehydration wait of up to 15 hours
(under 1 hour at high priority, capped at 10 GiB/hour per storage account) every time
the firmware needs to be examined.

For a project whose purpose is repeatedly analysing these images, that is a poor
trade. Cool tier is chosen deliberately: instant access, negligible cost, and no
retention trap.

If the project later goes dormant, migrate using `Copy Blob` rather than
`Set Blob Tier`, which leaves the source blob in place and avoids the early-deletion
penalty.

## Invariants

1. **Originals are never repacked, recompressed, or modified.** Byte-for-byte
   preservation is the policy; recorded SHA-256 values are the integrity anchor.
2. **No artifact is executed or flashed.** Tooling may download, mirror, hash,
   extract, index, and document only.
3. **No credentials or signed URLs in Git.** Presigned download URLs expire and may
   embed signature tokens; sidecars retain the stable public `source_url` and redact
   signed query strings.
4. **Tier 1 stays small.** If a proposed addition is large and opaque, it belongs in
   Tier 2 or 3 with a hash recorded here instead.

## Decision: publishing proprietary material

The GPL/open-source drops (for example the Harman Citation package and the Google/Nest
source trees) are freely redistributable under their respective licences.

The Harman Invoke firmware bundles are **proprietary**. The decision taken for this
project is to mirror them publicly with clear attribution to
[coggy9/HKHacking](https://github.com/coggy9/HKHacking), on preservation grounds: the
material is already public, and release assets have no external custodian.

The residual risk is a takedown request, which would remove the public mirror but not
the archived copies. Rights holders may request removal.

## A note on external custodians

Uploading the non-proprietary material to the Internet Archive or ensuring coverage by
Software Heritage advances the underlying goal directly: it makes *other parties* hold
the material, which is more durable than any single private copy.

See [source-retention-ranking.md](source-retention-ranking.md) for which artifacts
already have external custodians and which currently depend on this archive alone.
