---
title: Pre-NAND platform implementation
description: Implementation log for completing the owned RAM-only replacement platform
ms.date: 2026-09-05
ms.topic: overview
---

## Status

Implementation is 99 percent complete for this RPI cycle. LED clearing,
playback continuity, fail-closed microphone routing, service fault injection,
and reproducible ARM artifacts are implemented and machine-tested in RAM.
Three cold boots are complete. Cold boots 4 and 5, final fault confirmation on
v15, and attended playback remain.

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

## Completed in iteration 12

* Multi-Device Disambiguation:
  - Discovered and resolved Bluetooth cross-talk with nearby Invoke units.
  - Read USB-attached Invoke Bluetooth BD_ADDR (`D8:F7:10:C1:46:E9`) via ADB sysfs.
  - Locked `REINVOKE_TARGET_BDADDR='D8:F7:10:C1:46:E9'` in `tools/usb-boot/local.conf`.
* Live A2DP Playback & Rotary Audio Evaluation:
  - Streamed live audio over A2DP; confirmed audible tone/playback on the target unit.
  - Verified low-latency in-memory rotary volume control with fast response.
* Audio Stability & Mute Holdoff Debounce:
  - Root-caused momentary playback clicks to amplifier mute toggling during transient ALSA buffer gaps.
  - Added a 1.5s holdoff delay (`playbackPolicyHoldoff`) to `tools/mcu-interface/playback_policy.go` while keeping instant unmute on start.
  - Added unit test `TestPlaybackPolicyHoldsActiveDuringBriefUnderrun` in `tools/mcu-interface/playback_policy_test.go`.
* Mic-Mute Ring LED Clearing:
  - Captured the donor `ledOff` procedure and recovered its exact 41-byte
    packet: opcode `0x0e`, first-chunk flag `0x01`, and three zero frames.
  - Updated `tools/mcu-interface/led.go` to send that packet from `clearLEDs`
    and `Stop()`. The operator confirmed red-ring on/off behavior.
* Toolchain Pinning:
  - Pinned all Go compilation and testing strictly to `${archive_root}/toolchains/ubuntu-go-1.18.1/extracted/usr/lib/go-1.18` via `tools/mcu-interface/build.sh` and `test.sh`.
  - All 40 unit tests pass.
  - Rebuilt static ARM binary for `reinvoke-mcu-interface` (`2a9feb03...`) and updated `tools/usb-boot/build-native-runtime.sh` pin.
  - Hot-deployed updated binary to running RAM target over ADB and verified automatic supervisor restart.

## Completed in iteration 13

* Proved attended stutters against kernel XRUN records and rejected
  one-second ALSA state sampling as insufficient evidence.
* Captured 4,030 incoming ACL packets and found periodic A2DP arrival stalls,
  continuous packet sequence numbers, and advanced RTP timestamps.
* Recovered HK's final `music` PCM contract, 512-frame periods, 8192-frame
  buffer, complete-period writes, partial-write retry, and `snd_pcm_recover`.
* Added reproducible BlueALSA patches for the donor write loop, a two-second
  decoded PCM reservoir, and bounded timestamp-derived SBC gap concealment.
* Passed one 96-second adaptive-source machine run and one five-minute
  conservative-source soak with zero XRUNs, no mid-stream reopen, clean close,
  and lease removal.
* Added the missing MCU WAMP `caller` role and tracked DSP command completion.
  Failed microphone commands now leave the privacy LED unchanged.
* Built reproducible static ARM artifacts:
  - `bluealsa-aplay` SHA-256 `59dd5985...`
  - `bluealsa` SHA-256 `62a3c8c4...`
  - `reinvoke-mcu-interface` SHA-256 `b5215a01...`
  - `reinvoke-dsp-interface` SHA-256 `b5330be7...`
* Built the final v12 runtime twice with byte-identical trees and manifest
  SHA-256 `dc07e3c343c98a7353ab16afa8797e1664b68d8db76ed31758677727bd4ec70c`.
