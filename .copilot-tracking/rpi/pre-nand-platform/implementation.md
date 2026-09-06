---
title: Pre-NAND platform implementation
description: Implementation log for completing the owned RAM-only replacement platform
ms.date: 2026-09-05
ms.topic: overview
---

## Status

Implementation is 99 percent complete for this RPI cycle. LED clearing,
playback continuity, microphone privacy, service fault injection, and
reproducible ARM artifacts are implemented and machine-tested in RAM. The DSP
command blocker was the omitted GPIO5 pinmux lifecycle, not timing or command
order. Final RC cold-boot, Bluetooth state, and attended playback acceptance
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
* A controlled donor-client test also failed its checksum after a reset cycle.
  Later operator clarification established that every yellow-mode cycle removed
  power, so warm state was not proved by this result.
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
* Physical validation later confirmed front amber/white mutual exclusion,
  front slow/fast blink and off, and rear on, slow/fast blink, and off. Repeated
  isolated ten-second front-white and rear `dim` tests produced no visible
  output even in a dark room.

## Completed in iteration 19

* Booted v16 and passed all 23 native acceptance checks.
* Proved the preserved accepted-v12 DSP binary also fails response validation
  on the same hardware state. Operator clarification later established that
  every yellow-mode entry included power removal, so this rules out both the
  v15 retry change and a merely warm reset.
* Decoupled MCU WAMP lifetime from failed DSP mute reconciliation. Failure is
  logged and scheduled on the existing background retry without withdrawing
  unrelated MCU procedures.
* Live-tested the fix in RAM while the DSP was degraded: MCU status and every
  recovered indicator channel/mode remained callable, and all channels were
  cleared afterward.
* Built the MCU binary twice at SHA-256
  `9b38f0f3fdc2e7dba47d279909e8f2f2d185fb965417229ffa59df11fbfd83e7`.
* Built two byte-identical v17 runtime manifests at SHA-256
  `b3d0a36234693ed289af82dca00e926756ddaf1e57ef9c7b595a0ad9a0fae1f4`.
* Built two byte-identical 32,390,080-byte v17 initramfs images at SHA-256
  `2d0f17105343b9f8d5cd3d0cb8a530608c8c62ad73b5b3380c365eda7fc21cd4`.

## Completed in iteration 20

* Captured a clean successful donor `getVer` transaction on the current
  hardware and kernel after removing `/etc/profile` output contamination:
  `EVENT_DSP_VERSION=0.0.64.58`.
* Traced the owned client under the same conditions. Diagnostic tracing
  stretched nominal 10 ms waits to 18-19 ms; `getVer` and Mic-Mute then both
  succeeded.
* Provisionally attributed the result to a 20 ms timing envelope. Packaged and
  repeated detached tests later disproved that conclusion.
* Added a conservative cancellable one-second post-boot settle before restoring
  persisted microphone mute. It remains a readiness barrier, not the DSP
  command root fix.
* Repeated the attended DMA test at stereo 48 kHz `S32_LE`, 256-frame periods,
  and 16 periods:
  * unmuted: 244,163 of 244,224 samples nonzero, RMS 88,026,326;
  * muted: 0 of 244,736 samples nonzero, RMS and peak exactly zero.
* Restarted the combined timing candidate with muted state. Its first capture
  was again 244,736 zero samples and byte-identical to the attended muted WAV.
* Added a readiness barrier so WAMP commands cannot enter during the settle and
  be acknowledged before DSP initialization overwrites them.
* Built the final DSP candidate twice at SHA-256
  `95c223f94594ab8658043e491434b6d206da5dfbc5be7052fd82676b8173b548`.
* Built two byte-identical v19 runtime manifests at SHA-256
  `47343f69a1398e0dcd87abb97d716731001747e52719c9d033e4ab0e8e7959f5`.
* Built two byte-identical 32,389,139-byte v19 initramfs images at SHA-256
  `ca9d5ce4b3cd11881a97a72c02d17a35981f6e7cfdc76f2dbf72e3537070a72d`.

