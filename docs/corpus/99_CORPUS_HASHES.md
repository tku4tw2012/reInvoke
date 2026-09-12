---
title: Corpus file hashes and preserved snapshot
description: Current authored corpus digests alongside the unchanged earlier generated digest record
ms.date: 2026-09-12
---

## Documentation audit snapshot on 2026-09-12

These hashes cover the authored corpus text after the documentation audit.
They do not fingerprint vendor firmware, private captures, or source archives.

| File | SHA-256 |
|---|---|
| `docs/corpus/00_README.md` | `a1504661107b3d7c80981f7d2fe694fdd0a3dfaf24d3cd38f62dc66976b25b7c` |
| `docs/corpus/01_CANONICAL_HARDWARE_BASELINE.md` | `9697b5bdc7a4786162f0382279c21e4eee2c7da988fbd902cad0ce99336a2b99` |
| `docs/corpus/02_CLAIM_EVIDENCE_LEDGER.md` | `a151ba480e0abb103832589825ba082d98448e58582ec924be1372e12ef1bb23` |
| `docs/corpus/03_RESEARCH_FRAMEWORK_AND_CRITIQUE.md` | `a381f06f43c9534bc317d059341a57714a237765b53018c810e72a0211b1b49e` |
| `docs/corpus/04_FCC_EXHIBIT_INVENTORY.md` | `4bcbcf32624820342d8d29ff2fc263a6946a1fdb0758a397fef84620c7b19362` |
| `docs/corpus/05_SIBLING_SOURCE_CROSSINDEX.md` | `a220f201b7176fba1e1e6a8a9f5f28a9667c3663b0baf137a6c33130f9041b7c` |

## Preserved earlier generated snapshot

The original recorded digests remain below rather than being silently
overwritten. Before this audit, the ledger and sibling cross-index already
differed from these values. This older table is provenance, not a current
integrity check. Its generation date was not recorded here.

| File | SHA-256 |
|---|---|
| `docs/corpus/00_README.md` | `46221d8fd2c2ee80abb503431f934076166580a8dce72c71ea819f2cedb956e1` |
| `docs/corpus/01_CANONICAL_HARDWARE_BASELINE.md` | `878d563b615786388c59bef64ed72654df670366aa9f9f9a27e8489e5e301e7b` |
| `docs/corpus/02_CLAIM_EVIDENCE_LEDGER.md` | `c864b2c8e1d3b7c33250df47afadd84598775b891ff43e7b997d94c179167f70` |
| `docs/corpus/03_RESEARCH_FRAMEWORK_AND_CRITIQUE.md` | `75a18d814777cf02cdd0678fd75f8f630f713a6ebc65dafc21b84bae0bfba158` |
| `docs/corpus/04_FCC_EXHIBIT_INVENTORY.md` | `4bcbcf32624820342d8d29ff2fc263a6946a1fdb0758a397fef84620c7b19362` |
| `docs/corpus/05_SIBLING_SOURCE_CROSSINDEX.md` | `8606931caf4a82f3c3dc0b0552bcff29f297f83be1f2fe11ed1525e2b8914d12` |

These hashes fingerprint this generated corpus only; they are not hashes of external source documents unless a source entry explicitly says so.

## Bibliography

This file contains no independent hardware assertions. Bibliographies are embedded in every substantive corpus document.
