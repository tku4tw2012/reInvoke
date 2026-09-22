---
title: One-command sealed NAND trial runner
description: Offline inspection and an explicitly gated foreground apply, stopping before manual boot observation
---

## Scope and current status

`trial-runner.js` is a small host wrapper for the **already-built, sealed
`nandstockroot` single-phase engine**, not a builder, signer or general NAND tool.
It uses existing Node.js and ADB; no packages are required.

The owner approved one bounded trial of published StockRoot V11, without new
rootfs edits, using its matching native code and retaining unit data. Its
authorization is recorded separately in the archive. A software acknowledgment
does not authorize another trial or a repeat after failure.

The current pins cover only
`community-stockroot-native-01-20260911`. A future trial requires a newly reviewed,
prebuilt sealed engine/plan/capsule and corresponding reviewed runner pins.
There are no runtime pin overrides or automatic approval. Changing pins alone
cannot change the engine's compiled region, manifest, order or health guards.

## Inspect: default, host-only

From the repository root:

```bash
node tools/nand-inspect/trial-runner.js \
  ../reinvoke-archive/build/artifacts/community-stockroot-native-01-20260911
```

This hashes the manifest, capsule, ARM writer and host writer against the
runner's exact pins, then executes the pinned **host** writer's offline `plan`.
It creates no intent or staging files and never invokes ADB.

## Apply: reserved for a separately approved future attended trial

The interface below is documentation, **not approval to execute it**. An owner
must first approve the exact trial, target unit and NAND scope outside this
program. Record that approval's reference; entering text is not evidence that
the owner actually approved anything.

```bash
node tools/nand-inspect/trial-runner.js APPROVED_ARTIFACT_DIRECTORY apply \
  --evidence NEW_HOST_EVIDENCE_DIRECTORY \
  --confirm APPLY-STOCKROOT-NATIVE-01 \
  --approval-ref 'reference to fresh external owner approval'
```

The evidence directory's parent must exist; the directory itself must not.
Use durable local host storage, not a RAM filesystem. The artifact directory
must also be writable for the permanent intent marker. Do not move/copy an
artifact to circumvent a previous intent. There is no reset/reissue option.

The wrapper:

1. Checks the exact pins and phase acknowledgment before ADB access.
2. Selects physical USB **3-1.2**, on localhost ADB port **5037**, requiring one
   ready transport. Serial `0123456789ABCDEF` is only an additional check, **not
   a unique unit identity**. Subsequent commands use the transport ID, with a
   second physical-port check before apply.
3. Checks root, ARMv7, the complete running `/proc/version` cleanup-fixed
   banner, `/dev/reinvoke-nand-ro` **90:3, mode 0400**, and `/dev` tmpfs.
   Checks free tmpfs capacity and available RAM against staged input bytes
   plus reserves of 16 MiB tmpfs space and 32 MiB RAM. On older kernels without `MemAvailable`, the RAM
   estimate is `MemFree + Buffers + Cached - Shmem`.
4. Stages only the three pinned inputs in a new private
   `/dev/reinvoke-nand-trial-…` directory and
   checks their hashes on the target. The engine still independently checks
   source pins, RAM paths, topology and health before mutation. The existing
   native init mounts `/dev` as tmpfs; `/run` is not assumed to be tmpfs.
   No mount/remount, device-node creation or existing-file cleanup is added.
5. Flushes host intent files before invoking **one foreground `apply`**. The
   permanent artifact-side `.trial-…intent.json` also blocks another attempt
   with a different evidence directory after failure/disconnection.
6. Streams and saves stdout/stderr, records ADB's actual exit/signal, and
   requires unique nonce-bound begin and final remote-exit markers. Uses the
   legacy daemon's required PTY and normalizes CRLF. Shell startup output
   before the begin marker stays in the transcript, outside the parsed payload.
   Marker printing explicitly uses BusyBox, not the outer shell's PATH.
   Requires all per-block journal
   readbacks in sealed order, exact final whole-target hashes/unchanged-block
   verification, and successful engine cleanup. A short log, failed status,
   missing exit or ambiguous transport is **not success and never retried**.
7. Only after success, removes its **three exact staged input files**. It
   never deletes device nodes, writer locks, engine evidence or other RAM
   paths. Failure leaves staging/evidence intact for manual diagnosis.