## Completed in iteration 21

* Rejected the apparent 20 ms timing conclusion after the packaged v19 process
  still failed while an attached process passed.
* Proved the donor has the same launch-context behavior: clean donor `getVer`
  succeeds attached and fails under the detached launcher.
* Ran detached timing sweeps:
  * handshake/release 60, 80, and 100 ms still failed;
  * 120 ms could miss `EVENT_DSP_BOOTUP`;
  * response-turnaround 1, 5, 10, and 20 ms still failed; and
  * sleeping 50, 100, or 200 ms after the pump claimed a command still failed.
* Provisionally attributed isolated passes to one idle pump-cycle deferral.
  Ten identical detached trials later produced zero command passes, disproving
  that conclusion. The deferral was removed.
* Corrected the capture privacy claim. A raw ALSA `hw_params` after startup can
  overwrite an earlier DSP mute route. Reasserting mute after configuration
  produced 244,736 zero samples. A future capture owner must not consume raw
  samples while muted and must wait for post-configuration mute confirmation.
* Built the final DSP binary twice at SHA-256
  `667beeee278ee3692855e60a039de89d8d1e9b168f16e3609e55952a7ab44901`.
* Built two byte-identical v21 runtime manifests at SHA-256
  `aca3a532ea971482d88442669637e99dea3097a43e8fdf49cafbb572dd79c9de`.
* Built two byte-identical 32,388,952-byte v21 initramfs images at SHA-256
  `9b88112e5425c4095098d492d3e3b4bbc4319804d8ee786e079616d38c167c94`.
  These are experiment artifacts, not an accepted candidate.

## Completed in iteration 22

* Implemented donor-compatible Bluetooth short-press pairing toggle/cancel
  while retaining the validated long-press reopen fallback.
* Added exact-device BlueZ connection tracking and atomic mode-0600 publication
  of authoritative `pairing`, `connected`, or `off` runtime state.
* Added a bounded MCU watcher that drives rear slow-blink/on/off through the
  serialized `ledSet` command controller, deduplicates confirmed writes, retries
  failures, and chooses safe off if the producer disappears.
* Built the final pairing agent and MCU interface twice each with byte-identical
  outputs. Their SHA-256 digests are
  `7e9cb4c8d4047d3151e9ac2ed5ee34759af3a19a1dd2e014c3c3f134f33a20bd`
  and
  `53ffe4c017dc5f1ef08c5a422d70aeb8b0a11e3002263d9ecef466fbdf45afee`.
* Built two byte-identical v23 runtime manifests at SHA-256
  `1789b3af2714f3f3402b32b9ef9bb4bfe78602de6d9f1c7fb3cb053b342577f3`.
* Built two byte-identical 32,393,417-byte v23 initramfs images at SHA-256
  `77a3d9ee37d6f16e6fad2b3f016d7290da85e83606cf8005505eff3eafefd45e`.
  Physical short/cancel and rear-state validation remain.

## Completed in root-cause iteration

* Re-ran identical detached trials and rejected the one-off timing and scheduling
  passes rather than producing another candidate.
* Read the live SoC pinmux register after owned boot:
  `0xF7EA8008=0x0038D249`.
* Recovered the donor's required lifecycle: select GPIO5 manual chip-select mode
  before image download and restore its SPI message function afterward.
* Set only GPIO5's `0x01000000` bit. The resulting `0x0138D249` preserves the
  MCU GPIO3 bit `0x00200000` and made the unchanged detached DSP service return
  `EVENT_DSP_VERSION=0.0.64.58` immediately.
* Implemented a shared interprocess pinmux lock across MCU and DSP. DSP now
  clears only GPIO5's bit before every download, sets it afterward, and verifies
  readback. Message mode is restored even after download failure.
* Hardware-tested two consecutive DSP generations from message mode. Each
  downloaded 40,121 transfers, restored `0x0138D249`, received the boot event,
  and completed `getVer`. Persisted mute restoration and subsequent `getVer`
  also passed.
