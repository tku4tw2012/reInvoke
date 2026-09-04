---
title: Pre-NAND platform implementation
description: Implementation log for completing the owned RAM-only replacement platform
ms.date: 2026-09-04
ms.topic: overview
---

## Status

Implementation is 92 percent complete for this RPI cycle. The host-side v9
candidate is complete, and the rebuilt MCU pinmux/rotary path passed an
attended live test. Cold-boot, audible, and acoustic hardware acceptance
remain.

## Completed in iteration 1

* Proved physical microphone DMA capture at stereo 48 kHz `S32_LE`,
  256-frame periods, and 16 periods while amplifier and DAC mute remained
  asserted.
* Decoupled the MCU heartbeat from WAMP connectivity and added bounded WAMP
  reconnect loops to the MCU and DSP services.
* Proved the same MCU process survives router loss and reconnects; traced two
  successful heartbeat ioctls 5.01 seconds apart during the test.
* Found the vendor DesignWare SPI driver can spin forever waiting for GPIO 13,
  leaving a process unkillable in `spidev_sync`.
* Added a checksum-gated 100 ms kernel timeout and built the
  `audio-sd8887` kernel successfully.
* Built and target-tested a static BlueALSA control utility.
* Built an isolated 5 MiB Bonefish and D-Bus runtime that does not replace the
  recovery image's EGLIBC libraries.
* Added a deterministic autonomous runtime bundle and complete PID 1
  supervision.
* Reduced the first 50.4 MB candidate to 29.1 MB by pruning unused recovery
  graphics/media files and stripping reviewed static binaries.
* Booted the compact image in five seconds. Router, MCU, DSP, D-Bus, BlueZ,
  BlueALSA, playback, pairing, networking, `mlan0`, and `hci0` started without
  host staging.
* Verified MCU status `000116`, exact DSP image download, DSP boot event,
  runtime manifest, no NAND mount, and removal of writable MTD nodes.

## Findings requiring iteration 2

* The bounded kernel wait prevents the prior permanent DSP wedge, but repeated
  GPIO-ready timeouts still cause DSP message-loop restarts. The underlying
  message-path handshake must be corrected before soak testing.
* The initramfs MTD cleanup missed nodes under `/dev/mtd/`; the live system was
  corrected and the next image includes the fixed glob.
* BlueALSA playback initially raced service registration. Supervision kept the
  process alive, but dependency readiness should replace failure-driven
  startup.
* The autonomous image has not yet completed fresh pairing, A2DP playback,
  target-side volume/mute authority, acoustic microphone correlation, or
  button/LED acceptance.

## Completed in iteration 2

* Re-paired the allowlisted host from the autonomous boot, closed host
  pairability, and exposed the expected BlueALSA A2DP PCM.
* Sent a five-second muted A2DP stream. ALSA card 1 reached `RUNNING`; its
  hardware pointer advanced from 78,848 to 123,904 while amplifier and DAC
  mute remained asserted.
* Added a target-side BlueALSA authority to the MCU service. Live WAMP
  `volumeGet`, `volumeSet`, `musicMuteSet`, and `musicMuteToggle` calls changed
  the real BlueALSA PCM state while external physical unmute remained denied.
* Added an ALSA playback-status policy that permits ordered DAC-then-amplifier
  unmute only while the physical playback PCM is `RUNNING`, and remutes on
  stop, cancellation, or error.
* Routed physical input handling independently of WAMP and serialized rotary
  updates with WAMP media operations.
* Added aggregate pinned-toolchain validation and machine-readable
  on-device/host acceptance collectors. The complete host suite passes.
* Corrected Bluetooth startup ordering so playback waits for BlueALSA service
  registration.
* Disassembled the donor DSP loop and proved the original owned transmit order
  was reversed. The corrected service now sends the command before waiting for
  the response-ready edge.
* A controlled donor-client test also failed its checksum on the heavily
  warm-reset DSP state. Final response validation therefore waits for one clean
  power cycle; no reset will be requested while the operator is away.
* Added five owned WAMP volume and mute procedures backed by the real BlueALSA
  PCM. Live calls read and changed volume, toggled software mute, and continued
  to reject direct physical unmute.
* Established a fresh autonomous RAM-only Bluetooth bond and completed a
  five-second muted A2DP-to-ALSA stream with advancing DMA pointers.
* Added deterministic v3 runtime and initramfs builds. Two independent builds
  are byte-identical; the candidate is 29,184,656 bytes with SHA-256
  `4653f48636ef6936c0061f18b0fe462af49155a2c871c8de3fed7b29c2bc83c8`.
* Added dependency readiness for BlueALSA playback and persistent per-boot
  service restart logs.
* The v3 candidate is held host-side. No reboot or image load will occur until
  the operator returns.

## Completed in iteration 3

* Closed all five findings from the first functional review.
* Recovered and implemented the complete physical key code table.
* Added bounded, coalescing rotary work so MCU interrupt acknowledgement never
  waits for BlueALSA subprocesses.
