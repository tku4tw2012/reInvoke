---
title: Storage policy
description: Public source, private firmware retention, and historical storage measurements
ms.date: 2026-09-12
---

## Current publication boundary

Git holds authored source, documentation, acquisition metadata, and the
preserved small vendor evidence layer under its original terms. Firmware
packages, generated images, captures, credentials, and deployment manifests
remain in the private operator archive. No releases exist in this repository,
verified through GitHub's release list and API on 2026-09-12 UTC.
The public vendor-input source is
[coggy9/HKHacking releases](https://github.com/coggy9/HKHacking/releases), not a
reInvoke release. The custom image is not published.

## The problem

The acquisition-era working set was measured at approximately 4.9 GB; that is
not the size of the later native-build archive. Research value is concentrated in a very
small fraction of those bytes. Committing the bulk to Git would make it permanent in
history, bloat every clone forever, and buy nothing.

## Historical acquisition measurements

These are retained acquisition-stage measurements, not a fresh storage census
or a recommendation to repeat the experimental Git commit.

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

The decisive example: `gen-cmd.sh` is 596 bytes and yields a generic vendor NAND
map, serial-console configuration, and a recovery boot path. Its sizes sum to
512 MiB, but that does not establish this unit's physical geometry. Live
identification and complete logical reads instead established 256 MiB NAND.
The original script remains unchanged; this annotation corrects its earlier
interpretation.

## The three tiers

### Tier 1: public source and documentation

Research corpus, acquisition manifest, retention ranking, this policy, provenance
sidecars with SHA-256 values, authored runtime and acquisition tooling, the extracted text layer, and full
`unzip -l` listings of both bundles.

The complete bundle structure is therefore documented and greppable in Git without
the bytes being present.

### Tier 2: retained inputs

`Harman.Kardon.INVOKE.Flashing.zip`, `Harman.Kardon.INVOKE.Driver.OTA2.zip`, and the
standalone `83_IMAGE`.

These acquired firmware packages remain private and must not be committed or
published as release assets. Retained upstream source archives also belong
outside the Git working tree, with their own licences and hashes.

The original preservation rationale remains valid: a Git mirror or fork does
not copy release assets. It does not establish that no other public archive
exists, or grant rights to republish proprietary material.

Git LFS is not used for these private inputs. Historical quota estimates are
not current service pricing and are not a publication rationale.

### Tier 3: private working set and cold storage

The operator-managed archive includes Git mirrors, later native build products,
and evidence bundles. The retained cold-storage configuration uses Azure Blob
Storage; the table describes that configuration, not resources supplied by a
public clone.

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

Acquired inputs are indexed by corresponding sidecars in
[`metadata/`](../../metadata). Later native artifacts are bound by private build
and evidence manifests. The public sidecars are not a complete Tier 3 inventory.

### Why Cool, and not Archive

Historical acquisition-era estimate for the then-4.9 GB working set, retained
to explain the original choice rather than quote present prices:

| Tier | Minimum retention | Access | Annual cost |
|---|---|---|---|
| Hot | none | instant | $1.00 |
| **Cool** | **30 days** | **instant** | **$0.59** |
| Cold | 90 days | instant | $0.21 |
| Archive | 180 days | offline, up to 15 h to rehydrate | $0.06 |

In that estimate, the spread was under $1/year. Archive would save roughly $0.53/year while
imposing a 180-day retention commitment and a rehydration wait of up to 15 hours
(under 1 hour at high priority, capped at 10 GiB/hour per storage account) every time
the firmware needs to be examined.

For a project whose purpose is repeatedly analysing these images, that is a poor
trade. Cool tier is chosen deliberately: instant access, negligible cost, and no
offline retrieval requirement. Minimum retention and early-deletion charges
still apply; the table is not a current billing guarantee.

The earlier plan suggested copying rather than retiering dormant data.
Copying preserves the source but does not waive minimum-retention charges
if that source is deleted early. Any later storage migration needs its own
current service and cost review.

## Invariants

1. **Originals are never repacked, recompressed, or modified.** Byte-for-byte
   preservation is the policy; recorded SHA-256 values are the integrity anchor.
2. **Acquisition is not execution approval.** Acquisition tooling downloads,
   hashes, extracts, indexes, and documents. Separate offline builders and
   explicitly approved hardware trials now exist; neither authorizes another
   NAND operation. Image 99 remains excluded.
3. **No credentials or signed URLs in Git.** Presigned download URLs expire and may
   embed signature tokens; sidecars retain the stable public `source_url` and redact
   signed query strings.
4. **Tier 1 stays small.** If a proposed addition is large and opaque, it belongs in
   Tier 2 or 3 with a hash recorded here instead.

## Historical publication decision and current correction

Open-source drops retain their respective licences and any redistribution
conditions; a package name alone is not a licence determination.

The acquisition-stage policy proposed publicly mirroring the proprietary
Invoke bundles with attribution to
[coggy9/HKHacking](https://github.com/coggy9/HKHacking), on preservation grounds.
That proposal and dated upload records explain the earlier documentation;
they do not describe an available mirror today.

The current policy is private retention, not firmware publication. Public
availability at the upstream source is not permission to redistribute, and
the project MIT licence does not relicense vendor material. Preserve original
notices and hashes without rewriting extracted originals.

## A note on external custodians

Uploading the non-proprietary material to the Internet Archive or ensuring coverage by
Software Heritage advances the underlying goal directly: it makes *other parties* hold
the material, which is more durable than any single private copy.

See [source-retention-ranking.md](source-retention-ranking.md) for which artifacts
already have external custodians and which currently depend on this archive alone.