* Removed the disproved idle-cycle deferral and retained donor-compatible 10 ms
  waits plus the conservative readiness barrier.
* Classified the 23-check target script as structural smoke. The full host
  collector now requires `getVer`, version `25688`, Mic-Mute confirmation,
  initial-state restoration, and post-probe logs.
* Added a singleton loader lock so only one process may stage, wait, and inject.
* Built the first post-retrospective release candidate twice:
  * MCU SHA-256
    `2911c0cc0d8c13f069e65914fff58cf15761b4581b7bc759918b07e7637f1274`;
  * DSP SHA-256
    `32d0bd0b027ff0396c29d6eb523fc5b128051db75af2f70989a966f2e58f8f48`;
  * runtime manifest SHA-256
    `30f63fda4b6a2ee0f6472b9e6fb7ff80d3fb46695acb63ec5bd8bc6739d892ff`;
  * 32,440,707-byte initramfs SHA-256
    `11fed5e1e625dffc242f8e5f2b2dbb6294ec2fa93830d8d129b07e284078a831`.

## `pre-nand-rc1` cold-boot results

The candidate was cold-booted from power-off and validated without any manual
register writes or attached debugger.

* The full `collect-native-acceptance.sh` gate passed twice with zero failures.
  Both runs reported MCU status `000116`, the `com.harman.dsp.version` event
  `25688`, a confirmed Mic-Mute, and restoration of the initial state.
* PID 1 restored `0x0138D249` on every DSP generation. Killing the DSP service
  produced a replacement that restored the pin function, reapplied the retained
  mute before announcing readiness, and round-tripped `getVer` and both
  microphone directions. The second gate run executed against that generation.
* The MCU's `0x00200000` bit survived three DSP generations, so the shared lock
  and the single-bit read-modify-write hold on hardware.
* Killing `bluetoothd` produced a replacement that re-registered both A2DP
  endpoints. The generation guard restarted the pairing agent, which opened its
  bounded startup window and returned to `off` on expiry.

Two host-side defects were found and fixed during the run.

* The `adb` fork-server inherited the loader's singleton lock descriptor and
  held it for the life of the host session, so every later loader run failed
  with a false concurrent-loader error. Children are now spawned with that
  descriptor closed, and a held lock with no live loader is reported as the
  stale-descriptor case.
* The loader gave no machine-readable progress, so an operator could not tell a
  caught yellow-mode window from a missed one. It now writes one timestamped
  state line and refreshes an armed heartbeat every fifteen seconds.

Gates that remain need the operator or a second host: physical button and
indicator validation, attended playback, and the live-network drop rule. Button
presses arrive as MCU publications and cannot be injected over WAMP, and this
kernel exposes only a mount namespace, so neither can be simulated on-box.

The image ships no microphone capture consumer, which was reconfirmed on the
running candidate. Re-measuring capture privacy would require adding one and
opening raw PCM while muted, which the capture-owner contract forbids, so the
existing attended measurement stands rather than being weakened.

## `pre-nand-rc2` candidate

A functional review of the `pre-nand-rc1` delta found that the DSP service
reused the process context for its message-mode pinmux restore. Because that
context is cancelled by `SIGTERM`, `exec.CommandContext` would refuse to run the
restore during shutdown, leaving GPIO5 in download mode for the rest of the boot
with no supervisor restart to repair it. The cleanup was disabled by exactly the
condition that triggers it. The restore now runs on a bounded detached context,
and a failed download-mode selection also attempts a restore before exiting.

That change alters the DSP binary, so the candidate was rebuilt rather than
relabelled. `pre-nand-rc1` remains the only build with a passing hardware gate.

* MCU SHA-256 unchanged at
  `2911c0cc0d8c13f069e65914fff58cf15761b4581b7bc759918b07e7637f1274`;
* DSP SHA-256
  `4a7882d4f463b6a6adf84f38e868faf1b2e17a0a0303d0c6c2246ecf07ef4c95`;
