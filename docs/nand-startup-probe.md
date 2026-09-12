---
title: Historical two-block NAND experiment
description: Retained probe, correction and restoration evidence; not current execution instructions
ms.date: 2026-09-12
ms.topic: reference
---

## Historical scope

> [!NOTE]
> This records the earlier two-block experiment, not the current device state.
> Later work progressed through that failed pilot to native candidate 02, which
> now starts from NAND and provides Bluetooth, audio, controls, provisioning,
> and local-network services. Use the
> [native NAND platform](native-nand-platform.md) for current behavior and
> [NAND startup status](nand-write-decision.md) for the full history.
> Do not execute a reset, reinstall or restore from this historical sequence.

> [!CAUTION]
> The read-only preflight passed on 2026-09-09, but the temporary partition
> test lost ADB during cleanup. A double-free is present in the retained
> vendor kernel's MTD block-device removal code. The mapping-based writer is
> withdrawn for the old kernel and broad image. The corrected RAM kernel was
> booted successfully at 17:28 UTC on 2026-09-09, and its two-block mapping
> lifecycle subsequently passed. Do not run the withdrawn broad writer.

The two-block diagnostic has been written and verified, including after power
cycling. Its normal-startup acceptance remains unproven. Do not repeat the
installation, erase the chip, replace boot images, or open the enclosure.

The desired end state remains reInvoke starting from wall power and reaching
the local assistant backend. This probe is one intermediate experiment: can
the normal boot path execute a modified primary filesystem, and what does its
installed kernel actually provide?

The owner explicitly approved the two-block write. That installation completed
once with `INSTALL_EXIT=0`; independent NAND readback matched the candidate.
No automatic rollback or larger write is authorized.

The earlier offline suite passed but did not exercise kernel partition
destruction. Live preflight now verified all 373 target blocks against the
saved original with zero uncorrectable ECC errors. The subsequent mapping
comparison reached completion, but cleanup did not return and ADB went offline.
These describe the earlier failed mapping attempt, before kernel correction.

The subsequent owner-assisted RAM recovery succeeded. A fresh minimal-target
preflight matched both saved blocks, reported zero failed ECC increments and
six corrected-counter increments, captured 4,096 all-`0xFF` visible OOB bytes,
completed cleanup and returned status zero. ADB remained responsive afterward.
No mapping was created during that preflight.

The later mapping-only qualification also passed: the kernel reported the exact
two-block interval, comparison completed, removal returned successfully and ADB
remained responsive. Only the original `mtd1` remained listed. An independent
read through the read-only MTD character node still matched the original bytes.
This qualifies that mapping lifecycle, not NAND erase/program behavior.

## Installation and normal-boot result

The reviewed Phase B writer (`1b41fdcf...`) erased and programmed only physical
blocks 357 and 358. Both fresh readbacks passed exact content and ECC checks;
the full extent matched `a64b76e8...`, removal completed, and ADB stayed alive.
An independent read via `/dev/reinvoke-nand-ro` matched the candidate rather
than the original. Evidence is under
`evidence/nand-ram-recovery-20260909T1724Z/`, including `INSTALL-SHA256SUMS`.

The owner then performed normal power cycles, including power-on with USB
absent followed by USB attachment after 30 seconds. Neither the new USB product
nor runtime ADB was observed. USB reported Marvell `1286:8174`. That identifies
the observed endpoint, not the reason for arriving there.

The later host listener was incorrectly described as an "08-only" test. It
actually exposed the recovery-chain files, and its log records `09_IMAGE`,
`sysinit.img`, `bootloader.img`, `drm_erom.img` and `79_IMAGE` requests, not
`08_IMAGE`. It reached recovery U-Boot. The confirmed prompt was then used to
load the known corrected RAM Linux without another physical reset.

In that recovered RAM session, the two installed blocks still matched the
candidate. All main data before rootfs, `[0,0x02920000)`, matched the September 7
capture byte-for-byte. This excludes changed main-data bytes in those regions,
not unmeasured OOB differences or every possible boot cause. The claim that USB
attachment alone caused the failure was premature and is withdrawn.

