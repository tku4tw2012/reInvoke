---
title: NAND decision and experiment history
description: Candidate 03 startup, candidate 02 acceptance, failed NAND trials, recovery limits, and dated decisions
ms.date: 2026-09-12
ms.topic: reference
---

## Current result

Candidate 02 reached the wall-power-only functional milestone. Candidate 03
is now installed and has an image-dependent native startup indicator. Use the
[native NAND guide](native-nand-platform.md) for current operation, exact
installed hashes, private build prerequisites, and remaining gaps.
The chronology here preserves the reasoning and failed work, not an active
flash plan.

### Candidate 03 installation and startup

On 2026-09-12, the approved one-shot vendor operation installed bundle SHA-256
`8a26ac4e2160802fb0a5451c8d856bb7d70177e70a07991326ac65c4c159988f`.
All nine record program/read address sets, the exact transfer length, 2,046
good-block erase count, known bad blocks, and a fresh U-Boot response passed.
The complete wrapper took 36.819 seconds; command submission to verified
program/read coverage took 32.440 seconds. Those intervals include verification.
The helper stopped and active flash staging was removed before the owner's
power-only start. No RAM Linux was inserted between flash and native boot.

At 12:10:02 UTC a fresh Bluetooth remote-name query returned `reInvoke-NAND`
from the known unit, replacing candidate 02's `reInvoke-RAM`; active inquiry
also found it. This verifies changed native runtime behavior, not complete
03 acceptance. Initial radio checks had returned no response. Native USB
remained absent and the old LAN address was unreachable before reprovisioning;
SSH is not yet native-accepted. No new audio, controls, or microphone campaign
was performed. The first helper attachment also failed during recovery USB
transitions before U-Boot; reattachment to the stable downloader succeeded
without another physical request. The failed attempt and negative observations
remain in the private evidence.