* runtime manifest SHA-256
  `2d9e8ffcb10e63849f8cae6f45317d83c21ce629d7664f420004b58ee25aedfe`;
* 32,442,827-byte initramfs SHA-256
  `60da1c7427f65fb9cb384197a3fcbb2f08225907a49fecbbb350ac63fd81fc35`;
* kernel unchanged at
  `d29a007535794d74d8ed900da366f02631a9a981356caea707e6b163f6d07746`.

The runtime bundle and the initramfs were each built twice and agree byte for
byte. `pre-nand-rc2` is unproven on hardware until it clears the same gate
`pre-nand-rc1` cleared, which needs an operator-driven cold boot.

## `pre-nand-rc2` cold-boot results

The candidate was cold-booted from power-off and cleared the full
`collect-native-acceptance.sh` gate twice with zero failures, once immediately
after boot and once after the fault injection below. Both runs reported MCU
status `000116`, the `com.harman.dsp.version` event `25688`, a confirmed
Mic-Mute, and restoration of the initial state. The running DSP binary hashed to
`4a7882d4f463b6a6adf84f38e868faf1b2e17a0a0303d0c6c2246ecf07ef4c95`, and GPIO5
read `0x0138D249` on the first generation with no manual write.

The shutdown defect the candidate exists for was reproduced and shown fixed on
hardware rather than argued from the source. The DSP service was interrupted
with `SIGTERM` while its image download was in flight, which is the window that
previously stranded the register:

* the register read `0x0038D249` mid-download, confirming GPIO5 was cleared and
  the process was genuinely inside the vulnerable window;
* the service logged `boot DSP: context canceled`, confirming the signal
  cancelled the boot;
* it also logged `restored DSP message pinmux 0x0138D249`, so the restore ran
  despite that cancellation; and
* the register read `0x0138D249` afterwards.

`pre-nand-rc1` would have left `0x0038D249` in place for the rest of the boot.
The supervisor then replaced the interrupted service, which completed its
download, restored the pin function, booted the DSP, and round-tripped `getVer`
and both microphone directions.

Killing `bluetoothd` produced a replacement that re-registered both A2DP
endpoints. The guard restarted the pairing agent, which reopened its bounded
window and returned to `off` on expiry. After all of that churn the image
reported no zombies, no kernel oops, segfault, or BUG lines, both WAMP ports
listening, and every supervised service present.

This kernel sets `pid_max` to 4096, and the PID counter was observed wrapping
within 132 seconds of ordinary supervision churn. PID reuse is therefore a
routine event on this target rather than a remote possibility, and any logic
that identifies a process by PID has to prove identity another way. The MCU
pairing control already does: it resolves `/proc/<pid>/exe` and refuses to
signal when the executable does not match, which
`TestPairingControllerRejectsWrongExecutable` covers. The pairing agent also
exits on its own when `org.bluez` disappears, so agent recovery does not depend
on the guard's PID comparison alone.

### Generation and lock-contention stress

Eight further DSP generations were driven on `pre-nand-rc2`. Every one restored
`0x0138D249`, preserved the MCU's `0x00200000` bit, and round-tripped `getVer`,
for eight passes and no failures.

`configureMCUInterruptPin` runs only at MCU startup, so the shared pinmux lock is
contended only when the MCU starts while the DSP is between its download-mode
clear and its message-mode restore. That contention had never been exercised on
hardware, only in unit tests. Killing both services together in three rounds
produced replacement processes with adjacent PIDs, which places both
read-modify-write sequences in the same window. The register read `0x0138D249`
after every round, and both services then answered `getmcustatus`, `getVer`, and
a Mic-Mute round trip. Fifteen pinmux restores are recorded in the runtime log
across the whole session.

An earlier attempt at that contention test was invalid and is recorded here
because the failure mode is the one this effort keeps repeating. Its device-side
command substitution never executed, so nothing was killed; the process IDs were
identical before and after all five rounds while the register kept its correct
value. It would have read as five clean passes. A restart test is only evidence
when the process ID is shown to change, and the corrected run above asserts
exactly that.

### Router restart recovery