The entire 48,831,891-byte SquashFS read from NAND after power cycling also
matches the offline candidate, not merely the changed blocks.

The owner reports that the boot USB endpoint previously disappeared after a few
seconds unless yellow mode was selected, whereas it now remains present. Treat
that change as possible failure/fallback behavior, not a convenience improvement.
The exact cause is unknown.

### Controlled restoration

The owner then explicitly approved restoring the exact original two blocks.
The same reviewed writer completed that restoration once, with exact readback,
ECC checks, successful cleanup and `RESTORE_EXIT=0`. Independent main-data
readback matched the original `005be33d...` hash, not the diagnostic payload.
Evidence is in `evidence/nand-original-restore-20260909/`.

After restoration, the owner again confirmed wall power off, USB out, wall
power on, a 30-second wait, then USB in, without buttons. The passive observer
recorded the same persistent `1286:8174` endpoint with class/subclass `FF/FF`,
not ADB. No host boot helpers were running. Removing the startup edits therefore
did not restore the earlier transient USB behavior.

This does not prove all NAND activity was unrelated: logical restoration
regenerates hardware ECC rather than restoring every physical OOB bit. Stock
ADB availability was never established, so absence of ADB alone cannot establish
whether the speaker otherwise operates. The owner was asked whether Bluetooth
playback works after restoration; that observation is still pending.

### No-reset RAM recovery and full comparison

The retained open-source USB helper at revision `63444e82` can enumerate an
already-present `FF` device. It attached without another reset or USB replug,
served the bootstrap, observed `FF` to `FE`, then served the `09_IMAGE` recovery
chain to U-Boot. The known corrected RAM kernel and unchanged RC12 initramfs
were loaded from that prompt.

The loader reported failure waiting on ADB port 5038, but RAM ADB was already
working on the existing port 5037 server. The USB product, serial, kernel build,
RAM mounts and a fresh shell command were verified. Check existing server
ownership before treating an ADB timeout as a failed boot; do not request an
extra reset merely because the selected host port has no device.

In that session, a complete 256 MiB main-area read and all 32 controller-visible
OOB bytes per page matched the September 7 captures byte-for-byte. The main
hash is `2fac4159...`; the exposed-OOB hash is `a2df8249...`. The ECC failure
counter was five after the full main read, consistent with the historical
unverified-page limitation. This comparison is not a raw physical-chip clone.

The original rootfs data is restored and RAM ADB on port 5037 is currently
available for diagnosis. Recovery image servers have exited. Do not repeat
installation or expand the write. Evidence is in
`evidence/nand-restored-ram-inspection-20260909/`.

## Why this experiment

The unit's captured version tables establish a 90 MiB rootfs allocation at
`0x02920000-0x08320000`, end exclusive. The known ECC-failing pages, bad
blocks and changed factory block are outside that allocation.

The actual installed `bootimgs` has an encrypted CPU0 image descriptor. There
is no demonstrated way to boot that exact kernel with a substituted root
filesystem entirely in RAM. Running stock userspace under the development
kernel would not answer the stock-kernel question.

A diagnostic change to the installed filesystem is therefore a useful first
boot experiment. It does not require proving recovery from every imaginable
failure before accepting ordinary DIY risk.

## Exact software change

Only `init.rc` changes. Immediately around its existing USB setup:

```diff
-    write /sys/class/android_usb/android0/iProduct "MRVL USB SDK"
+    write /sys/class/android_usb/android0/iProduct "reInvoke-min"
     write /sys/class/android_usb/android0/functions "adb"
-    # write /sys/class/android_usb/android0/f_acm/instances "1"
+    start adbd
     write /sys/class/android_usb/android0/enable "1"
```

The existing `adbd` service and all other stock startup actions stay intact.
No microphone, DSP, Bluetooth or assistant code is replaced in this probe.
Spaces pad the replacement service-start line to preserve its length. The
complete file retains its length and changes 69 bytes.