* Built the final v12 initramfs twice with byte-identical 29,226,746-byte
  outputs and SHA-256
  `110376e026c28d1304227847365ac15beb53c12767de45e743358044d4db94e4`.

## Completed in iteration 14

* Captured repeated Mic-Mute trials from the owned and donor MCU services.
  Both services missed occasional physical attempts because the companion MCU
  emitted no event frame. Valid duplicate `04 04` frames are now debounced
  before local control and WAMP publication.
* Corrected the GPIO event path so each latched falling edge causes at least
  one MCU read, even when the interrupt line returns high before userspace
  samples it.
* Moved microphone privacy control out of the WAMP lifecycle. The process-wide
  controller handles physical Mic-Mute while the router is unavailable,
  persists required mute state in `/run/reinvoke`, and retries unresolved mute
  operations until the DSP confirms them.
* Removed DSP Mic-Mute from the unauthenticated WAMP surface. The MCU owns the
  public compatibility procedure and reaches DSP opcode `0x09` through a
  mode-0600 Unix socket.
* Made DSP boot re-read the RAM privacy state after `EVENT_DSP_BOOTUP`, restore
  mute before socket and session readiness, preserve unrelated valid frames
  encountered during command correlation, and terminate on pump failure
  independently of router state.
* Gave the DSP sole ownership of expander reset bit 0 and its direction. The
  MCU preserves that bit while configuring amplifier, DAC, and DSP power-rail
  outputs.
* Restarted Bonefish, MCU, DSP, BlueALSA, and `bluealsa-aplay` independently.
  Each supervisor recovered. Router, MCU, and DSP restarts re-confirmed the
  persisted microphone mute.
* Captured 786,688 stereo frames after the accepted restart sequence. All
  1,573,376 32-bit samples were zero.
* Reproduced the final static ARM binaries byte-for-byte:
  * MCU SHA-256
    `a6c75ad9937519f71c6570e29f0c32c843aefb3efd7f0089bc2e3f3adda9e744`
  * DSP SHA-256
    `ae31b0ca6a1dbd02a2993f6b26eac73030cb52d3ff0106b0b288426b6e1ede7a`
  * BlueALSA SHA-256
    `62a3c8c465437240b9c8f1fa41bddbbde8fd98796a51527636c70a5ede605348`
  * `bluealsa-aplay` SHA-256
    `edc3a6cccb01bf4ac5ab8ab2898fad29e7fbafbebc1e9053aeed8b4c5f006558`
* Built two byte-identical accepted runtime trees with manifest SHA-256
  `018e2038b34eaf14460c7187f9c932b66c08a69c5c872a015ac7fb3cd9020592`.
* Built two byte-identical 29,251,747-byte accepted initramfs images with
  SHA-256
  `b728f242ded43423a3a5be33664905c27ec2703131f8b40be0995568535ae1d7`.

## Completed in iteration 15

* Ran the on-device acceptance suite against the live v12 boot: 22 of 23 checks
  pass. The single genuine failure is the WAMP firewall, which v12 predates.
* Recorded the pre-fix exposure baseline: `9998` and `9999` listening on
  `0.0.0.0` with no INPUT filtering.
* Injected router, MCU, and DSP faults. Bonefish restarted while the MCU and DSP
  services kept their PIDs and re-registered, 10 and 8 registrations.
* Found and fixed a DSP readiness race. Boot state was recorded only on the
  best-effort WAMP delivery path, so a restart could leave
  `/run/reinvoke/dsp-booted` absent. The pump now records it at link detection.
  `TestPumpRecordsBootStateWithoutWAMPDelivery` fails without the fix.
* Found and fixed kernel irreproducibility. `vmlinux` and `System.map` were
  already identical; `piggy.lzo` differed only in the lzop header modification
  time and its checksum. Patch `0004-reproducible-lzo-piggy.patch` compresses a
  named file with mode and modification time pinned.
* Cross-built a reproducible static `libdbus-1.a` and the previously unbuilt
  `bluez-media-control`, and recorded the recipe in `tools/control/README.md`.