The WAMP router had never been fault-tested, and it is the service every other
component depends on. Killing it produced a genuinely new process, confirmed by
the listening socket changing owner and by the pid file advancing, while the MCU
and DSP services kept their original process IDs throughout.

Both clients logged `WAMP session ended: EOF; reconnecting in 5s`, retried once
against a router that was not listening yet and logged `connection refused`,
then re-established. The DSP logged `registered 7 procedures` ten seconds after
the kill, and both services answered `getmcustatus`, `getVer`, and a Mic-Mute
round trip afterwards.

The property that matters is what did not happen. Because neither client
restarted, the DSP image was not downloaded again, so a router fault does not
disturb DSP hardware state. The pin function stayed at `0x0138D249`, the boot
marker stayed present, and the retained privacy state was untouched. Recovery
from a router fault costs a reconnect, not a DSP reboot.

`runWithReconnect` in `tools/dsp-interface/lifecycle.go` is the mechanism, and
the supervisor restarts the router itself.

### Physical controls are dead on this boot

An operator session pressed Bluetooth short, Bluetooth short again, and
Bluetooth long, then Mic-Mute twice. The capture recorded zero button
publications and zero Bluetooth state transitions, and the operator reported no
indicator light of any kind for the whole boot apart from the yellow U-Boot
light. Both symptoms have one grounded cause.

`/sys/class/gpio/gpio3` is configured `direction=in edge=falling` and its value
has been stuck at `0` for the entire boot. The MCU asserts that line and is
expected to release it once the host reads the queued event. The service logged
`MCU interrupt remained low after 1024 pending reads` at 36 seconds of uptime
and three more times at 12 and 13 minutes, which correspond to the MCU service
restarts driven during fault injection. Each fresh process drains up to 1024
events, never sees the line released, and gives up.

The first failure at 36 seconds predates every fault injection in this session,
so the testing did not cause it. The boot handshake itself succeeded: the
protocol marker exists and the service would have exited on failure.

Two software defects follow from this, independent of why the MCU holds the
line.

* Once `drainPendingEvents` returns `errMCUDrainLimit`, `recoverySuppressed` is
  set and the poll-timeout branch stops draining. It clears only when the line
  reads high, which a wedged MCU never does. Physical input is therefore
  permanently dead for the rest of the boot with no retry and no escalation. A
  service that cannot see its only input source should not fail silently.
* `startupDiscardPending` stays true until a drain completes without error. With
  the drain always failing, every decoded event would be discarded even if one
  arrived.

Indicator writes are consistent with a wedged MCU rather than a broken LED path.
`com.harman.ledSet` was called directly for front white on, back on, and front
amber fast-blink. All three returned results, so the I2C write was accepted, yet
nothing lit. The controller records success from `WriteMCUCommand` alone and
never confirms the device acted.

One hypothesis was tested and rejected rather than assumed. The donor writes
`0x0118D249` for message mode, which leaves the MCU's GPIO3 bit clear, while
this platform runs `0x0138D249` with it set. Clearing that bit live and sampling
the line for eight seconds left it at `0` throughout, and the register was
restored. The GPIO3 pin function is not what holds the interrupt low.

### `getmcustatus` is not a health check

`com.harman.vui.getmcustatus` returns the compile-time constant
`recoveredMCUVersion`, which is `"000116"`. It performs no bus traffic. It was
used repeatedly in this session as evidence that the MCU survived a fault
injection, and it cannot support that claim. Those checks show only that the MCU
service process is running and answering WAMP. Every earlier statement in this
session that the MCU "still works" after a restart should be read with that
limit in mind, and a genuine MCU health probe is still missing.

### Next experiment: boot the known-good v16 image

Before building another candidate, the more useful question is whether this is a
regression in our own software or a change in the hardware's behaviour. Physical
indicator validation was recorded against the v16 image, and that artifact is
preserved and still hashes to
`fef5f412fd0d5589f5a1cf739c9b131127f9e788147574c4388ef7122eea4453`. The kernel it
was built against, `d29a0075`, is the kernel in use now, so it can be booted
unchanged.