Unlike the older broad diagnostic, this candidate does not create a `/run`
marker. Its execution signal is the changed USB product string, followed by
ADB inspection of the running kernel, mounts and exact `init.rc` content.
Neither signal alone certifies full boot or product readiness.

## Prepared image

The layout-preserving
[builder](../tools/nand-inspect/cmd/squashfs-min-probe) reads the pinned
captured filesystem and recompresses only the shared fragment containing
`init.rc`. Its zlib stream uses valid empty DEFLATE blocks to retain the
original stored length. It does not append ignored trailing padding or move
filesystem metadata.

It checks that:

* the capture hash and decoded rootfs allocation match the reviewed unit;
* only `init.rc` content changes;
* file types, modes, owners, groups, modification times and symlink targets
  remain unchanged;
* re-extracting the final image reproduces the expected file hashes and
  metadata; and
* the image and its erase-rounded extent fit inside rootfs.

The [offline validation](../tools/nand-inspect/squashmin-validation/README.md)
also uses the actual vendor kernel's decompressor, compiled for the host.
Original and candidate streams decode to the intended fragment; negative
controls reject corrupt/trailing input. This does not execute the filesystem
through the device's full mounted kernel path.

Independent review confirmed the current two-block payload but found that the
first verifier could accept incorrect payload files or proposal metadata.
The corrected verifier binds the complete block files and proposal to the
pinned capture and full image. Four counterexamples passed the old verifier
and fail the corrected one. That evidence is retained in
`derived/squashmin-bundle-binding-20260909T1325Z/` in the archive.

The final candidate is retained outside Git:

```text
reinvoke-archive/derived/squashmin-20260909T1320Z/candidate/
```

| Item | Value |
|---|---|
| Image | `minimal-probe.squashfs` |
| Image bytes | 48,831,891, unchanged from original |
| Image SHA-256 | `1d7b26012a9feed017439b030c1965315e39464d2044e98c50aa3ef9017d3594` |
| Proposed write start | `0x02ca0000` |
| Proposed erase end, exclusive | `0x02ce0000` |
| Proposed erase blocks | Two, rootfs-relative 28 and 29, physical 357 and 358 |
| Affected data bytes | 262,144, including 48,015 differing compressed bytes |
| Write payload | `changed-blocks-candidate.bin`, not the whole SquashFS |
| Rollback data | `changed-blocks-original.bin`, exact original two-block contents |

This is a 256 KiB physical erase/program scope, not a 69-byte write. The
filesystem header and all other erase blocks remain unchanged. Both blocks
must be restored together if returning to the original compressed fragment.

`PROPOSAL.json` records these values with `write_approved: false`. It remains
the immutable offline build record; subsequent owner approval is recorded
separately and does not change the hash-pinned input bundle.
The earlier full-recompression candidate and 367-block writer are withdrawn
references. Their hashes, extent and approval strings must not be reused for
this smaller candidate.

## Before an actual write

The [separate bounded writer](../tools/nand-inspect/probe-writer.md) is now
implemented and fault-tested on host-only fake flash. Its corrected-kernel live
preflight and mapping lifecycle have now passed. A separately reviewed
write-enabled build completed the approved write. Its checks are:

1. Select the current unit explicitly and confirm the expected geometry.
2. Read the exact proposed extent afresh, checking bad blocks and attributable
   ECC-counter changes. Its data must match the retained rollback image.
3. Back up its available OOB metadata as well as main data. Do not pretend that
   the current API exposes every physical OOB byte.
4. Restrict erase and program operations to the proposed extent through a
   temporary kernel MTD partition as well as userspace bounds.
5. Use normal hardware ECC, not fabricated raw OOB bytes.
6. Stop on an error; never silently skip into another region, mark a block bad,
   or extend the write.
7. Verify both complete block contents and ECC counters before restart.

The running kernel's retained source and symbol map contain `mtd_add_partition`
and the MTD partition ioctl. Such a partition is a RAM mapping, not a rewrite
of the on-flash version table. The original-kernel attempt lost ADB before
cleanup returned; the corrected-kernel two-block attempt completed successfully.

