# Project Journal

A dated record of what was done, what it changed, and what was wrong along the
way. `PLAN.md` describes the current state; this file describes how the project
arrived there.

Corrections are recorded deliberately. A preservation project whose governing
rule is that claims must trace to evidence has to be equally honest about the
claims that did not.

## 2026-08-28

### What changed

The project moved from static firmware analysis to three results that were not
available before: the control plane now runs off-device, the last firmware
Harman shipped was recovered and understood, and the regulatory evidence the
corpus depends on is finally held locally.

### The control plane is not proprietary

`system-manager` starts `bonefish`, an open-source WAMP router. Every subsystem
is a WAMP client of it. Because both the router and its clients are ordinary
ARM Linux executables in the preserved rootfs, the whole control plane can be
reconstituted on a workstation.

It was. Under `qemu-user` in a rootless `bwrap` sandbox, the router runs, the
services join, and a third-party client called `com.harman.musicMuteToggle` and
watched the service's own state change. See
[control-plane-emulation.md](emulation/control-plane-emulation.md).

The practical consequence is that protocol discovery is no longer a reason to
touch hardware. Work that previously implied bench capture on a physical unit
can be done in emulation, repeatedly and without risk.

### Harman already built the repurposed device

The OTA2 bundle had been downloaded and hash-verified in an earlier session but
never opened. It contains `Barracuda_libre-12.2134.0`, a 2021 build that is
newer than anything previously examined.

In it, Cortana, the Cortana harness, Spotify, and the Skype call library are
removed. `oobe-ui` and `wifi-blocker` are added, the latter carrying a
configuration file dated 2021-09-11. The emulated final firmware registers a
complete Bluetooth media control surface: pairing, transport, track selection,
repeat, shuffle, volume, mute, and shutdown.

This reframes the goal. Repurposing completeness was being treated as something
to build. Harman shipped a version of it. Whether a given unit already runs
that build is now the cheapest and most decisive question about it, which is why
it leads [the observation procedure](no-disassembly-observation-procedure.md).

### The evidence gap is closed

Twenty FCC exhibits for `APIHKINVOKE` are now held in the archive with
per-exhibit provenance sidecars. Until now the canonical hardware baseline
cited internal photographs as a proxy teardown while the project possessed none
of them.

Reading the held photographs produced identifications that were previously
listed as unresolvable, including the DRAM part and the Marvell Avastar
wireless combo, plus silkscreen confirming the Micro-USB service port carries
data lines. See [04_FCC_EXHIBIT_INVENTORY.md](corpus/04_FCC_EXHIBIT_INVENTORY.md).

### Sibling source finally cross-referenced

Roughly 4.3 GB of Berlin-family source had been mirrored and never read. The
highest-value item is a real Berlin audio-out driver in the Steam Link SDK,
which is the only such driver in the corpus and sits directly on the speaker
path. The Berlin I2C bus mapping was also resolved. See
[05_SIBLING_SOURCE_CROSSINDEX.md](corpus/05_SIBLING_SOURCE_CROSSINDEX.md).

The mainline Linux tree was reclassified. It had been ranked cite-only on the
grounds that a full mirror is multi-gigabyte, but that judgement compared the
whole tree against the need rather than the Berlin subset. A shallow, blobless,
path-filtered checkout is 44 MB, and `acquire.py` gained a `git_sparse`
acquisition kind so the decision is reproducible rather than a manual one-off.

### Corrections

Three confident claims failed verification during this session. All three were
plausible, and two of them were mine.

The Marvell USB boot toolchain was described as present in the repository. It
is not. That reading came from `LISTING.txt`, a manifest of the original zip,
whose file sizes were mistaken for files. The binaries were in the preserved
zip in the archive the whole time.

A reviewer then corrected that error in the opposite direction, reporting the
binaries as absent entirely and requiring re-acquisition from the internet. That
was also wrong; it had checked a stale archive path embedded in the manifest
header.

The runtime interface inventory recorded `mcu-interface` and `audio-ui` as
sharing a local service endpoint on port 9999 with an unknown wire protocol.
Two servers cannot bind one port. Both are clients, and the listener is
`bonefish`.

A fourth correction came from the emulator rather than a reviewer. An extraction
pass reported 130 control URIs, produced by a lowercase-only pattern that
truncated every camelCase name at its first capital letter. `com.harman.volumeSet`
was being recorded as `com.harman.volume`. Probing the truncated list against
the live router returned zero registered procedures, which is what exposed it.
The corrected count is 165, and the truncated names were not incomplete, they
were names that do not exist.

The pattern worth naming: every one of these was fluent, specific, and wrong.
Volume of analysis scales confident error as efficiently as it scales insight.
The evidence rule is what separates the two, and running the software turned
out to be the fastest way to enforce it.

### Still unresolved

Whether a physical unit ever exposes Marvell USB download mode without opening
the case. No BootROM, OTP, or secure-boot source exists anywhere in the corpus,
so this cannot be settled by analysis and remains a measurement.

The MCU's I2C slave addresses and register semantics. Emulation reaches the
bring-up sequence, including IO expander initialization, amplifier and DAC
muting, DSP power-on, and DAC initialization, but capturing register-level
traffic needs an I2C shim that answers rather than a placeholder file.

Argument shapes for the volume setters and the Bluetooth transport calls, both
blocked on sandbox dependencies rather than on protocol knowledge.

The daughterboard connector pinout, which no amount of software analysis can
resolve.
