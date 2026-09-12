---
title: Project journal
description: Engineering milestones, corrected assumptions, and consequences for the local-assistant platform
ms.date: 2026-09-12
ms.topic: reference
---

reInvoke progressed from retained firmware and emulated services to
closed-enclosure RAM execution, then autonomous NAND userspace.
These August-September 2026 milestones connect measured results to the
decisions they enabled.
Detailed trial records remain in Git history and the private archive.
The [product contract](current-product-contract.md) defines current behavior;
[remaining work](revival-roadmap.md#remaining-work) tracks the assistant goal.

## August 28: firmware and off-device control

Preserved ARM Bonefish and vendor clients ran under `qemu-user` in a rootless
sandbox. A third-party WAMP mute call changed state. MCU/audio services were
clients, not separate unknown listeners. A corrected extraction found 165
control URIs; the original lowercase-only search had truncated camelCase names,
which the live router rejected. Protocol discovery could proceed off-device,
without treating emulation as physical hardware acceptance.

Final vendor firmware `Barracuda_libre-12.2134.0` removed Cortana, its harness,
Spotify and Skype components and added Bluetooth-speaker-oriented UI and
`wifi-blocker`. This established a vendor Bluetooth baseline, not a completed
reInvoke assistant or the firmware actually installed on the observed unit.
See [firmware generations](firmware-reference.md#firmware-generations).

Twenty FCC exhibits were retained with provenance. Photographs supported DRAM,
wireless and Micro-USB data-path findings while leaving unreadable markings
unresolved. Sibling Berlin sources supplied audio/I2C references; later
Invoke-specific source determined the usable ASoC path.
The [hardware corpus](corpus/01_CANONICAL_HARDWARE_BASELINE.md) and
[source cross-index](corpus/05_SIBLING_SOURCE_CROSSINDEX.md) retain those links.

## Late August: modeled hardware boundaries

Capturing donor `I2C_RDWR` requests required accounting for relocated guest
addresses and short-lived stack data. `i2c-stub` lacked raw I2C capability;
`snd-aloop` did not make QEMU forward the required control ioctls.
An ARM ioctl shim modeled the boundaries instead. Direct syscall forwarding
removed its accidental dependency on a newer host glibc.

The model answered startup I2C transactions and exposed ALSA controls.
WAMP volume arguments were value first, stream second; independent reads
confirmed state changes. The donor radio stack was Bluedroid, not BlueZ,
so emulation needed an HCI boundary rather than a D-Bus session.
See [control-plane emulation](emulation/control-plane-emulation.md).

Disassembly also showed `mtd_exec setbootflags` changing a `fw_stat` update
record. `qeru` and `puon` select update-required/no-update behavior; they were
not proved active-slot selectors. The
[boot/update analysis](emulation/boot-update-state.md) preserves that distinction.

## Closed-unit bring-up

September 1-3 closed-enclosure work replaced predictions with measurements.
The initial ordinary-power endpoint was `1286:8174`, briefly requesting
`08_IMAGE`, not the runtime gadget predicted from vendor `init.rc`.
Charge-only cables, an unattached helper console and telnet negotiation loops
were separate host-side false negatives.

Recovery-only staging and deliberate yellow-mode entry later served the full
`09_IMAGE` chain and reached responsive U-Boot. Both FE and FF were observed,
with different helper selection rules for device/interface subclass.
Neither subclass nor panel animation identifies the executing boot stage.
The [recovery procedure](uboot-access.md#verified-recovery-sequence) retains the
working sequence rather than every failed button/timing variation.

RAM Linux then executed owned PID 1, returned ADB and read NAND without mounting
it. The active SquashFS identified `Barracuda_libre-12.2050.3`, correcting the
earlier assumption that the unit already ran final `12.2134.0`.
Stock ADB was never established by the earlier gadget prediction.

USB identity, shell access, Bluetooth transport and audible output proved
distinct milestones. Later disconnected starts established host independence;
cable-only influence on boot remains unknown.

## September 2-3: kernel, audio and owned services

GCC 11.4/9.5 kernels did not return the expected USB gadget. NDK GCC 4.9 and
the proven recovery layout did; non-PIC modules fixed the radio loader failure.
SPI alone moved data but did not complete DSP startup. Adding the required
GPIO-bank base enabled MCU replies, DSP boot notification and version `25688`.
The Invoke ASoC increment then produced a guarded, audibly confirmed tone.
The [RAM platform](native-ram-platform.md) records the working input contract.

Donor disassembly recovered the DSP protocol, and a September 3 byte-exact
trace confirmed all 160,484 bit-reversed loader bytes over SPI. Message headers
were five bytes in both directions; donor log formatting had suggested three.
This established a volatile host-loaded DSP program, not PCM-over-SPI,
beamforming or AEC activation.

Donor Bluedroid paired and received SBC but failed PCM handoff. BlueZ/BlueALSA
replaced it and produced verified playback. Owned MCU/DSP and network services
replaced donor lifecycle policy while retaining board assets and Bonefish.
An inaudible automatic-unmute candidate with I2C arbitration loss was rejected;
positive PCM-read leases, ALSA ownership checks and edge-driven MCU reads
became the speaker-safety basis.

RAM capture required 16 periods of 2,048 bytes. Later attended speech/tap
correlation and all-zero muted capture established the software privacy path.
Network tests covered credential delivery, DHCP/DNS cleanup, restart and stale
owner controls. These measurements remain RAM-scoped, not native acceptance.

## September 7-11: NAND evidence and failed startup methods

CRC-valid tables corrected the allocation map and the `tz_en`/kernel confusion.
Repeat-matched main data and exposed OOB still lacked physical parity bytes;
five pages failed per-page ECC controls. A changed factory block remained
unexplained. These were useful logical backups, not raw programmer restores.

Temporary mapping removal exposed a kernel lifetime defect before any write.
The corrected kernel qualified mapping cleanup, two-block installation and
exact restoration, but native diagnostics still failed. Larger rootfs, BSL,
reconstruction, early-ADB and bounded StockRoot trials likewise demonstrated
storage persistence without useful startup. RAM-assisted NAND handoff executed
the runtime, but depended on already initialized USB/nodes.

The [NAND decision record](nand-write-decision.md#withdrawn-methods) retains
the distinctions that changed the approach. Large U-Boot dumps were withdrawn
after an abort; descriptor omission did not prevent whole-good-block erase;
guaranteed undo and full-erase recovery-test proposals were abandoned.

## September 11-12: autonomous native userspace

Candidate 02 completed a nine-record vendor whole-good-block operation and
started without a helper. Encrypted Bluetooth/A2DP, attended melody and rotary
volume, physical provisioning, eight RawSocket test groups and a real WebSocket
WAMP handshake established useful autonomy. No heartbeat appeared in 75 seconds.
Vendor programming, app seed, cleared state and USB-independent service startup
changed together, so the breakthrough's cause was not isolated.

Candidate 03's power-only start advertised `reInvoke-NAND`. Bluetooth connection
and physical Wi-Fi provisioning completed. TCP22 reached Dropbear 2026.94 and
the pinned host identity, then closed at user authentication. This was listener
acceptance, not a login or proved NSS failure. USB remained absent and TCP5555
refused; actual native kernel/PID 1/mount/firewall state remains unread via shell.
Candidate 02's broader acoustic/rotary/WAMP results do not transfer to 03.

Retained vendor bootloader, TrustZone and encrypted kernel payloads remained
byte-identical, but their blocks were erased/reprogrammed. Recovery worked after
the observed experiments, not arbitrary corruption. Credentials, bonds and
settings remain volatile. Native administration, remaining acceptance,
persistent configuration, assistant integration and distributable recovery
still separate this milestone from a future 1.0.0.