The owner has returned, the corrected RAM reload and two-block preflight have
passed, including mapping qualification. The approved two-block write is
proceeding through final build/review checks under autopilot.

The selected footprint contains only the two differing blocks. The writer
must bind its kernel mapping and file hashes to that same narrow extent.
It restores the original only through a separate explicit
action; a failed install never triggers an automatic reflash or reboot.

An optional one-block test in the unused-by-SquashFS rootfs tail can exercise
the writer first. That would need its own approval and cleanup. It is not
required to prove total-chip recovery and cannot prove stock boot acceptance.

## One attended boot session

After the writer is prepared, reviewed and specifically approved:

1. Perform the scoped write while the current RAM system is running.
2. Stop the host's automatic USB boot loader so it cannot supply the
   development image and disguise the result.
3. Power-cycle normally, without the yellow-mode button sequence.
4. Observe the new USB product string, then use ADB if it becomes available.
5. Read `init.rc`, the kernel release, command line and mount table. Establish
   that this is a normal persistent boot, not the development kernel supplied
   by the host. Installed module metadata suggests `3.8.13-yocto-standard`;
   record the actual result rather than assuming that release string.
6. Check early and later ADB reachability and record normal boot behavior.

Discover the new ADB transport at the selected USB port rather than assuming
the RAM image's serial or device-node paths persist. Confirm a genuine restart
from USB re-enumeration and uptime/build observations. Interpret the complete
kernel command line and mount layout: a vendor initramfs can itself be loaded
from NAND, so a `root=/dev/ram` token alone would not prove host injection.
The host boot helpers must be stopped regardless.

USB remains connected only for this development test. The intended installed
product must subsequently boot with the cable absent.

If the new USB product is observed after a normal power-on without RAM
injection, and ADB confirms the expected filesystem and running kernel, the
changed startup code executed. That is the relevant positive result. It does not prove
there is no verification elsewhere or that a different kernel image will pass.

Missing USB alone is not evidence of a signature failure. Gadget initialization,
daemon startup, normal boot and later vendor behavior can fail separately.
An unsuccessful probe also cannot exclude a pre-existing normal-boot problem.
Compare against restoration of the original extent before attributing failure
to rootfs authentication.

## Rollback and remaining risk

The intended rollback is the exact saved rootfs extent through the same
normal-ECC writer, if yellow-mode RAM recovery remains reachable. It is not a
full `83_IMAGE` reflash.

The 2026-09-09 failure happened before any rootfs write. Do not run rollback
to address that incident. The required recovery is of the volatile RAM boot,
not restoration of an intentionally changed NAND filesystem.

An interrupted in-place write can leave rootfs unbootable. Early boot images
remain excluded from the writer, which is a reason to prefer this approach,
but recovery after this failure has not been demonstrated on the unit.

Normal vendor boot also mounts writable factory/application storage. Those
regions are not reflashed by this proposal, but ordinary startup may change
them. This is not a promise of zero autonomous filesystem activity.

These are concrete DIY risks for the owner to accept, not a demand for a
programmer, disassembly, or an exhaustive fault-injection campaign.

## Temporary mapping incident and offline correction

The host retained the successful preflight but could not retrieve mapping
logs after ADB became offline. The mapping transcript reached read-check
completion before deferred cleanup, so the lifecycle result is a failure.

The retained source and compiled kernel contain a double-free in
`mtdblock_remove_dev`. The
[kernel patch](../patches/invoke-kernel/0005-fix-mtdblock-removal-lifetime.patch)
saves the bad-block-map pointer before generic deletion, removes the duplicate
private-object free, and releases the map after queued I/O is stopped.

The [offline ownership regression](../tools/nand-inspect/internal/probewrite/kernel_cleanup_test.go)
compiles the actual callback before and after patching. The original fails;
the patched callback passes immediate and deferred ownership cases. This
does not replace a kernel build or a later hardware test.

The corrected kernel has now been loaded and identified on the device.
Partition removal and the scoped write subsequently completed successfully
with the reviewed new binaries. Do not repeat the old mapping binary.