See the [current startup result](native-nand-platform.md#candidate-03-startup).
The candidate 02 section below preserves that earlier session's observations
and decisions, not the currently installed image or a request to replay them.

### Functional boot before diagnostics

Native acceptance need not start ADB early or expose native USB ADB to count as
progress. It first requires an unmistakable image-dependent
change and useful operation after normal power-on without a firmware-serving
host: for example, a changed startup cue and working Bluetooth or Wi-Fi.
Ordinary stock-like Bluetooth behavior alone does not identify the modified
image. Missing USB ADB alone does not establish failed native boot.

ADB is a diagnostic/control channel when needed, including wireless ADB if the
native image provides it. If the image appears inert, the demonstrated USB
recovery path can supply the known RAM Linux and ADB for inspection and repair
planning. That is a separate boot environment, not attachment to the failed
native Linux session; its volatile logs and checkpoints are not thereby
recovered. Recovery has been demonstrated for the observed experiments, not
guaranteed for every possible future flash.

### Current native result: reInvoke candidate 02

The complete vendor-programmed reInvoke candidate has now produced native
functional progress. After the owner-confirmed power-only start, the speaker
advertised `reInvoke-RAM`, stopped the prior swirl, and accepted the operator client
after the owner long-pressed Bluetooth. Fresh host queries confirmed pairing,
an authenticated encrypted ACL connection and an A2DP endpoint. No helper
firmware was supplied after that native start. The Bluetooth name is inherited
configuration, not evidence that this was another host-loaded RAM boot.

The owner also confirmed that two Mic-Mute short presses turned the red
indicator on and off, and reported a blue flash on a top tap. These are
observations of controls and indicators, not full microphone-privacy
acceptance. The first musical playback test was inaudible at low host gain.
A later replay produced an owner-confirmed audible melody, and a subsequent
test was clearly audible while the owner adjusted its volume up and down with
the speaker's physical ring. Host PulseAudio's transport state is separate
from those owner-observed acoustic and rotary results.

A long Mic-Mute press opened the expected provisioning access point. The Mac
mini authenticated the ephemeral descriptor by its TLS certificate fingerprint
and bearer token, delivered the active local Wi-Fi credentials without logging
them, and restored its own network connection in one local operation. The
Invoke then answered on that network. Read-only reInvoke WAMP calls returned
MCU status, DSP version 25688 and speaker-side music volume 44, unmuted.
No SSID, local address or physical MAC is retained in repository documentation.

Reconnecting USB without changing power produced no Invoke USB enumeration or
ADB transport. The current native session is being preserved; do not boot
helper-supplied Linux over it for diagnostics. The image allows only the Mac
mini's configured Bluetooth address and opens a 120-second pairing window, so
failure to pair a phone or after the window expires is not by itself a runtime
failure.

The installed bundle is 62,812,192 bytes, SHA-256
`80cc1e2f17f284f161333e31bfc50d3574c0677b233b06b653474254f3b7679b`.
Its vendor command reported 2,046 good blocks erased and completed all nine
program/read loops. The correct transfer length and a fresh returned U-Boot
command were verified. No RAM Linux or independent Linux readback was inserted
between that vendor flash and the first native boot test.

This trial changed several variables together: complete vendor programming and
default app seed, cleared prior status/data, and owned startup that no longer
withholds the core runtime when USB diagnostics fail. Which change enabled
progress has not been isolated. Modified-native startup, Bluetooth connection,
audible playback, rotary volume, physical provisioning and local-network
MCU/DSP control are demonstrated. Native USB/ADB and remaining product
acceptance still require work.

On 2026-09-12, 02:43:39-02:44:54 UTC, native validation passed all eight
RawSocket WAMP groups, including rejected unknown procedures/malformed
arguments, fresh MCU/DSP sessions/events, real `44 -> 43 -> 44` volume,
music-mute/event checks, and restored baseline music `44` unmuted/system `70`.
A 75-second watch saw no heartbeat. At 02:46:14 UTC, WebSocket WAMP on 9998
also passed the `wamp.2.msgpack` handshake, MCU/volume reads, and
unknown-procedure rejection. This was protocol verification, not just an
open TCP port. Neither check used helper firmware, reboot, playback, or a
microphone-state change. See the [current evidence summary](native-nand-platform.md#current-result)
and private
`evidence/reinvoke-native-02-fullflash-20260911/WAMP-WEBSOCKET-OVERNIGHT.json`.
TCP 5555 explicitly refused ADB while WAMP remained reachable; USB did not
enumerate. This is not evidence of a native shell, process list, or mount
table. Port 5037 is the host ADB server, 8141 is the host recovery-helper
console, and USB ADB has no IP port.

Evidence is in sibling archive
`evidence/reinvoke-native-02-fullflash-20260911/NATIVE-FUNCTIONAL-PROGRESS.json`
and `native-bt-session/NATIVE-BLUETOOTH-CONFIRMED.json`.

## Historical NAND investigation

The remainder of this page preserves the failed and superseded trials that
established the current method. Commands and “next” statements below are
checkpoint-era records, not current execution instructions. Do not remove this
history: it records false-positive tests, failed assumptions, recovery evidence,
and operations that must not be repeated without a new review.
September 9-11 sections retain their original per-trial state and chronology,
including uses of “current”, “now”, and “next”. Candidate 01 and other earlier
trials failed their observed acceptance gates; a missing USB endpoint or ADB
alone did not conclusively locate their boot failure.

### Completed trial: published StockRoot

The owner delegated image selection and approved a larger NAND trial. The
selected source is coggy9's published
[StockRoot release](https://github.com/coggy9/HKHacking/releases/tag/StockRoot),
`Barracuda_rooted_libre-11.1842.0`. It enables network ADB, blocks OTA domains
and changes the startup sound. It is a development/research image, not the
finished reInvoke runtime. The publisher warns that the first boot sound is
loud and that setup AP creation sometimes needs a five-second Mic-button hold.

The full process clock started on September 11 at 18:10:35 UTC, before source
selection/preparation. The 107,934,810-byte published asset downloaded and
matched SHA-256
`f59d0a56f5d3d4cc90b146e2433ec32da36239e6c4373813d57fe92e19326cc7`.
The existing inspector verified all nine payload CRCs, the actual 256 MiB
geometry and agreement with the unit's named allocation addresses.

The candidate uses the published 84,824,064-byte rootfs without new edits or
recompression, together with its matching native code. Compared with the
installed 2134 reference, the pre-bootloader, post-bootloader and both kernel
containers differ; TZ and BSL already match. The raw package block0 payload
also matches the earlier OTA2 package, but actual block0 remains protected
rather than being rewritten as raw logical data.

The separately sealed profile and parent boundary review passed. One approved
apply process started at 19:10:24 UTC for 737 changed blocks (92.125 MiB), with
pre-bootloader copies 8 through 1 last. It completed at 19:17:53 with the whole
target main and exposed OOB verified, 20 corrections within the per-read
allowance, no accepted failed ECC, and successful cleanup. No duplicate
standalone preflight was run. Separate read-only captures of the rootfs and
all changed native-code allocations matched every target byte at 19:21:32.
Installation preserved
unit app/factory/status data, unreplaced allocations, tail, bad blocks/BBT and
all exposed OOB; no image99, donor app/ZIP import or whole-chip erase is allowed.
Normal firmware may subsequently update its own writable data after boot.

The observer was armed and calibrated at 19:23:06. The owner reported two power
cycles, the second with USB unplugged, and more than 90 seconds of waiting:
no sound or changed behavior, only the top swirl. The second power-only cycle
was owner-confirmed, not directly timed through the disconnected USB cable.
Reconnecting USB without changing wall power returned FF and no native ADB.
The published five-second Mic-button setup workaround produced no reported
response or change. A fresh request to the known Bluetooth address returned
no name. No useful modified native behavior was demonstrated.

The helper was started only after that observation ended. It reached U-Boot
without a physical reset, and the known RAM diagnostic returned a fresh ADB
shell at 20:01:40. The unit is now in host-assisted RAM Linux, with StockRoot
code remaining installed. This recovery is not native-boot proof. No further
NAND write is approved.

This was not an exact replication of the published whole-chip procedure:
the bounded Linux writer retained app and fw_stat state. The vendor flasher
would erase unlisted state such as fw_stat and programs through its own path.
Those differences remain candidates for investigation; another rootfs edit
alone is not supported as the next step.

Artifacts and authorization are in sibling archive
`build/artifacts/community-stockroot-native-01-20260911/` and
`evidence/community-stockroot-native-01-20260911/`. Earlier research and
preparation are not retroactively counted as part of this trial; everything
after its stated start, including waits and failed attempts, is included.
`TIMING-SUMMARY.json` records 59m50s of preparation, about 7m28s for runner
staging/apply, 39s for independent reads/transfers/comparison, and the separate
post-write coordination and observation period. Within the engine, the changed
block loop took 115s, including 77s of erase/program/immediate verification.

### Previous installed reference: 12.2134.0 with early ADB

The owner treated the coherent 12.2134.0 early-ADB diagnostic as the installed
reference before the StockRoot trial, not a known-good native-boot baseline.
Its approved 763-block update
completed, including pre-bootloader copies last. The writer and a separate full
main-data/exposed-OOB readback matched the target. No image99 or vendor
whole-chip command was used for this update.

The subsequent normal power cycle failed native ADB acceptance. With USB
connected throughout and the helper off, USB disconnected at 15:41:24.991 UTC
on September 11 and returned as `1286:8174` FF at 15:41:41.287. It remained FF
at 15:44:37, without the diagnostic product, ADB or retrieved checkpoints.
No native U-Boot or Linux console transcript was received. The failing boot
stage remains unknown.

At the owner's request, the pinned helper attached to that existing FF endpoint
at 16:31 UTC without a physical reset or replug. A fresh `version` response and
prompt worked. The known cleanup-fixed kernel and RC12 handoff diagnostic then
returned RAM ADB at 16:34:40. That is host-assisted recovery, not native startup.
The helper subsequently exited; the unit is left in the known RAM diagnostic.

Read-only post-cycle inspection verified on this unit:

* The complete 46,370,816-byte NAND SquashFS still matches the early-ADB image,
  SHA-256 `d35bb0c89d2fc6d8188839173b0934b5127c5a751eea4ad68d4980fe76fe710d`.
* Its actual `etc/version.txt` reports `Barracuda_libre-12.2134.0`. Its modified
  `init.rc`, added early helper and stock adbd match their intended hashes.
  The init hash also rejects the unchanged donor file as a negative control.
* All eight pre-bootloader copies and the first 4 KiB of both boot-image slots
  match the installed reference. This was a targeted read, not a new whole-chip
  or OOB verification.

No NAND filesystem was mounted and no installed startup script was executed
during this inspection. File persistence is established; native execution is
not. The later StockRoot trial is separately approved above; no other write is
authorized.

The recovery console's actual help says `nandrd` displays bytes. Its default
`bootcmd` uses TFTP/NFS development placeholders, not the installed NAND.
There is no established native-NAND launch command in this console; generic
`boot` was not tried. The next useful investigation is how the normal boot path
selects and launches its kernel/container, rather than another USB-label edit.

Current evidence is in sibling archive
`evidence/2134-early-adb-01-20260911/BOOT-RESULT.json` and
`evidence/nand2134-helper-inspection-20260911T1632Z/POSTCYCLE-RESULT.json`.
The histories below describe earlier states, not the current installed image.

### Historical 12.2050.3 stock-plus-ADB reconstruction

The owner selected September 7's repeat-matched main-data and exposed-OOB32
captures as the source, then rejected an unmodified-original boot as the goal.
That target retained the original kernel, libraries and services with a minimal
ADB-enabling startup edit. No unmodified-baseline boot detour was performed.
The owner approved the combined stock-plus-ADB reconstruction after its full
read-only preflight passed. The first phase wrote only tagged app block 1573
and passed full mixed-state checks and cleanup. A separate BusyBox main-data
read and legacy OOB reader then confirmed that both data and tags actually
changed and matched exactly, with all other exposed OOB unchanged.
The approved remaining 865-block phase completed in its exact order, ending
with pre-bootloader copies 8 through 1. The writer verified the full target main
and exposed-OOB hashes, unchanged blocks and successful cleanup. It recorded
10 corrected read events within the per-read allowance and no new failed-ECC
events. A separate full data/OOB recapture then matched every target main-data
byte and every selected exposed-OOB byte. The installed `init.rc` hash and
three ADB-enabling lines were checked from that actual readback.
The earlier unmodified `RESTORE-PLAN.json` is superseded and was not executed.

The subsequent owner-confirmed normal boot failed ADB acceptance. With USB
connected and the firmware server off, the observer recorded disconnect at
12:24:01 UTC and return to `1286:8174` FF at 12:24:28. Checks at 12:27:43 and
12:29:45 still found no ADB or native console. One fresh Bluetooth remote-name
query also received no reply. This does not establish a working Bluetooth-only
speaker or identify the failing boot stage.

The all-identities observer had captured a real RAM shell as its explicitly
labelled pre-cycle calibration, not a native-boot result. It was subsequently
stopped. At the end of that test, access was the FF USB endpoint only, with no
attached prompt or ADB. Evidence is in
`evidence/september7-stock-adb-20260911/FINAL-INDEPENDENT-VERIFICATION.json`
and `native-boot-observation/`.

The diagnostic changes only three lines of the original `init.rc`: product
`reInvoke-ADB`, the existing on-boot `start adbd` uncommented, and that service's
`disabled` flag replaced by a comment. There are 29 changed byte positions;
file length, filesystem size and all metadata remain unchanged. Only physical
rootfs blocks 357 and 358 differ from the selected source. This is distinct
from the first probe, which inserted an earlier post-fs start before USB enable.

All extracted file contents except `init.rc` matched. Canonical fragment replay,
metadata comparison and the actual retained kernel inflater with its negative
controls passed. The source already has `ro.secure=0` and `ro.debuggable=1`;
those properties and the stock ADB executable were not changed.

The patched filesystem is in `build/artifacts/september7-stock-adb-20260911/`,
SHA-256 `597b869df383f13b7e129d031c2e884b0b707d23b8279e4affdb40b9eaf3499a`.
The combined reconstruction target is in
`build/artifacts/september7-stock-adb-reconstruction-20260911/`.
Its target main-data hash is
`33f16be0dcbc42b92221471ab5685d84868ebff6f6ce7dabf3b66adcd638489f`;
target exposed OOB remains
`a2df824977591738f4d3ce51bd070308a19a58a3b8827af2751b5069428a9251`.
The sealed plan hash is
`7f8b2939b8aaf2064648d5a254df502c061be8d4c25fda509f9d6cd837dbbb53`.
These pins identify the approved inputs, not proof of completed reconstruction
or normal boot.

No physical reset was required to regain the current U-Boot console at
04:14:36 UTC. The known RAM diagnostic was then loaded for read-only inventory.
A complete current 256 MiB data capture and 4 MiB exposed-OOB capture finished
with checked transfers and zero corrected/failed ECC counters. Comparison
against September 7 found 866 changed erase blocks; 81 app blocks also need
their saved OOB metadata. The recorded bad blocks, BBT tail, block0 and factory
allocation already match and do not need rewriting.

The current captures are in
`evidence/nand-september7-reconstruction-20260911/`. Their hashes are
`4113074a844a0d8406bdb2ff771b104ae17b1a768bb51755d8e6f75dae8838ad`
for main data and
`eb66beedbe9450840fa159b3bfed350c666820fea2c35cc07e0e775cbbe70371`
for exposed OOB. `reconstruction-diff.json` records every changed block.
The actual kernel source supports combined single-page `MEMWRITE` in PLACE
mode with 2,048 data and 32 OOB bytes. The reconstruction implementation uses
that path under review, not RAW, AUTO or separate OOB-only programming. It must
verify real data and OOB because the legacy driver can lose NAND FAIL status.
The earlier data-only BSL writer is not a complete reconstruction method.

The diagnostic boot observation keeps USB connected before normal power-on,
without Reset/MicOff and without a firmware-serving helper. Observe all USB
identities and any ADB transport belonging to the unit's physical port, rather
than assuming the stock image uses the RAM diagnostic's serial number.
The goal is a real native ADB shell with the diagnostic file/kernel verified;
changed USB identity alone is not full acceptance.
Any later helper attachment is a separate host-assisted test. The five
uncertain captured fw_stat pages and absent hidden physical OOB remain
limitations of reconstructing the selected saved logical state.

Inspection of `init.rc` inside the selected September 7 capture provides a
concrete expectation limit: it configures the USB gadget, but `#start adbd` is
commented out and `service adbd /sbin/adbd` is declared `disabled`. That file
does not automatically start ADB; other possible triggers have not been
established. This is why an unmodified-original image would not itself establish
the desired ADB access. The current diagnostic intentionally changes those
settings. The source excerpt is retained as `baseline-usb-startup-excerpt.txt`
alongside the current inventory.

### Executed 12.2134.0 early-ADB update

The owner approved the concrete 12.2134.0 early-ADB update after preparation
and live preflight. The single bounded `apply` operation and independent full
readback completed successfully; the subsequent normal boot failed ADB
acceptance as recorded above. No factory-reset experiment was performed.
The donor payload itself reports `Barracuda_libre-12.2134.0`; no separate
12.2143 artifact was established. A provisional note contains the different
transposition 12.2314, which is not evidence of another verified release.

The audited 12.2134.0 and original 12.2050.3 `init.rc` files are byte-identical.
Both comment out the on-boot ADB start and declare adbd disabled. The adbd
executable is present and byte-identical in both, SHA-256
`7a142bbf80eeefd29f8bb1be24e3c9b9d39f55f3f7d7538a7501e6cb646e3a55`.
Their init executable, loader, required ADB libraries and shell also match.
Each image's own ARM loader resolved adbd and its shell executed under
emulation. That rules out a missing daemon or these missing userspace
dependencies, not untested native kernel USB behavior or startup ordering.

Both configurations run mount/data/module setup before USB configuration and
the on-boot ADB start. The implemented diagnostic uses the 12.2134.0 rootfs and a
compatible same-release boot/kernel/module set, and places diagnostic startup
after necessary device/PTY setup but before those uncertain dependencies.
The label being changed is USB `iProduct`, not a Linux PS1 or U-Boot prompt.

Acceptance requires helper-free normal power-on, recorded USB transitions,
then a real ADB command and intended kernel/rootfs/startup-file evidence.
A product label alone is a checkpoint, not a complete boot result.
The proposal review records three high-priority design issues and one
medium-priority evidence issue in
`evidence/adb-image-audit-20260911/2134-proposal-review.txt`.
The supporting executable/configuration/loader audit is in that directory.

The built 2134 rootfs modifies `init.rc` and adds `sbin/early2134-adb.sh`;
all other original entries, modules and libraries are preserved. The gadget
and stock adbd start during `on init`, before the vendor mount/data/module
sequence. Eight separate `/run/early2134-phase-*` files and a helper log
record executed checkpoints. The product is `RI2134-ADB01`, not a PS1 change.
Image SHA-256:
`d35bb0c89d2fc6d8188839173b0934b5127c5a751eea4ad68d4980fe76fe710d`.

The code update changes 763 blocks in `[0x00020000,0x08320000)`, with
pre-bootloader copies last. Block0, app, factory, fw_stat, bad blocks and
BBT are preserved, and exposed OOB is unchanged. Six normal vendor boot/kernel
representations were compared with actual prior programmed readback; the
package's special block0 payload is not copied as raw NAND data. No vendor
whole-chip command or image99 is used.

The sealed update is in `build/artifacts/2134-early-adb-update-01-20260911/`.
Plan SHA-256:
`d142d5d1b0bb795da17dae4aac3162c6b955c3797603bdf23a47dec75fc912b8`;
target main-data SHA-256:
`02c36e6675656cee07c5fdaf8aedf91c8e6586c3729baf734672722b8891c6cc`.
Live current-state preflight passed with two corrected reads and no failed
ECC events. Execution evidence is in `evidence/2134-early-adb-01-20260911/`.
The observer was running and calibrated before the normal power-cycle request.
The subsequent speed review measured a redundant 104-second standalone
preflight and substantially longer manual preparation. The
private host trial runner, `tools/nand-inspect/trial-runner.md`, removes that duplicate
preflight and hand-assembled staging, while keeping the engine's own checks.
Its live read-only protocol checks exposed legacy ADB incompatibilities missed
by the original mocks; those were fixed and retested with positive and
deliberate-failure controls. No NAND apply has used the new wrapper.

### Results before reconstruction

**The vendor-stack/reInvoke flash completed, but normal boot failed acceptance.
Yellow-mode U-Boot and RAM ADB still work after that flash.**

The first vendor-stack attempt produced spinning lights with no observed USB,
ADB or responding Invoke Bluetooth endpoint. The later BSL-only variation
changed the result again: the owner's normal power cycle returned the Marvell
`FF` downloader, without BSL/pilot ADB. Neither result meets standalone startup
acceptance; the exact failure stage remains unknown.

At 01:51:52 UTC on September 11, an owner-assisted yellow-mode entry returned
a fresh U-Boot version response and prompt. The already-proven custom kernel
and RC12 handoff diagnostic subsequently returned a real ADB shell. The unit
was used for independent readback and the BSL-only forward write. After the
BSL variation's normal-boot failure, the pinned helper attached to the returned
FF endpoint without another physical reset and a fresh U-Boot command worked
at 02:43:10 UTC. The known RAM diagnostic was subsequently loaded for the
compact BSL write. That write and independent readback passed, but both normal
boot tests returned persistent FF without ADB. The current state is Marvell FF
USB only at the end of that test. The later access and inventory are described
above; they did not alter the installed compact image.

### Executed first multipart attempt

The owner explicitly approved one `l2nand 83` invocation of the
61,982,272-byte image with SHA-256
`25b1553e1278568eed1ddda9a0411e0dabc1c80ccce2346659bfb57f89cca4f7`.
The full image transferred, all eight record program/read loops completed,
the vendor reported success, and a fresh command confirmed the returned
prompt. Image 99 was not used.

The flasher reported erasing 2,046 blocks, the whole device apart from the two
known bad blocks, before programming. Omitting descriptors did not protect
their allocations from that erase. The active flash image was removed and the
firmware server stopped before the normal-boot test. There was no automatic
retry or rollback.

The auxiliary `07_IMAGE` still contained the earlier RAM image length,
34,557,407 bytes. U-Boot printed that length, while transport logs show the
complete 61,982,272-byte image and all record loops, including its final BSL
record. This preflight omission is recorded rather than dismissed or corrected
by an automatic second flash.

Independent post-flash reads through the read-only NAND character device found:

* The complete 40,370,176-byte rootfs extent still matches
  `c580a8ff440fee68bd001777a16e24b6feb4446e84b1e7d44d34cd1fd3f37e17`.
  The read accumulated one corrected ECC event and no failed-ECC events.
* All 5 MiB of `factory_setting` are `0xff`, exactly as in the September 7
  capture. This comparison does not support a newly lost factory-data cause.
* All 1 MiB of `fw_stat` are now `0xff`. The prior capture had 38,916 non-FF
  bytes in its first two blocks. No saved status data will be restored: the
  owner explicitly prohibited restoration.

Evidence: `evidence/reinvoke-install-attempt-01-20260911/` for the write and
normal-boot observation; `evidence/nand-postbundle-yellow-20260911T0149Z/`
for recovery and independent readback. The original proposal remains in
`build/artifacts/reinvoke-install-attempt-01-20260911/` as an execution record,
not instructions to repeat `l2nand`.

### Historical forward variation: BSL launcher v2

The next candidate changes only the 5 MiB BSL allocation
`[0x01a20000,0x01f20000)`, not the rootfs, bootloaders or saved status.
The retained vendor BSL `etc/init.d/rcS` mounts another NAND filesystem at
`/lsync` and executes `/lsync/rbua/run.sh`; it does not start USB ADB.
Whether the failed normal boot actually reached that path remains an inference.

The new reInvoke launcher instead starts early USB ADB, records the real kernel,
PID and mounts, locates the existing rootfs by its exact partition identity or
the previously tested read-only whole-NAND loop view, verifies the bootstrap
and runtime hashes, and hands PID 1 into the installed reInvoke runtime.
Missing inputs leave a diagnostic failure rather than launching an updater.
No old firmware, factory bytes, update flags or bonds are restored.

Candidate: `build/artifacts/reinvoke-bsl-v2-20260911-slim02/`.
The filesystem is 5,111,808 bytes, SHA-256
`16921ec74f3f88f19bba741319f9a60c2adefbf6da2e655b418f0ebc564d3194`;
the 5 MiB padded payload is
`91a9dde15fea2959232fc9a0cdafdb739e08a7498c4e8b57d867889c8f358163`.
Full extraction metadata/content checks, dependency checks and actual ARM
loader checks passed. The owner approved the BSL-only forward variation after
all 40 blocks passed live preflight with zero corrected/failed ECC events.

The fixed writer then changed blocks 1 through 38 followed by header block 0,
preserving block 39. Full extent verification and mapping cleanup returned
success. Independent reads match the new BSL payload and the unchanged complete
main rootfs; both adjacent blocks and the captured pre-bootloader prefix also
remain byte-identical. No status, factory data or old firmware was restored.

The owner confirmed the normal power cycle without a boot server. The passive
watcher recorded USB leaving the RAM diagnostic at 02:38:28 UTC and returning
as Marvell `FF` at 02:38:58. A second observation at 02:39:37 still showed FF
and no ADB. Neither `reInvoke-BSL-v2` nor the pilot interface was observed.
This is a changed boot result, not proof that the BSL script ran or that the
earlier bootloader state was restored. No old state was restored.

The watcher was stopped before attaching the pinned recovery helper. A fresh
U-Boot version response at 02:43:10 proved console access without another
physical reset. That host-assisted recovery is distinct from the failed normal
boot. Evidence is in `evidence/nand-bsl-v2-write-20260911/`, including the
approval, write log, `INDEPENDENT-VERIFICATION.json` and normal-boot observations;
the subsequent console is in `evidence/nand-bsl-v2-uboot-20260911/`.
Do not repeat the same write or use the whole-chip vendor command.

### Compact BSL size-only variation

The owner requested the next forward write and boot test on September 11.
The compact candidate keeps the BSL v2 startup script, ADB and all other files
unchanged except non-runtime debug sections in `libc` and `libpthread`.
Its filesystem is 2,445,312 bytes, down from 5,111,808 and below the vendor
payload's 2,715,648 bytes. This tests image size, not a verified diagnosis of
the previous failure.

Program headers and PT_LOAD contents compare identically except the ELF
header fields locating/counting the removed section table. Loader checks and
failure controls for changed entry points, segment layouts and loaded bytes
passed. Full filesystem extraction compared all 409 entries. The exact startup
hash remains `75c9b3d1e4da39b7ccfeb3e23866de9e510b210fef62027a84f265a75772ac8c`.

The new `nandbslcompact` profile retains the same 5 MiB target and rejects
restore/resume. Its 67 targeted tests passed, along with selected previous-BSL
and default-profile regressions. The actual plan changes blocks 8 through 38,
then header block 0; blocks 1 through 7 and 39 are preserved. All 40 live
preflight blocks passed with zero ECC corrections/failures and clean close.
No whole-chip vendor command, rootfs or saved-state rewrite is included.

Artifact: `build/artifacts/reinvoke-bsl-compact-20260911/`.
Filesystem SHA-256:
`d23e1844df71cf58b5f1313038116bc1055db9432cfb726a074e43de52bfee88`.
Padded payload:
`606923f27236a1bf86807c19354c772dea65ddefca73a0ba075a59f68c4ffec1`.
The single compact write completed with the planned 32 erases, zero writer ECC
corrections/failures, full-extent verification and successful mapping cleanup.
Independent post-write reads match the compact BSL; the full rootfs, adjacent
blocks, pre-bootloader prefix and eight preserved BSL blocks remain unchanged.
No new failed-ECC events occurred during the independent reads.

Execution evidence is in `evidence/nand-bsl-compact-20260911/`, including
`writer-result.json` and `INDEPENDENT-VERIFICATION.json`. The USB product and
startup script remain the v2 versions deliberately: this variation changes
size, not logic.

Both owner-confirmed normal-boot tests failed acceptance. After the cable-free
start, USB returned as FF at 03:41:25 UTC. A second observation-only power cycle
kept USB connected with the firmware server off: the passive watcher saw
disconnect at 03:47:38 and re-enumeration as FF at 03:47:54. The final check at
03:48:49 still showed FF and no ADB. No BSL or pilot USB identity was observed
at the 250 ms sampling cadence. The watcher was then stopped.

The compact image being smaller than the vendor payload did not resolve this
failure. This does not identify the failing boot stage or prove that Linux
never executed. There was no second write during the observation-only test
and no restoration. See `normal-boot-result.json` and
`early-connected-watch.jsonl` in that evidence directory.

### Earlier rootfs-only history

* **Verified on this unit:** the final 38.5 MiB NAND extent matched the approved
  payload byte-for-byte, independently of the writer. Mapping cleanup and the
  returned command status passed.
* **Verified on this unit:** during the owner's normal power-cycle, USB left
  the RAM session at 23:51:31 UTC and reappeared at 23:52:32 as Marvell
  `1286:8174`, subclass `FF`. Neither the pilot USB product nor ADB appeared.
  No host firmware server was running.
* **Unknown:** why ordinary startup selected or remained in that download
  state. A spinning light pattern is not a decoder for the USB subclass.
* **Verified on this unit, September 10:** a host-assisted PID 1 handoff into
  the read-only NAND copy reached its runtime and a real ADB shell. This is
  not a normal power-on or a vendor-kernel acceptance result.
* The owner required retaining reInvoke rather than restoring the original
  rootfs, and excluding image 99.

The later kernel-only diagnostic did not expose USB/ADB. The owner supplied
another normal power-cycle and the known RAM system was recovered; an actual
shell on ADB port 5037 was verified at 00:46 UTC on September 10.

At the next attended diagnostic on September 10, the prepared handoff RAM
image transferred completely, but USB/ADB did not return. At that point there
was no shell. No NAND program/erase command was issued. Both unsuccessful tests
introduced a boot-time MTD partition definition; whether that prevents kernel
initialization or a later step fails remains unknown without a kernel trace.

A revised RAM-first diagnostic was tried with the known boot arguments and no
boot-time partition change. It also failed to expose USB/ADB before any mount
request could be sent. This rules out treating the partition arguments as the
established cause; the custom diagnostic startup and earlier kernel execution
remain undistinguished. No additional NAND write occurred.

The owner supplied another normal power-cycle. The exact previously successful
RC12 image returned ADB at 10:08:41 UTC; an actual shell, corrected kernel
identity, and RAM-only mounts were verified. NAND remains unchanged.

From that working shell, opening the console succeeded. Each USB configuration
write used by the failed diagnostic also returned zero; the host observed the
changed USB product and its restoration, then another shell worked. Those are
live-operation tests, not proof of the cold-start ordering. Packed-image
comparison found only the replacement init, the added loop helper, and an
unintended root-directory mode change from 0755 to 0700. None is yet a proved
explanation of the missing early ADB.

The wholesale replacement init is withdrawn. A narrower RC12 image preserves
every original startup byte through the working ADB launch and adds one
host-gated handoff hook before radio/audio services start. All other original
file contents and metadata, including root mode 0755, are preserved. A
deliberately altered startup fails the archive check. Image:
`build/artifacts/rc12-nand-handoff-20260910/82_IMAGE.rc12-handoff`,
SHA-256 `9f1182123fa08791d7c00d3c8e7d049a81be1565a7e387bbbb06dc09a0a5b5ba`.
No boot-time partition arguments or automatic NAND mount/write are added.
The owner confirmed its power-cycle, and a real shell reached the waiting
hook. Evidence for the earlier failures and control recovery is in
`evidence/nand-ram-first-handoff-20260910/`.

### September 10 executed NAND runtime

At 10:31 UTC the host requested the handoff, after verifying the actual loop
offset `0x02920000`, length `0x02680000`, read-only access, and both the NAND
bootstrap and complete compressed runtime hashes. Observations:

* USB changed from the RC12 diagnostic product to `reInvoke-NAND-pilot-01`.
  Repeated actual shell commands succeeded beyond the fallback window.
* PID 1's init matched the installed runtime manifest:
  `d927037fd8b4f73be53faab7ccddb0b4c0dcc31b92f5d2a0c6fbc5cfd36b395d`.
  The recorded phase was `runtime-dispatched`.
* Kernel mount records showed the bootstrap entering the read-only SquashFS,
  then a tmpfs runtime root with read-only `/nand-source` and identity binds.
  The loop remained backed by the whole NAND block device at the tested bounds.
* Direct process command lines and two status samples showed unchanged PIDs for
  ADB, MCU, DSP, networking, Bluetooth and BlueALSA. This is startup/process
  evidence, not a new functional audio or microphone acceptance run.
* Status retained a pairing-agent-guard retry failure. It was not hidden or
  treated as an all-green service result.

The status tool labels loop mounts `non-nand-squashfs-unattested`; it cannot
distinguish this NAND-backed loop from a regular-file rehearsal. The independent
host mount/offset/hash records establish the source for this experiment, not
that status label. The command line still identifies the USB-loaded kernel and
RAM bridge. No additional NAND write was made.

The bridge had already initialized device nodes, loopback and USB. This result
therefore does not prove that the NAND bootstrap works as the first userspace
process on a cold kernel. At 10:40 UTC the host boot helper and console relay
were stopped; another actual runtime shell still worked. They are no longer
armed to supply firmware on an unexpected power-cycle.

Evidence: `evidence/rc12-nand-handoff-20260910/`, including
`nand-mounted-verified.txt`, `post-handoff-shell.txt`,
`nand-runtime-independent-checks.txt`, `runtime-source-backing.txt`, both
runtime status samples, the copied bootstrap diagnostics, and
`boot-server-stopped-shell.txt`.

### September 10 bundle checkpoint

Within the time-boxed next attempt, an offline eight-record vendor/reInvoke
candidate was assembled and independently checked. It retains the base vendor
boot/kernel bytes, substitutes the existing pilot rootfs, and excludes
`99_IMAGE`, the donor `app` record and the ZIP trailer. The trailer was confirmed
to contain factory key/certificate files and Bluetooth configuration.
It is not staged for flashing.

The candidate is in
`build/artifacts/reinvoke-vendor83-candidate-20260910/`, named
`83_IMAGE.candidate`, with an explicit not-flash-approved manifest. Hash:
`25b1553e1278568eed1ddda9a0411e0dabc1c80ccce2346659bfb57f89cca4f7`.
All eight descriptor CRCs and the unit allocation comparison pass; a corrupt
payload is rejected by the separate inspector.

The base package identifies as `Barracuda_libre-12.2134.0`. Its three normal
radio modules exactly match the pilot's stock-kernel modules. That supports
component matching, but not a claim that this bundle boots.

The installed pilot's PID 1 handoff is now demonstrated with the custom
RAM-loaded kernel. The candidate vendor kernel and normal power-on boot chain
remain untested, as do the vendor flasher's exact effects for a package without
the donor app/trailer. Do not treat the custom-kernel result as resolving those
separate questions.

### Multipart flasher review: not cleared

The focused review did not obtain the device flasher's parsing and erase
implementation. Its proposed safety conclusions were not accepted:

* **Independent public source:** the
  [PodiumFlashing parser at the pinned revision](https://github.com/CaramelKat/PodiumFlashing/blob/8ccae0e019323474bbc9672a800eef902d0b492a/imager.py)
  reads a region count but starts payloads at a fixed `imageOffset = 640`.
  Its builder also preserves a footer. Our eight-record candidate starts
  payloads at 576. This establishes a host-tool assumption, not the same
  behavior in U-Boot, and does not validate the reduced package.
* **Unknown:** whether the actual flasher accepts eight records, ignores the
  trailer, erases only described allocations, or updates factory/status/BBT
  data outside them. A booting nine-record community flash does not exclude
  those side effects. The log message `Erase NAND chip...` alone does not
  establish erase scope.
* **Insufficient evidence:** finding a Linux userspace `fw_stat` writer does
  not prove that U-Boot cannot also update it. Likewise, a host descriptor
  parser does not prove the target has no positional assumptions.
* **Rejected experiment:** zero-length descriptors or targets in an already
  blank factory block are not non-destructive tests of `l2nand`. Erase can
  precede data validation, and the erase boundary is precisely what is unknown.
  No such package will be sent as a dry run.

The next non-flashing avenue is to locate the executing U-Boot image with its
board-information command, then assess a bounded read of its RAM-resident code.
Only a reported DRAM/code range is eligible; no guessed MMIO scan, `imls`, or
NAND-writer invocation is part of that step. If the executable bytes can be
read, inspect the actual command path rather than importing host-tool
assumptions. Availability of such a read is not yet demonstrated.

### RAM-resident U-Boot inspection and read-command failure

After the next owner-confirmed power-cycle, `bdinfo` reported DRAM
`[0x00000000,0x20000000)`, relocated U-Boot at `0x1feb7000`, and the TLB at
`0x1fff0000`. A 64-byte read at the reported code address returned ARM
instructions; its independently requested target CRC32 was `da050cbf`, matching
the host-decoded bytes. Corrupted, incomplete and wrong-range controls failed.
This proves that at least this running-code prefix is readable without
decrypting the packaged image on the host.

A subsequent 64 KiB `md.b` request was too aggressive. The console displayed
only the first 1 KiB, then reported a prefetch abort with PC `0x6566317c` and
reset U-Boot. It stopped after the DRAM banner, with Marvell USB still present
but no returned prompt or ADB. No NAND program/erase command was issued.
The exact defect is unknown; text-like register values suggest a display-path
corruption, not proof of bad DRAM or NAND. The large console-dump collector is
withdrawn and rejects execution; its offline parser checks remain usable.

The immediate next hardware action is owner-confirmed recovery, not another
large console read. Linux access to this address is not automatically qualified
by its readability in U-Boot; it requires separate memory-map and access-path
review. Evidence: `evidence/uboot-resident-read-20260910/`, including the complete
abort transcript and the disabled collector.

### Rubber-duck review and next-action boundary

The review identified three high-priority errors and two medium-priority
process weaknesses:

| Priority | Finding |
|---|---|
| High | A successful 64-byte read and host CRC checks were incorrectly generalized to a 64 KiB device display request. |
| High | The collector used the packaged image's 419,840-byte length as a resident-memory extent without establishing that equivalence. |
| High | The recovery loader remained armed after the owner selected pause. It and its two identified host helpers have now been stopped; port 8141 was verified not listening. |
| Medium | The host timeout stopped waiting; it did not cancel or contain the command already running on the device. |
| Medium | Investigation expanded into new hardware probes before each probe's decision value, evidence boundary and stop condition were fixed. |

Next: recover with the already-proven images and verify the real shell and
NAND runtime handoff, without flashing. Keep new investigation separate from
that recovery. The existing partial transcript contains relocation-header words
that resemble the
[upstream U-Boot 2013.04 startup layout](https://github.com/u-boot/u-boot/blob/v2013.04/arch/arm/cpu/armv7/start.S);
review those and their instruction references offline before selecting any
further read range. The correspondence is a lead, not yet a verified bound for
a new collector. No automatic switch to Linux physical-memory reads is approved
by this review. Any resumed device investigation needs its own explicit scope
and time limit, and another missing response ends the attempt.

## Historical installed pilot on September 9

| Item | Value |
|---|---|
| Build | `reInvoke-NAND-pilot-01-20260909`, PTY-corrected artifact |
| Written extent | `[0x02920000,0x04fa0000)`, 308 x 128 KiB blocks |
| Filesystem bytes | 40,267,776 |
| Padded payload / original rollback bytes | 40,370,176 |
| Filesystem SHA-256 | `2f1622c5573a1dfe777f5595206d5750a2afb11e22809f8c0e063189fa27624a` |
| Installed payload SHA-256 | `c580a8ff440fee68bd001777a16e24b6feb4446e84b1e7d44d34cd1fd3f37e17` |
| Exact original rollback SHA-256 | `3bb7f29b7961424c9b27d3e908477643840a6cf3967f65a0e7ac33990c26fc6d` |

The [pilot](../tools/nand-pilot/README.md) contains actual RC12-derived services,
early supervised ADB, a shell, build identity, and a status command. Its
read-only NAND bootstrap extracts runtime files into RAM. That is compatible
with host-independent startup if the kernel and bootstrap actually execute.
The runtime binaries and BusyBox were retained rather than rewritten.

Kernel images, bootloaders, factory data, `app`, `fw_stat` and BBT storage were
not included in the approved write. Kernel compatibility and the actual NAND
entrypoint were not demonstrated by the RAM-file rehearsal.

The first pilot also keeps new Wi-Fi credentials and bonds in RAM. Even a
successful boot would not yet deliver persistent network provisioning across
power cycles. That product gap must not be called a finished installation.

## Historical kernel-only tests on September 10

The known kernel can mount this already-written SquashFS and execute its
`/init` through the demonstrated RAM bridge. Whether the installed boot chain
could start a compatible kernel and runtime without a host was unresolved at
that checkpoint. Candidate 02 later established useful native startup, without
isolating the cause of these earlier failures.

The first attempted experiment supplied a **host-loaded known kernel,
NAND-loaded runtime**, without a RAM bridge:

1. Reach recovery U-Boot using the existing retained helper.
2. Supply only the known kernel, not the RC12 `82_IMAGE` runtime.
3. Give the kernel a single read-only MTD partition at the installed rootfs
   extent and select that partition as its root.
4. Observe the pilot's early USB identity, real shell, build/status output,
   source mount and service startup.

This kernel-only test was executed on September 10 with no external runtime
image or NAND write. USB disappeared and no early diagnostics returned. No
Linux trace was obtained, so mounting and PID 1 startup are not distinguished.
After owner-assisted RAM recovery, the installed SquashFS and its complete
runtime payload were read successfully through a read-only NAND block view.
That is filesystem-read evidence, not PID 1 boot acceptance.

The legacy BusyBox loop tool silently left its requested offset at zero.
The failed mount from that incorrect view was not a corrupt-image result.
A narrowly scoped diagnostic set and read back the private loop's actual
offset and size using the standard kernel ioctl, after which the mount and
payload hashes passed. All temporary mounts/nodes were removed.

A RAM-only handoff diagnostic with boot-time partitioning was subsequently
run, but did not expose its fallback ADB. The replacement RAM-first init also
failed without that partition change. The narrower, original-startup hook
above subsequently executed the NAND runtime; neither failed image is a retry
instruction. The remaining standalone step is still the power-on boot chain
and candidate kernel. These diagnostics are not proof that the multipart
firmware candidate is safe or bootable.

Independent basis: the retained Harman kernel's Berlin NFC driver parses
`cmdlinepart`; its built-in MTD block and SquashFS drivers can supply root.
[Upstream Linux documents the same `mtdparts` syntax](https://github.com/torvalds/linux/blob/v3.8/drivers/mtd/cmdlinepart.c).
Exact installed kernel selection, boot-container acceptance and the cause of
`FF` remain unknown. Do not infer them from our own image builder or from
recovery U-Boot's `verify=n`.

### Independent community evidence

The original [StockRoot release](https://github.com/coggy9/HKHacking/releases/tag/StockRoot)
was published on April 17, 2021. An independent owner reported hearing the
changed startup sound, completing setup and connecting through network ADB
after flashing it:
[first-person result](https://github.com/coggy9/HKHacking/discussions/3#discussioncomment-623161).
That report explicitly says service-port ADB was not visible. This is evidence
of executed custom firmware, not a USB prompt inferred to mean success.

The successful procedure used multipart `83_IMAGE`. The author described it as
including bootloader and DRM regions:
[container explanation](https://github.com/coggy9/HKHacking/discussions/3#discussioncomment-623311).
The earlier [repacking experiment](https://github.com/coggy9/HKHacking/discussions/3#discussioncomment-607823)
also encountered an `app`-region flashing failure. Those reports do not validate
our rootfs-only procedure, justify reflashing every region, or establish a
safe existing-NAND boot command.

The [independent repacker](https://github.com/CaramelKat/PodiumFlashing/blob/8ccae0e019323474bbc9672a800eef902d0b492a/imager.py)
rebuilds gzip SquashFS and updates container CRCs; it is not evidence of
cryptographic kernel signing. The successful owner's "Secure Boot" and
"self sign drivers" discussion concerns the Windows host's USB driver, not
Invoke kernel authentication.

The public [USB helper](https://github.com/jryruegas92/hk-invoke-arm-flasher/blob/63444e82cc5274abe31ec49ad55ee552b50b64b3/src/usb_boot_arm.c)
branches on the **interface** subclass to supply the bootstrap. Our recent
passive logs also record the **device** subclass; these are distinct fields.
Neither is a documented decoder for a failed signature, NAND fault or boot
strap. Bounded research found no Invoke-specific explanation for why this
ordinary boot remains at `FF`.

## Historical workflow recommendation before candidate 02

This rootfs-writer workflow records the earlier bounded-write policy. It is
not the later whole-good-block vendor operation: candidate 02 used nine
vendor write/read loops and went directly from U-Boot to the owner's first
native boot without independent Linux readback. Any successor needs its own
explicitly reviewed operation and approval.

| Step | Required result |
|---|---|
| Prepare once | One frozen image, exact extent, rollback, identified kernel and agreed ECC policy |
| Check once | Current target matches the expected state; bounds, bad-block status and attributable ECC checks pass |
| Write once | Stable reviewed writer, bounded extent, visible progress, stop on unexpected errors |
| Verify once | Complete data/ECC verification, successful cleanup/exit, one independent target readback |
| Boot once | No host image server; record USB stages, one ADB server, build and boot-origin evidence |

Reuse the existing builder, writer and evidence. No new agent team, planning
stack or bespoke installer for every attempt. Retain the raw logs, but show
the owner a short phase/block-count/failure summary.

Keep these safeguards:

* Explicit approval for the particular write; no automatic rollback or reboot.
* Fixed bounds and exact payload/rollback hashes.
* Hardware ECC, exact returned data and failure-counter **deltas**, not lifetime
  totals. The approved pilot resume allowed at most one correction per actual
  candidate-page main read and separately per exposed-OOB read.
* A real completion check after cleanup, not merely an earlier progress line.

Drop repeated full-chip captures when no new question requires them, unrelated
binary hardening, redundant whole-suite runs, repeated Bluetooth scans while
only the downloader is visible, and repeated physical resets without a new
hypothesis.

The existing writer still has historical build profiles and a fixed mixed-state
resume. **It is not a general resumable updater.** Do not reuse that resume
against the now-complete image. Simplifying its code/policy for a future release
must happen before the next write, not during one.

## Critique of this effort

1. We should have established a contemporary normal-boot baseline before
   changing NAND. A historic brief `FE` endpoint was not proof of a running
   stock system.
2. We proved storage integrity more thoroughly than the boot integration that
   the product needed. A larger rootfs did not establish a route past the
   already-observed early downloader.
3. I conflated download mode, U-Boot, RAM Linux and stock ADB, and initially
   missed the competing ADB servers. The alleged "08-only" helper actually
   served the recovery chain. Those interpretations were wrong.
4. Zero corrections on every fresh read was an experiment policy, not a vendor
   requirement. Introducing a one-page exception and writing resume software
   after the stop added substantial avoidable delay. Agree on a measured
   policy and prepare continuation before writing.
5. Review and negative controls were useful: they caught a kernel cleanup
   defect, missing PTY device, unreachable CLI dispatch and counter-baseline
   mistakes. Passing host tests still did not exercise hardware boot.
6. Agent-message loops, stale artifact announcements, unnecessary BusyBox
   hardening and contradictory runbooks caused overhead without advancing the
   home-assistant goal. One execution owner should freeze and hand off artifacts.
7. The 154x figure was the data-size ratio, not a time estimate. The hardware
   operations took minutes; software churn caused the hours.

## Unit facts to preserve

All offsets are end-exclusive. Allocation evidence comes from eight identical,
CRC-valid captured version tables, not the example 512 MiB vendor layout.

| Allocation | Start | End |
|---|---|---|
| `block0` | `0x00000000` | `0x00020000` |
| `pre-bootloader` | `0x00020000` | `0x00120000` |
| `post-bootloader` | `0x00120000` | `0x00320000` |
| `postbootloaderB` | `0x00320000` | `0x00520000` |
| `factory_setting` | `0x00520000` | `0x00a20000` |
| `tz_en` | `0x00a20000` | `0x00f20000` |
| `tz_en-B` | `0x00f20000` | `0x01020000` |
| `bootimgs_B` | `0x01020000` | `0x01a20000` |
| `bsl` | `0x01a20000` | `0x01f20000` |
| `bootimgs` | `0x01f20000` | `0x02920000` |
| `rootfs` | `0x02920000` | `0x08320000` |
| `app` | `0x08320000` | `0x0fe20000` |
| `fw_stat` | `0x0fe20000` | `0x0ff20000` |

Toshiba ID `98 DA 90 15 76 16`; 256 MiB main area, 2 KiB pages, 128 KiB erase
blocks. Linux declares 64 OOB bytes; 32 are meaningfully exposed by this
controller. The ID prefix maps upstream to 128 physical OOB bytes. Captures are
not programmer-raw restores. The retained source selects 48-bit BCH per 2 KiB.

The five historically uncorrectable pages are in `fw_stat`; the two bad blocks
at `0x0c000000` and `0x0c020000` are in `app`. The tail contains mirrored BBT
storage. The unexplained erased factory block at `0x00660000` predates these
pilot writes and must not be repurposed.

Before the complete pilot, the restored entire main area and controller-visible
OOB matched September 7 captures. That is not a statement about current contents
after installing the pilot, hidden physical OOB, or correctness of the five
ECC-failing pages.

## Evidence and tools

All firmware, private configuration and generated artifacts stay in the sibling
`reinvoke-archive`, not Git.

* Historical pilot artifact: `build/artifacts/reinvoke-nand-pilot-01-20260909-pty01/`
* Final write/readback: `evidence/nand-pilot-uniform-resume-20260909/`
* Failed normal boot: `normal-boot-watch.log` in that evidence directory
* Earlier full restored comparison: `evidence/nand-restored-ram-inspection-20260909/`
* Preserved pre-consolidation runbooks: `evidence/nand-workflow-consolidation-20260909/`
* Private/deferred writer contracts: `tools/nand-inspect/probe-writer.md`
* [Recovery access](uboot-access.md)
* [Earlier two-block experiment history](nand-startup-probe.md)
* [Runtime contract](current-product-contract.md)