* Added Bluetooth-long-press signaling that reopens the bounded allowlisted
  pairing window. A two-second live signal test passed without reboot.
* Added microphone-button software mute toggle through the same serialized
  BlueALSA authority.
* Recovered and implemented the LED animation transport, including 13-byte
  frames, 390-byte chunks, first/later flags, cancellation, and shipped-asset
  checksum gating.
* Added startup, action, and pairing animations without copying assets into
  Git.
* Built the 29,204,587-byte v4 image candidate with SHA-256
  `6672addac08333ffc793ad15c5dacc81ab410edf968608b38cb0572e86842663`.
* The complete host validation suite passes. A follow-up functional review is
  running; no reset or image load will occur while the operator is away.

## Completed in iteration 4

* Cold-booted the v8 autonomous image and passed all 20 machine checks,
  including hashes, NAND isolation, radios, ALSA, DSP boot, service
  supervision, zombie detection, and fatal-kernel-error detection.
* Confirmed rotary events in both directions, two distinct microphone-mute
  events, Bluetooth long press, pairing-window reopening, and the owned white
  pairing indicator.
* Completed a fresh allowlisted RAM-only bond and verified BlueALSA PCM,
  target-side volume and mute authority, and muted A2DP DMA.
* Attempted a guarded 10 percent audible test. ALSA reached `RUNNING`, but the
  operator heard no sound and the kernel reported sustained I2C arbitration
  loss and timeouts.
* Rejected ALSA `RUNNING` as sufficient physical-unmute authority because
  `bluealsa-aplay` can hold that state while receiving silence.
* Traced the I2C read flood to a GPIO poll mask that accepted regular-file
  readiness instead of requiring a real priority edge.

## Completed in iteration 5

* Patched BlueALSA 4.0.0 so a positive PCM FIFO read creates a RAM lease
  containing the ALSA-owning worker thread ID. Inactivity, cancellation, and
  process exit remove the lease.
* Required ALSA `RUNNING`, matching lease and ALSA owner thread IDs, and the
  expected `/proc/<tid>/exe` before physical unmute.
* Changed GPIO handling to request and validate only `POLLPRI`, acknowledge the
  sysfs edge with checked I/O, and reject non-edge wakeups before any I2C read.
* Added a checksum-gated deterministic BlueALSA player build. Two independent
  static ARM builds are byte-identical.
* Added bounded BusyBox syslog rotation, kernel-ring fallback, prompt
  mute-first shutdown, and managed bootstrap process state.
* Replaced rotating-log DSP acceptance with a lifecycle-owned RAM marker.
* Fixed legacy ADB acceptance parsing, WAMP session-reader leaks, and atomic
  media mute toggling found during final review.
* Ran the complete host suite and MCU/DSP race suites successfully.
* Built the v9 runtime and initramfs twice with identical bytes.
* Rebuilt the MCU interface after the mmap-based pinmux correction; host tests,
  vet, and race tests passed.
* Booted the v9 image, verified WAMP readiness, and attended repeated
  clockwise and counterclockwise rotations. The monitor captured multiple
  `volumeup` and `volumedown` events with GPIO3 high and pinmux
  `0x0038D249`.
* Attended one physical Mic-Mute press; the donor-compatible `micmute`
  keypress publication was captured. BlueALSA had no active PCM at that
  moment, so no mute mutation was attempted.
* Attended a physical Bluetooth long press; the `bluetooth-long` publication,
  bounded pairing-window reopen, and `pairing window=300 seconds` log were
  captured. The operator also observed the top white pairing light.

## v9 host candidate

* Runtime manifest SHA-256:
  `beb0acacc27cb3643be1399c077536e5b9726dc59ffa125aca2e6f4663cff7ca`
* Initramfs size: 29,212,479 bytes
* Initramfs SHA-256:
  `202504d1edc1043531d76a36e13148acade56a5fb81c68c6b33f8b079827ff5c`
* MCU interface SHA-256:
  `c9102b23af4ca77d8d27a4e3892e1dfe927c39b7b33adbe6f4de4858f8e79763`
* DSP interface SHA-256:
  `f5b36cc396a07158ead5ad62d9f6da0899dbef6dc1dcb5e319a5e8b713e989b8`
* BlueALSA player SHA-256:
  `4c9978214873589991b995b482b5503fe16b9607e6a8c8896cef251ad3b1d937`

The packaged runtime still needs a fresh build with the rebuilt MCU digest.
Iteration 6 confirmed the shipped `bluealsa-aplay` already carries the
playback-lease patch and matches its gate digest, and archived the pinned
upstream sources that were previously missing.
The next required operations are a clean cold boot, five-boot acceptance,
attended audible output, and microphone correlation.

## Change log

The current worktree contains all five implementation iterations, the
iteration 6 host audit, and RPI tracking artifacts. Implementation work
remains uncommitted because final hardware acceptance is still open. Repository hygiene work (MIT relicensing and
identifier scrubbing) has been committed and pushed separately.