The discriminator does not need an operator to press anything. If GPIO3 leaves
its stuck-low state on v16, the fault arrived with a later change and can be
bisected against a known-good anchor. If GPIO3 is stuck low on v16 as well, then
every candidate behaves identically and the cause lies in the MCU or the board
rather than in the platform software.

A diagnostic build was written and tested but deliberately not released. It
reports a bounded set of distinct undecodable frames when a drain hits its
limit, which discriminates between the MCU repeating one status frame and a real
queue being mis-decoded. That question only becomes worth a boot once the v16
comparison has narrowed the search, so releasing it first would have spent a
reset on the less informative experiment.

Two alternative explanations were tested and rejected rather than carried
forward. The GPIO3 pin function is not responsible, because clearing that bit
live left the line low for eight seconds before the register was restored. The
service is also not talking to the wrong I2C bus or address: the boot handshake
requires frames whose leading byte matches opcodes `0x01` and `0x23`, and it
completed, which a wrong device would not satisfy.

### D-Bus restart recovery


Killing the session bus cycled the whole BlueZ stack rather than stranding it.
The bus, `bluetoothd`, `bluealsa`, and the pairing agent all came back with new
process IDs, `bluetoothd` re-registered both A2DP endpoints, the playback
process stayed supervised, and the pairing agent reopened its bounded window.
The MCU and DSP services were unaffected, since neither uses the session bus,
and both still answered `getmcustatus` and `getVer` afterwards.

Two checks in that pass needed correcting before they meant anything. A probe
for a powered adapter reported zero because `hciconfig` is absent from this
image, not because the adapter was down; sysfs shows `hci0` present with its
address and rfkill unblocked. `bluetoothd` also logs `Loading LTKs timed out`
and `Load Connection Parameters failed`, which look alarming next to a fault
injection. The rotated log carries the identical messages from the first
`bluetoothd` at nine seconds of uptime, so they belong to this controller and
BlueZ pairing rather than to the restart.

Supervised recovery has now been exercised for the DSP service, the MCU service,
`bluetoothd`, the WAMP router, and the session bus. Every one produced a new
process and a working service.

### Log growth is bounded

The runtime log is written by `syslogd -s 256 -b 1`. Rotation was exercised
rather than assumed: the log was driven past the threshold, `runtime.log.0`
appeared holding 262,150 bytes, and the active log restarted near 37 KB. Total
log residency stays near 300 KB, which matters because this platform is RAM-only
with no swap. Memory held at roughly 160 MB free and per-service descriptor
counts stayed between four and thirteen after about twenty service restarts, so
the churn above leaked neither memory nor descriptors.

### Candidate discipline
No further candidate is warranted. The only commit after the `pre-nand-rc2` pin
is documentation, and no image-affecting path differs, so rebuilding would
produce the same artifact under a new name. The provenance chain is closed:
`build.sh` at `HEAD` reproduces
`4a7882d4f463b6a6adf84f38e868faf1b2e17a0a0303d0c6c2246ecf07ef4c95`, that binary
is the one inside the `pre-nand-rc2` runtime bundle, and it is the binary the
gated device is running.

Reproducing an owned service binary requires the checked-in `build.sh` for that
service rather than hand-assembled flags. It pins `-trimpath`, `-buildvcs=false`,
`-mod=readonly`, `-ldflags="-s -w"`, and the archived Go 1.18.1 toolchain.
Omitting `-buildvcs=false` stamps the commit and a dirty-tree flag into the
binary, and omitting `-trimpath` embeds absolute build paths, so either one
silently breaks reproduction. `pre-nand-rc1` was re-derived byte for byte from
its commit this way, which confirms the artifact's provenance.

## Change log

Iterations land on the `feat/native-ram-platform` branch as they complete.
Committing there records the work; it does not claim acceptance. The pull
request remains gated on the repeated cold-boot campaign, which needs the
operator because yellow mode requires the recovery button held at power-on.
