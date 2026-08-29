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

### The owner's unit

The physical unit runs the final build, `Barracuda_libre-12.2134.0`. That
settles what would otherwise have been the first hardware question and makes
the emulation work directly applicable, since the sandbox runs the same
binaries the device runs.

Analysis of that build's reachable surface is in
[final-firmware-control-surface.md](emulation/final-firmware-control-surface.md).
The short version is that the control API exists and is fully documented, but on
stock firmware it listens on all interfaces and is blocked externally by the
firewall. `adbd` is present but not started, and the debug gate reads a DCT
record from the factory-setting partition. That partition is writable-mounted,
but the record's format and authentication remain unresolved.

The observation procedure now carries explicit predictions derived from that
analysis, so the hardware session either confirms or falsifies them rather than
being fitted to whatever is observed.

### Still unresolved

Whether a physical unit ever exposes Marvell USB download mode without opening
the case. No BootROM, OTP, or secure-boot source exists anywhere in the corpus,
so this cannot be settled by analysis and remains a measurement.

Argument shapes for the Bluetooth transport calls. These need an HCI transport
inside the sandbox. The earlier claim that they need BlueZ was wrong; see the
correction below.

The daughterboard connector pinout, which no amount of software analysis can
resolve.

### The MCU I2C capture

Recorded separately because it was the session's last blocked item and it came
unblocked.

The bring-up order was recoverable from the service's own log messages, but the
register traffic was not, because the transfers fail against a placeholder
device. Reading the `I2C_RDWR` structures out of the emulator's memory failed
twice before working. The first attempt hit a permissions wall, since
`ptrace_scope` is 1 and only a parent may read a process's memory, so the
capture had to spawn the sandbox itself. The second attempt read the right
process at the wrong addresses, because `qemu-user` relocates the guest and
trace addresses need `guest_base` applied. The third attempt read the right
addresses too late, since the request structs are stack locals that are
overwritten as soon as the call returns.

Polling during the bring-up window, with the base derived from where the guest
ELF landed, produced a stable set of transactions across three runs: three
slave addresses, `0x20` during IO expander initialization and muting, `0x4c`
receiving ten register writes during DAC initialization, and `0x36` in six-byte
frames after the analogue settle. See
[mcu-boundary.md](emulation/mcu-boundary.md).

The device identities behind those addresses are deliberately not claimed. An
access pattern narrows a device class; it does not name a part.

### Three suggestions that did not survive contact

Worth recording, because each was stated with more confidence than it deserved
and each was cheap to test.

Loading `i2c-stub` was proposed as the clean way to give the MCU service a bus
that answers. It cannot. The module implements SMBus only, reports no
`I2C_FUNC_I2C`, and rejects the raw `I2C_RDWR` transfers this firmware uses.
The initial `modprobe` also failed outright, because the module needs
`chip_addr` to instantiate anything, which was only knowable once the addresses
had already been recovered by other means.

Loading `snd-aloop` was proposed to make the volume setters work. The Loopback
card appears exactly where the device expects its DSP, and the sandbox sees
every relevant node, but `qemu-user` never forwards the ioctl.

`chmod 666` on a device node was suggested for the stub bus and was the wrong
instinct. The node was instead scoped to a group the user already held, which
avoids both world access and touching the host's real I2C buses. Worth noting
that the conventional fix, an `i2c` group with a subsystem-wide udev rule,
would have been broader than the sloppy thing it replaced.

### The ioctl boundary is now under test control

The cross-compiler changed the answer to two emulator limitations. Both raw
I2C and ALSA control failed at `ioctl()`, after the application had assembled
the complete request but before a host driver could act on it. An ARM
`LD_PRELOAD` library can intercept that shared boundary inside the guest.

The first build loaded but depended on `dlsym@GLIBC_2.34`, newer than the
firmware's glibc 2.23. Forwarding unhandled requests with a direct
`SYS_ioctl` syscall removed that dependency. The rebuilt ARM EABI5 library
requires only `GLIBC_2.4`; changing either host or firmware glibc was
unnecessary.

On the MCU path, the shim answers raw `I2C_RDWR` against a synthetic register
file. All 30 startup transactions now succeed. The acknowledged reads expose
the IO expander's state progression from `0x00` through `0x02`, `0x03`, and
`0x13`, while preserving the previously captured DAC and six-byte messages.

On the audio path, the shim supplies the control-card and mixer-element ioctls
that qemu-user omits. `audio-ui` initializes `music`, `call`, `voice`, `system`,
`timer`, and the `mic` mute control without ALSA errors. This made the real WAMP
setters executable and recovered their exact positional contract:

```json
{"volumeSet": [30, "music"],
 "volumeAdjust": [5, "music"],
 "musicMuteSet": [true, "music"]}
```

The order is value first, stream second. Starting at music volume 50, the calls
returned 30, then 35, then mute state 1. Independent `volumeGet` calls confirmed
each transition. Full-system emulation is no longer required for these two
boundaries.

### The update flag is not the boot slot

Disassembly closed another ambiguity. `/usr/bin/mtd_exec setbootflags` reads
2,832 bytes from the MTD partition labelled `fw_stat`, toggles its first word,
stages the complete record through `/run/stat`, and writes it back. `/run` is
tmpfs, so the file is staging rather than persistent state.

The two first-word markers are reversed-looking ASCII. `qeru` means
update-required and `puon` means no-update, confirmed by the binary's messages
and the recovery loader's comparison. The normal client invokes
`setbootflags`, waits 50 milliseconds, syncs, and enters its reboot path. The
recovery updater toggles the marker back on completion.

This establishes an update gate, not active-slot selection. The recovery
configuration targets `bootimgs` and `rootfs`, but preserved artifacts still do
not map a marker to a specific active or inactive image. See
[boot-update-state.md](emulation/boot-update-state.md).

### A wrong claim about the Bluetooth stack

Three documents stated that recovering the Bluetooth transport arguments needed
BlueZ and a D-Bus session inside the sandbox. That was wrong, and it was wrong
in the most expensive way: it named a concrete blocker that would have sent the
next session building an environment nothing in this firmware talks to.

`usr/bin/bluetooth` links neither `libbluetooth` nor `libdbus`. It links the
Android HAL libraries, and the rootfs carries `bluetooth.default.so`,
`libbt-vendor.so`, a Marvell `bt8xxx.ko` driver, and `sd8887_bt_a2_new.bin`
controller firmware. The stack is Bluedroid.

The real boundary is the kernel Bluetooth subsystem. `libbt-vendor.so` opens
`/dev/rfkill` and waits for an `hci%d` interface, so the sandbox needs an HCI
transport rather than a session bus. See
[bluetooth-stack.md](emulation/bluetooth-stack.md).

The error came from pattern-matching Linux Bluetooth to BlueZ instead of
reading the binary's dependencies. It is the same failure mode recorded earlier
in this journal: fluent, specific, and unverified.