The runtime banner is a check of the **running kernel's identity**, not a
cryptographic readback of its executable image. Independently retained RAM
load provenance must identify supported kernel SHA-256
`708c8a17a2817b1d44216b1597e2e3cf6d59d366d95d804bdb96b16d2ecaf32e`.
This wrapper neither reloads a kernel nor treats a hash of a stored image as
proof that image is running. The engine repeats its own runtime checks.

## End-to-end timing for the next candidate

Start the trial clock when the new image direction is selected, before image
preparation. Keep a persistent timestamped event ledger through the native-boot
verdict. The runner's apply duration alone is not the overall process duration.

Record preparation/build, validation/review, approval request and response,
USB/RAM access, staging, writer checks, erase/program/readback, any separate
verification, observer readiness, power-cycle request and response, and the
observed boot result. Retain failed attempts and unexplained gaps in that same
timeline rather than restarting the clock after a problem.

Report total wall time alongside phase durations. Separate owner waiting from
active tool work and preparation/coordination time; do not silently omit either.
Show overlapping work without adding overlapping durations into a false total.
Use the existing writer's per-stage journal for the NAND breakdown, not another
instrumentation framework. Timing never authorizes a write or power action.

## What is faster, and what is not claimed

The measured review is in archive
`evidence/2134-early-adb-01-20260911/TRIAL-SPEED-REVIEW.md` and
`trial-timings.json`. The wrapper removes hand-assembled per-trial staging and
apply commands. It does **not** call the redundant standalone full-device
`preflight` (previously about 104 seconds): `apply` already performs that
fingerprint before the first erase.

It leaves the entire engine unchanged, including complete initial/final scans,
read-only/writable-view comparisons, immediate per-block data/OOB readback,
exact allowed regions/write order, ECC limits and cleanup. Per-block readback
is essential because this controller can report success after a failed write.

The fast path uses the writer's existing **full-target verification**, not a
second automatic whole-chip capture or separate `verify-target`. It **does not
claim independent readback**. Reserve the existing independent full main/OOB
reader for engine/format changes or unexplained drift, under separate manual
control. There is intentionally no new reader or automatic readback plugin.
Keep its distinct evidence if used.

## Mandatory manual stop

The wrapper never starts/stops a boot helper, resets, reboots or power-cycles.
It returns at the manual observation gate, not at native acceptance.

Before a separately requested normal power-cycle, the owner/operator must:

* Verify firmware-serving/boot helpers (including port **8141**) are **off**.
* Start the observer and positively calibrate it against the current RAM
  shell before the power transition.
* Obtain the owner's explicit power-cycle acknowledgment and pause if the owner
  is unavailable. For this StockRoot trial, use the publisher's power-only
  procedure: unplug USB and power, then reconnect wall power only, with no
  Reset/MicOff sequence. Keep the observer running; absent USB is expected while
  the cable is unplugged. Warn about the loud published startup sound first.
* Observe native startup for an unmistakable image-dependent change and useful
  operation, such as a changed startup cue with working Bluetooth or Wi-Fi.
  Native ADB is optional diagnostics, not a prerequisite. Ordinary stock-like
  behavior, a product string or pre-cycle calibration alone does not identify
  the intended modified runtime.
* If recovery is needed, record that boundary before using the USB helper.
  Helper-loaded RAM ADB is not the native system's live shell and does not
  recover its lost volatile logs.

Keep the runner foreground and transport live; do not impose a timeout on
erase/program. On any failure or ambiguity preserve host evidence and RAM
state, do not clean up/reissue/power-cycle automatically, and escalate for
manual review. The preserved intent deliberately blocks blind retries, even
when failure might have happened before mutation.

## Mock-only validation

```bash
node tools/nand-inspect/trial-runner-test.js
```

Tests use synthetic inputs and in-process ADB mocks exclusively, writing only
inside an exclusively created, owned project test directory. They do not
contact hardware, launch helpers or execute the ARM writer.

A read-only live check found issues that the initial mocks missed: this daemon
rejects `shell -T`, its outer shell has no bare `printf`, and shell startup
prints `/` before the command's output. The wrapper and regression cases now
handle that protocol explicitly. Live UID and RAM-identity queries passed.
A deliberate remote `false` returned host ADB status zero but remote status
one, and the parser rejected it. All 26 mocked trial cases passed. A successful
mocked apply still does not qualify real NAND execution through this wrapper.