## Corrected RAM recovery

The corrected kernel was built and then booted on this unit. The existing
[kernel builder](../tools/kernel/README.md) applies the fix only with
`--mtd-cleanup-fix`, using separate source/build paths. The retained baseline
kernel and RC12 runtime are unchanged.

| Item | Prepared value |
|---|---|
| Corrected kernel SHA-256 | `708c8a17a2817b1d44216b1597e2e3cf6d59d366d95d804bdb96b16d2ecaf32e` |
| Unchanged RC12 initramfs SHA-256 | `a0f273ddfb4a7a3844078b88f4ff826a1f1d1c7ae8c01b77b87533e3bf8985de` |
| Kernel release/module ABI | `3.8.13-reinvoke-audio-sd8887` |
| Expected `uname -v` | `#1-mtd-cleanup SMP PREEMPT Thu Jan 1 00:00:00 UTC 1970` |
| Isolated host kit | `reinvoke-archive/build/artifacts/nand-ram-recovery-20260909/` |

The final kernel's clean build and forced rebuild produced identical images.
Config, DTB and module ABI checks supported using the unchanged RC12 initramfs.
The live boot subsequently confirmed its runtime modules loaded and ADB
remained responsive. The exact
`/proc/version` string and evidence are in the kit's
`ram-handoff-manifest.txt`.

Host-only loader preparation rejected a deliberately wrong kernel hash before
staging either image. With correct hashes it staged both exact files in the
isolated kit and returned `prepared ... no commands sent`. The usual host
firmware directory was not changed. The kit contains no active `08_IMAGE`,
`83_IMAGE` or `99_IMAGE`, and `79_IMAGE` is comment-only.

The following records the attended recovery procedure. Any repeat still needs
an owner-approved window. First confirm no other session owns the speaker's USB
connection, the console port or the loader. Do not stop unrelated processes.

```bash
recovery="$HOME/harman-kardon/reinvoke-archive/build/artifacts/nand-ram-recovery-20260909"
(cd "$recovery" && sha256sum -c SHA256SUMS) || exit 1
INVOKE_FIRMWARE_DIR="$recovery/firmware" \
  "$recovery/host-tools/start-session.sh" absent || exit 1

"$recovery/host-tools/boot-native-ram.sh" \
  --firmware-dir "$recovery/firmware" \
  --kernel "$recovery/firmware/81_IMAGE" \
  --kernel-sha256 708c8a17a2817b1d44216b1597e2e3cf6d59d366d95d804bdb96b16d2ecaf32e \
  --initramfs "$recovery/firmware/82_IMAGE" \
  --initramfs-sha256 a0f273ddfb4a7a3844078b88f4ff826a1f1d1c7ae8c01b77b87533e3bf8985de \
  --adb-server-port 5038 \
  --status-file "$recovery/live-loader-status" \
  --wait-for-prompt
```

Only after the poller reports `READY` and the loader is waiting, perform the
[previously verified yellow-mode sequence](uboot-access.md): power off, hold
Reset, restore power and press MicOff four times within five seconds. This is
one owner-assisted RAM recovery, not a NAND install or restore.

After ADB returns, require the new `uname -v`/`/proc/version` identifier, a RAM
root command line, no NAND mounts and continued responsiveness. The unchanged
`uname -r` alone cannot distinguish the old faulty kernel from the correction.
Use `/bin/busybox uname` on this RAM image; bare `uname` is not on its ADB PATH.
Capture these observations on the host. A RAM boot alone does not qualify a
writer: the separate mapping lifecycle and approved installation evidence
above establish what was actually exercised on this unit.

## After a successful probe

Use the installed-kernel observations to choose between:

* a reInvoke userspace/startup adapter with vendor-compatible modules; or
* a kernel handoff, only if that kernel actually supports it.

Then build the full reInvoke rootfs and test standalone startup, networking and
the already-implemented hardware services. Do not reinterpret a successful
diagnostic probe as completion of that work.