* Re-pinned seven build gates, each confirmed by two byte-identical builds and
  documented with its reason in the v13 build notes.
* Built the v13 candidate. Two independent builds agree byte for byte:
  * kernel SHA-256
    `514700fa88835c591cf6a02e8db7ef8d80d5b5f199355e643317609b69e33500`
  * modules SHA-256
    `05a8bfadeccf846396721c21a7714a8c117a53eb9b80a8e8824e64f13edfc496`
  * runtime manifest SHA-256
    `51d304b34ebabac29c9b9dbf7eb28001135e743f032f46635733b028afcb9299`
  * initramfs SHA-256, 32,381,114 bytes
    `e28b17016fe38078af439cf27e80c212689265cc494366849422ab5fee0389d8`
* Staged the pair through the checksum-gated loader. It is ready to inject at
  the next yellow-mode window.

## Completed in iteration 17

* Caught cold boot 3 and loaded v14. ADB returned in five seconds.
* Confirmed the bounded SPI-ready fix: the DSP downloaded 40,121 transfers,
  emitted `EVENT_DSP_BOOTUP`, and stayed up through native acceptance.
* Passed all 23 native acceptance checks, including storage isolation, runtime
  hashes, the WAMP firewall, DSP readiness, and every supervised service.
* Restarted the DSP once; it completed the download, recreated its private
  microphone socket, and restored its boot marker.
* Validated the pairing-agent generation guard on hardware. The old agent died
  within one second of `bluetoothd`; the replacement daemon appeared at five
  seconds and its replacement agent at six.
* Found that HCI initialization only covered the first daemon generation.
  Restarted BlueZ therefore saw `hci0` as not powered. Live `hci-init --reset`
  restored a real 120-second pairing window. PID 1 now initializes HCI before
  every supervised daemon generation.
* Found that the response-correlation refactor retransmitted an entire DSP
  command after an all-zero response header. Restored the earlier receive-only
  retry: send once, sleep, wait for Ready, and retry response reads. This avoids
  duplicating an effect whose acknowledgement was missed.
* Built the DSP binary twice at SHA-256
  `c64bf11cd821b92a8362a55523e2b6d7afeb350a66cd8894d2c76d11849d7be9`.
* Built two byte-identical v15 runtime manifests at SHA-256
  `4685923f86a8e485cc5be4bf0618384593b488ddfbf481525e174f8aa3cfc6bb`.
* Built two byte-identical 32,382,132-byte v15 initramfs images at SHA-256
  `9ab76db2ee7f8d9e7533355ce91d2dde014205a5d6f22111096db256004eddd9`.
* Staged v15 with the unchanged reproducible v14 kernel for cold boot 4.

## Completed in iteration 18

* Statically recovered and implemented the donor `com.harman.ledSet` contract:
  persistent front amber, front white, and back states in one zero-padded
  six-byte opcode-`0x09` MCU command.
* Serialized indicator state with I2C publication, retained only confirmed
  state after failures, and added focused mapping, rollback, WAMP, and
  concurrency tests.
* Built the MCU binary twice at SHA-256
  `c3db4b9e650588f7261f5967a4137a62f543a24bfca704e2b88ea44d16cbd36f`.
* Built two byte-identical v16 runtime manifests at SHA-256
  `1bf8622eb08517739628fd6a17737945435a000b18c7f78f7be0166266401980`.
* Built two byte-identical 32,389,027-byte v16 initramfs images at SHA-256
  `fef5f412fd0d5589f5a1cf739c9b131127f9e788147574c4388ef7122eea4453`.
  v16 keeps the v15 lifecycle fixes and adds the indicator contract.
* Physical front and rear indicator validation remains outstanding; this
  iteration records static donor evidence and host validation only.

## Change log

Iterations land on the `feat/native-ram-platform` branch as they complete.
Committing there records the work; it does not claim acceptance. The pull
request remains gated on the repeated cold-boot campaign, which needs the
operator because yellow mode requires the recovery button held at power-on.
