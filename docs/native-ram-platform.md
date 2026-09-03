---
title: Native RAM platform and component audit
description: Verified native Linux bring-up, installed-version evidence, and board-support boundaries for a replacement Invoke stack
ms.date: 2026-09-03
ms.topic: concept
---

The Harman Kardon Invoke can run a custom-owned Linux lifecycle from DRAM. The
verified prototype uses the preserved recovery kernel, a sanitized initramfs,
and selected hardware firmware from the unit's installed rootfs. It does not
start Harman's supervisor, Cortana, OTA updater, or normal application stack.

The result is a development platform for a replacement stack, not a modified
Cortana image.

## Verified outcome

The physical unit completed this sequence:

1. Yellow service mode loaded U-Boot into DRAM.
2. U-Boot loaded the reviewed `81_IMAGE` kernel at `0x0c400000`.
3. U-Boot loaded a sanitized initramfs at `0x08000000`.
4. The kernel executed the replacement `/init` as PID 1.
5. The USB gadget enumerated as `18d1:0d02`.
6. Root ADB connected as `0123456789ABCDEF`.
7. No NAND filesystem was mounted.
8. A complete 256 MiB logical NAND data image streamed through a fresh
   read-only MTD node.
9. The SD8887 Wi-Fi function loaded with the unit's firmware and calibration.
10. `mlan0` completed a scan without joining a network.
11. A RAM copy of the installed SquashFS mounted read-only.
12. Bonefish and `mcu-interface` ran under the replacement PID 1.
13. `mcu-interface` completed its mute-first hardware sequence and registered
    its WAMP procedures.
14. The installed SD8887 Bluetooth module loaded after a metadata-only
    `vermagic` adjustment, downloaded firmware, and created `hci0`.
15. The installed Bluedroid service joined the custom-owned WAMP bus using
    RAM-only pairing state and registered its control procedures.
16. A GCC 4.9 replacement kernel registered the first SoC GPIO bank at base 0
    while retaining USB, SPI, and Wi-Fi.
17. `mcu-interface` completed live MCU application-version and recovery-flag
    queries with GPIO 3 configured as a falling-edge input.
18. `dsp-client` received `EVENT_DSP_BOOTUP`, requested the normal DAC and
    amplifier unmute sequence, and published DSP version value `25688`.

## Persistence model

| Component | Analogy | Persistence | Current policy |
|-----------|---------|-------------|----------------|
| DRAM | Whiteboard | Lost on power removal | Primary development and validation target |
| NAND | Internal SSD | Persistent | Read through `/dev/reinvoke-nand-ro`; no writes |
| SPI NOR | Early-boot flash | Persistent | Sampled only; image before any future write |
| SD8887 radio firmware | Device firmware loaded by Linux | Volatile in the radio; source file is persistent | Load from the RAM initramfs |
| MCU firmware | Firmware in a companion controller | Presumed persistent in the MCU | Use the existing protocol; do not invoke upgrade RPCs |
| DSP loader image | DSP program loaded by the host | Volatile in the DSP; the host sends all 160,484 bytes over SPI at every service start | Ship the file with the replacement; a byte-exact capture confirms the transfer is verbatim |

The 16 MiB M25P128 SPI NOR supports erase and program operations in principle.
U-Boot maps it at `0xF0000000`, reports an invalid stored environment, and
returned zeroes at the sampled offsets. Those observations do not prove that
the full chip is unused.

## Installed version audit

The complete logical NAND data image is:

```text
size:   268435456 bytes
sha256: edf38ef2af48d249c9925ebb6a94c716cfdb2c1ce575fb704283918cdd0e53be
```

This is an ECC-processed data-area image. It does not include NAND OOB bytes and
is not a substitute for a raw programmer image. The kernel reported
uncorrectable ECC conditions near the end of the device while the complete data
length still streamed successfully.

The active rootfs identity comes from the extracted SquashFS at `0x02920000`,
not from unscoped strings elsewhere in NAND:

| Field | Value |
|-------|-------|
| Product | `barracuda` |
| Release | `Barracuda_libre-12.2050.3` |
| Git commit | `6c36464edbac87c01fcba0f81c86293f554acf50` |
| Build timestamp | `20210204092918` |
| SquashFS creation | 2021-02-04 05:08:43 |
| SquashFS size | 48,831,891 bytes |

Strings for `Barracuda_libre-2.1727.0`, commit
`0c29b0f24b4f687957a40e8857fb644810421e18`, DSP `378`, and MCU `000111`
remain later in NAND. They are retained data or logs and do not identify the
active SquashFS.

A second SquashFS begins at `0x01a20000`. It is a 2,712,641-byte configuration
image created in 2017. Its `Software_Version` metadata reports NAND image
generation `20170622:0721`.

## Boot and kernel audit

| Component | Identity | Evidence |
|-----------|----------|----------|
| USB-loaded U-Boot | 2013.04, built 2016-04-11 | Live `version` output |
| RAM development kernel | Linux `3.8.13-mrvl`, built 2014-09-11 | Live `uname` output and reviewed `81_IMAGE` |
| Reviewed `81_IMAGE` | SHA-256 `dda4f295e037786c5302b91976e6b37d99bdaa108e76bb94d1337181f64c4763` | Host hash and U-Boot header |
| Reviewed recovery initramfs | SHA-256 `08a8f96a5c476a08ba19441d83637e606f27f442d56c2689dd6b56d2fc72b7a8` | Host hash |
| Installed normal kernel partition | 8 MiB at `0x00a20000`, SHA-256 `68bc06928caec07893b2ba698ec0b4546191787add22ce7a245b7392cdbff0eb` | NAND carve |

The installed normal-kernel carve has a structured header followed by
high-entropy data. It has no U-Boot `uImage` magic or readable Linux banner.
This is consistent with a signed or encrypted Marvell container, but the
cryptographic format is not yet proven.

The RAM kernel does not implement `kexec`. Its `sys_kexec_load` symbol is a weak
alias of `sys_ni_syscall`. A custom kernel must therefore be loaded directly by
U-Boot or through another verified boot-stage mechanism.

### Secure boot impact

Manufacturer signing is expected to block an unauthorized replacement of the
normal kernel through the stock persistent boot path. The private signing key
is not expected to exist on the device. OTP output exposes key-status and CRC
fields, not a reusable private key.

This does not block the current development path. The RAM-loaded U-Boot accepts
the reviewed standard `uImage` kernel and our modified initramfs. Signing
becomes a hard blocker only if the final design requires the stock boot chain
to accept a replacement persistent kernel. A persistent custom userspace may
remain possible with a signed donor kernel, but its rootfs verification and
slot-selection behavior are not yet established.

### Replacement kernel build

The acquired Invoke GPL archive contains Linux 3.8.13 source, the
`berlin2cdp-a0-acast` device tree, and
`berlin2cdp_a0_amp_acast_defconfig`. Unlike the recovery/dongle kernel, this
configuration enables:

* Berlin ALSA
* DesignWare SPI and `spidev`
* SD8887 Wi-Fi and Bluetooth modules
* Berlin NAND and randomizer support
* I2C and GPIO
* An appended device tree suitable for the existing U-Boot `bootm` path

The first reproducible build produced Linux `3.8.13-reinvoke`, a
3,816,179-byte `81_IMAGE.reinvoke`, and five modules. The image SHA-256 is
`f2fdec3a09e3c8c90045c2d15281bd0d9b8b4c26a98404554bd4a730234ab8e1`.
The U-Boot header specifies load and entry address `0x01108000`.

The build also enables the Marvell UDC and Android composite gadget so loss of
USB diagnostics is not an accepted default. See
[Invoke kernel build](../tools/kernel/README.md) for provenance and the build
procedure.

### Rebuilt-kernel boot experiments

The known-good recovery kernel returned its `18d1:0d02` USB gadget 2.58 seconds
after the Marvell endpoint disconnected. Replacement attempts use that measured
time as the success baseline.

| Experiment | Compiler | Load address and DTB | Result |
|------------|----------|----------------------|--------|
| ACast reference | GCC 11.4 | `0x01108000`, ACast DTB | No USB after 360 seconds |
| Known layout | GCC 11.4 | `0x02008000`, exact recovery DTB | No USB after 367 seconds |
| Known layout | GCC 9.5 | `0x02008000`, exact recovery DTB | No USB after 218 seconds |
| Pristine source | Android NDK GCC 4.9 | `0x02008000`, exact recovery DTB | USB returned after 7 seconds |
| SPI-only | Android NDK GCC 4.9 | Proven layout plus SPI node | USB returned after 6 seconds; `spidev0.0` created |
| SPI plus GPIO | Android NDK GCC 4.9 | Proven SPI DTB plus first-bank `base-gpio = <0>` | USB returned after 6 seconds; MCU and DSP completed bidirectional startup |
| Audio | Android NDK GCC 4.9 | Proven SPI/GPIO DTB plus Berlin ASoC nodes | USB returned after 5 seconds; ALSA card 1 and audible output verified |

The successful kernel is `3.8.13-reinvoke-gcc49`. It was built from the
preserved Harman source without the ARM `uaccess` compatibility backport.
Modern GCC versions can compile the tree after compatibility changes, but their
images did not boot on this hardware. The current working policy is therefore:

* Use verified NDK GCC 4.9 for device kernels and modules.
* Keep GCC 11 compatibility work as a host-build research branch, not the
  hardware baseline.
* Hold load address and device tree constant while adding peripherals one at a
  time.

Android's GCC defaults also emitted position-independent references in modules.
The first Wi-Fi load failed on `_GLOBAL_OFFSET_TABLE_`. Rebuilding modules with
`-fno-pic -fno-pie` removed that dependency; `88mlan.ko` and `sd8801.ko` then
loaded, downloaded firmware and calibration, created `mlan0`, and completed a
24-access-point scan.

The SPI-only kernel added DesignWare SPI at `0xF7E81C00` and a conservative
1 MHz `spidev0.0` child. It booted in six seconds and exposed both `spi0` and
`/dev/spidev0.0`. It established the transport and exposed the missing
control-line dependency:

* Bonefish and `mcu-interface` completed their established startup.
* `dsp-client` opened `spidev0.0`, remained alive, joined WAMP, and registered
  its DSP procedures.
* A `com.harman.dsp.getVer` call reached `dsp-client` but did not receive a DSP
  reply.
* A bounded trace observed 45,476 successful four-byte
  `SPI_IOC_MESSAGE(1)` transfers in 12 seconds.

`dsp-client` requests GPIOs 4, 5, 12, 13, and 15, while `mcu-interface`
requests GPIO 3. The acquired Invoke `gpio-dwapb` driver requires a
`base-gpio` property. The older recovery DT omits it, so the platform driver
binds but unregisters every GPIO bank.

The next DTB added only `base-gpio = <0>` to the first SoC bank while
preserving the proven SPI node. The resulting kernel:

* Has SHA-256
  `66ccac301084be80eae8e8ac64bd3151a14c372feb82993f45f9df1d8f1059fc`
* Returned USB in six seconds
* Registered `gpiochip0` as `gpio_soc_0`, base 0, with 32 GPIOs
* Kept `/dev/spidev0.0`, Wi-Fi, and the read-only NAND boundary active
* Configured MCU GPIO 3 as a falling-edge input
* Let `dsp-client` drive GPIOs 4, 5, 12, 13, and 15 through its normal startup
* Received `EVENT_DSP_BOOTUP` from the physical DSP
* Completed `com.harman.dsp.getVer` and published
  `com.harman.dsp.version [25688]`

The DSP boot event invoked the normal MCU DAC and amplifier unmute procedures.
The DSP transcript identifies the same value as
`EVENT_DSP_VERSION=0.0.64.58`: the WAMP integer is hexadecimal `0x6458`, or
decimal `25688`.

The transport, frame layout, checksum, GPIO handshake order, expander-backed
reset, and boot-image staging behind these observations were later recovered
from the donor binary itself and are recorded in
[dsp-boundary.md](emulation/dsp-boundary.md). The captured
`readmsg: 0x00 0x01 0x04` line in that run is message id 1, event code 4,
`EVENT_DSP_BOOTUP`, which is what makes the unmute happen.

A later run on 2026-09-03 captured the SPI `ioctl` boundary byte for byte while
forwarding every call unchanged, archived as
`hardware/software-captures/20260903T191657Z-dsp-ioctl-record/`. It contains
40,121 four-byte image transfers whose concatenated payloads are byte-identical
to the per-byte bit-reversal of `dsp-img.ldr`, followed by 23 one-byte message
transfers carrying the boot event and one `com.harman.dsp.getVer` exchange.
That settles the DSP program as **host-loaded and volatile**: it lives in a
file on the host filesystem and is retransmitted in full at every service
start, so a replacement platform has to carry the image, not merely the
protocol. Outputs were remuted after the run.

## Board-support audit

| Layer | Verified identity | Persistence and replacement boundary |
|-------|-------------------|--------------------------------------|
| Wi-Fi | Marvell SDIO functions `02df:9135`, `9136`, and `9137`; station interface `mlan0` | Linux downloads firmware at module load; safe to bundle in our initramfs |
| Wi-Fi firmware | `sd8887_wlan_a2_p78.bin`, SHA-256 `1230d8073a271b38733685671eedeb1f5042e8edcbe1bf5a67d654baa979b59e` | Volatile controller load |
| Wi-Fi calibration | `WlanCalData_ext-LS9AD-20160725.conf`, SHA-256 `11cb55e4f238ce179fb33e197aec11c3ed4ea578f735ef1b0ec5a4fedabd0431` | Board-specific donor data |
| Bluetooth firmware | `sd8887_bt_a2_new.bin`, SHA-256 `c04b50f8e3a604d85dd7e4a6e05545ec6d0fae417f5898910722993bee62bc73` | Separate volatile controller load |
| MCU image | `cortana_mcu.bin`, 13,312 bytes, SHA-256 `af0db96faaa79fcff254c5c95cef858e1fc6543ad73238b740f28f0e9fd98811` | Separate MCU upgrade surface exists; do not use it |
| DSP image | `dsp-img.ldr`, 160,484 bytes, SHA-256 `e76f6ce7c53bb5b508507354fb08523089c136b3731d5ad4f4488a50526a44c8` | Host-loaded and volatile: pushed bit-reversed over SPI on every start, confirmed byte-identical on hardware |

The recovery kernel has no active ALSA card, no SPI controller, and no
Invoke-specific SD8887 Bluetooth transport module. Wi-Fi works because its
recovery module supports SDIO function `9135`.

The first recovery-kernel control changed only a temporary copy of the installed
module's `vermagic`. It created `hci0`, but its first HCI command timed out.

The audio kernel now uses `bt8xxx.ko` built from the Invoke GPL source with
exact `3.8.13-reinvoke-audio` vermagic. The module SHA-256 is
`b77adca16d3c2778a047243f824b8fea339603343c88da32ab4c42e952bbd522`.
It downloaded `sd8887_bt_a2_new.bin`, reported `BT FW is active(2)`, created
`hci0`, and registered an unblocked Bluetooth rfkill device.

Bluedroid then:

* Enabled the adapter with its controller-provided local address
* Initialized A2DP Sink, AVRCP Controller, and AVRCP Target with result `0`
* Set the compatibility name `HK Invoke_4E5601`
* Entered connectable mode
* Entered connectable and discoverable pairing mode on a WAMP request

All configuration and bond paths point to RAM-backed `/data`. An iPhone
completed pairing and negotiated A2DP at 44.1 kHz stereo. The physical rotary
ring changed the real ALSA `music` control and Bluedroid forwarded those values
to the phone's absolute-volume scale.

Ubuntu 22.04 on the development Mac mini provided a controlled second source.
BlueZ discovered, paired, and connected the Invoke's classic Audio Sink profile.
PulseAudio loaded its Bluetooth policy and discovery modules, selected
`a2dp_sink`, exposed an SBC 44.1 kHz stereo sink, and showed an active sink
input during controlled playback.

Both sources sent sustained RTP/SBC traffic to dynamic L2CAP channel `0x44`.
The Invoke received thousands of incoming ACL packets, including SBC frames
with the expected `0x9c` sync byte. The donor Bluedroid stack did not emit its
A2DP audio-start callback, connect its PCM consumer, or open ALSA card 1.
A read-only client connected to its abstract `.a2dp_data` socket but received
zero decoded bytes. The standard `.a2dp_ctrl` `CHECK_READY` command returned
failure acknowledgement `1`.

This verifies pairing, A2DP negotiation, and compressed media ingress. Decoding
and PCM handoff in the donor Bluedroid stack remain unresolved. The replacement
architecture should not depend on fixing that opaque userspace stack; a
maintained Bluetooth stack or a small owned SBC-to-ALSA bridge is preferred.

The recovery kernel exposes neither an SPI master nor a usable low-numbered GPIO
bank. The SPI-plus-GPIO replacement kernel closes both gaps. The real MCU
adapter opened `/dev/i2c-0`, completed its mute-first sequence, reported
application version `000116` and recovery flag `0`, and configured GPIO 3 as
expected. The real DSP then completed its boot notification and version
response over SPI.

The installed kernel metadata identified the audio increment. Its
`modules.builtin` contains ALSA loopback, the Berlin PCM, DHUB, AIO, SPDIF,
AVPLL and playback components, and `snd-soc-wm8904`. This confirms WM8904 as an
installed-kernel component rather than relying only on the ACast reference
device tree.

The public `courk/gmini-linux` Berlin PCM source at commit
`764b617b647c91fe969332ceb690282ecdad4e0c` directly calls
`snd_berlin_card_init()`. Harman's Invoke source adds an immediate `return 0`
inside that initializer and adds a separate WM8904 ASoC machine driver. This
supports using the Invoke ASoC machine path rather than removing the return to
revive the older direct-card path. The pinned comparison file is recorded as
`P2-005`.

### Live audio validation

The audio kernel registered:

* ALSA card 0 as `Loopback`
* ALSA card 1 as `marvell-wm8904`
* Card 1 PCM 0 with one playback and one capture substream

The WM8904 I2C probe at `0x1a` returned `-121` for early register accesses, but
the ASoC machine link still registered. This matches the observed design in
which MCU-controlled DAC and amplifier stages plus the SPI-loaded DSP form the
physical output path. The unresolved I2C response must not be treated as proof
that a discrete WM8904 is present.

With MCU-controlled amplifier and DAC mute asserted, `aplay` opened card 1 at
48 kHz, stereo, `S32_LE`, sent one second of zero samples, and closed without an
xrun, DMA error, or kernel fault. A second guarded test:

1. Asserted amplifier and DAC mute.
2. Unmuted DAC and amplifier only for the playback window.
3. Played a 0.5-second, 1 kHz, -48 dBFS PCM tone.
4. Remuted amplifier and DAC through an exit trap.

The operator audibly confirmed the tone. This proves the RAM-owned
kernel-to-speaker output path.

The capture side is not yet operational. On 2026-09-03, the target exposed
card 1 PCM 0 as a capture substream, but the donor `tinycap` utility could not
set hardware parameters on that device. The failure reproduced before DSP
startup, after the owned DSP image booted, and after `micTestNormal`, including
the documented 48 kHz stereo `S32_LE` format and several period sizes. Each
attempt captured zero frames. Card 0 loopback capture did open, so the utility
and ALSA capture ioctl path work; this does not constitute microphone evidence.
Microphone support remains blocked on identifying the required Berlin/WM8904
capture configuration or completing the kernel capture path.

The physical rotary ring also produced 63 volume-up and 57 volume-down MCU
events. Bonefish recorded exactly 120 matching
`com.harman.test.inputEvent` publications. After the stock empty-stream ALSA
initialization created the soft-volume elements, `audio-ui` set the real
`music` mixer control to 20 percent. Later physical turns changed both channels
to matching values, including 32 percent, and Bluedroid translated the same
changes to the paired peer's absolute-volume scale.

## Userspace component audit

Semantic versions are recorded only when an artifact exposes one. Build IDs and
hashes identify stripped binaries when no semantic version is available.

| Component | Installed evidence | Status in the replacement architecture |
|-----------|--------------------|-----------------------------------------|
| Bonefish | Build ID `11d0eab176300d7c3c585b87dd7b82b0dd250d9d`; SHA-256 `f8ca28a9536b2795adee89d17c38a616fca859b89bdf11529228790e36584b24` | Runs under custom PID 1; replaceable WAMP router |
| Autobahn-C++ | Symbols embedded in Bonefish and clients; semantic version unresolved | Protocol dependency, not a required long-term binary |
| `mcu-interface` | Build ID `140341f267dc32d01b82175787e8d86fc75162bc`; SHA-256 `25e09fa524d0df037d53ceafc93fa938fb73dd008bc5b8d366b3ff968d9c20b7` | Temporary board-support adapter; protocol can be reimplemented |
| `dsp-client` | Build ID `bee96ed4a94512944506d92660f1043df0a99385`; SHA-256 `a6ce3ff85ff04d9978e3f60acfe1339c561148254e610c7f392f2eb8fe5c72b8` | Temporary DSP adapter; live SPI/GPIO and direct PCM output are verified |
| Bluedroid service | Binary SHA-256 `0a551959cf9b185722c8979834e8f56388890f9240685fa5cfa3f329abf442af` | Donor reference; a modern Bluetooth stack is preferred |
| Dropbear | `2015.68` | Available donor SSH server, but too old for the final platform |

## Preserved hardware evidence

The external evidence directory
`reinvoke-archive/hardware/dumps/20260902T215700Z-native-ram/` contains:

* Complete logical NAND data image and SHA-256 manifest
* Extracted active and configuration SquashFS trees
* Installed kernel-partition carve
* Component hash inventory
* Kernel log, mount table, MTD table, module list, and network state
* Bonefish, MCU, DSP, and Bluetooth service logs
* Bounded real-hardware MCU syscall trace
* Wi-Fi scan count with no SSIDs retained

The SPI-plus-GPIO milestone is preserved under
`reinvoke-archive/hardware/usb-attempts/20260903T022804Z-replacement-kernel-gcc49-spi-gpio-original-absent/`.
Its USB capture contains 20,380 packets with zero drops. The
`native-platform-evidence/` subdirectory records the live kernel, mounts,
modules, network, GPIO, SPI, process, MCU, DSP, and WAMP state. NAND remained
unmounted.

The audio and Bluetooth milestone is preserved under
`reinvoke-archive/hardware/usb-attempts/20260903T031355Z-replacement-kernel-gcc49-audio-original-absent/`.
Its 130,649,184-byte USB capture contains 218,178 packets with zero drops. The
private evidence subtree holds bond and HCI records with directory mode `0700`
and file mode `0600`; those identifiers are not copied into Git.

The final reproducible RAM pair is:

| Artifact | SHA-256 |
|----------|---------|
| Audio kernel with packaged native Bluetooth | `fb4340f7d92a40ac32a4a58af166e7a1d5f0897978ee4a95e4a551208938328e` |
| Audio/Bluetooth initramfs | `afdf0f5171bf299dd71b36f4d8a8f3269bf4d20b2e7d4bb0139f7cc360e4ab84` |
| Audio DTB | `4dd7a39aa8c8d23ee824724e3f633ec16bb3a0f28c46cb096f0552dc28737dbb` |
| Native `bt8xxx.ko` | `b77adca16d3c2778a047243f824b8fea339603343c88da32ab4c42e952bbd522` |

The initramfs automatically loads the volatile Bluetooth firmware and native
module. The checksum-gated service launcher was then tested from a clean
service state: Bonefish, MCU, DSP, audio UI, source manager, identity adapter,
and Bluetooth all started on their first attempt. The MCU was ready in two
seconds, the DSP answered, volume initialized to 20 percent, and NAND remained
unmounted.

The donor DSP boot event transiently requests amplifier and DAC unmute before
the launcher can reassert mute. The safe unattended default therefore skips
`dsp-client`; attended audio work must opt in with `--start-dsp` and wait for
the launcher to confirm that both outputs were remuted.

The native SD8887 STA/uAP milestone is preserved under
`reinvoke-archive/hardware/usb-attempts/20260903T105444Z-sd8887-sta-uap-reconnect-arm-stock/`.
The candidate returned the USB gadget in five seconds and exposed `mlan0`,
`p2p0`, `hci0`, GPIO, SPI, and both ALSA cards. A Mac mini joined a random-key
WPA2 AP on `p2p0`, received DHCP without a gateway or DNS, pinned the
provisioning certificate, and completed an HTTP 202 parser-to-adapter request.
Forwarding remained disabled, NAND remained unmounted, and all temporary
credentials and AP processes were removed. The active host staging directory
was then restored to the proven audio/Bluetooth pair above.

The same boot later cloned the Mac mini's active WPA2 profile through the
authenticated parser without printing or storing the plaintext PSK on the
host. The real station adapter reached `wpa_state=COMPLETED`, retained only a
derived key in mode-0600 RAM, acquired a DHCP lease, and verified gateway,
public IPv4, and DNS reachability. The station and renewal client remain
ephemeral, and NAND remains unmounted.

### Owned network lifecycle

The static ARMv7 `reinvoke-networkd` service replaced the temporary DHCP hook
without changing the derived station credentials. It detected the existing
supplicant, started a supervised BusyBox DHCP client, atomically installed
RAM-only resolver state, and preserved gateway, public IPv4, and DNS
reachability.

Graceful service termination removed the DHCP child, IPv4 address, default
route, resolver link, and lease state. Restarting the service reacquired them.
A forced station disconnect produced the same cleanup, and reconnect restored
connectivity. A full-lifetime lock rejected a second supervisor without
disturbing the active instance. The final artifact identity is recorded in
[P1-046](../metadata/P1-046.json).

Hardware validation also pointed a stale owner record at an unrelated live
process. The corrected service removed the stale records, preserved the
unrelated process, started one DHCP child, and restored IPv4 and DNS. DHCP event
handlers now verify the active owner token and interface before changing state.

An authenticated replacement request then stopped the prior supplicant and
DHCP child while the same network supervisor remained active. The replacement
created new child PIDs and regained association, lease, route, resolver, public
IPv4, and DNS. The host passed its active profile entirely through shell memory;
no plaintext credential was printed or written to a host file.

The final hardened checksum-gated lifecycle image was built twice with
identical bytes. The external 40,068,440-byte initramfs has SHA-256
`c056d21b0e147fb9fd38a9458952528be1f58b17566f1223a1147eca14d53e21`.
Archive inspection confirmed the owned daemon, provisioning adapters, release
manifest, pinned module tree, and updated PID 1. PID 1 restarts networkd after
failure with a five-second delay, sends its output to the bounded kernel log, and
supports `reinvoke.networkd=off` for manual recovery.

On 2026-09-03 the final hardened checksum-gated image was cold-booted through
yellow mode. PID 1 auto-started `reinvoke-networkd`, whose live SHA-256 matched
`cb61bcdd0b9f4b145619514b9acb41d74d98042f8698419ea37e0c4864340a66`.
The packaged `reinvoke-provisiond` also matched
`5bde5aefdb21a9caf605fb57e9a62cf9597b8ebddd1fc9d65938441d04678b07`.
The service entered its expected supplicant-wait state, and the mount table
showed only RAM-backed or virtual filesystems; no NAND or MTD block was
mounted. The SD8887 WLAN and Bluetooth drivers also loaded. Because this
acceptance image intentionally contained no station credentials, it did not
attempt association, DHCP, DNS, or default route acquisition; those
credentialed transitions remain covered by the live RAM-only validation above.

### RAM-only replacement Bluetooth control path

BlueZ 5.55, built statically for the target's EGLIBC 2.12.2 userland, registered
the classic adapter when launched with `ControllerMode=bredr`. BlueALSA 4.0.0
registered an A2DP sink and `bluealsa-aplay` opened no PCM until a peer connects.
The owned pairing agent registered on the private D-Bus system bus and
allowlisted one operator-supplied peer address plus A2DP/AVRCP services.

The Mac mini was freshly paired after its stale host record was removed. The
target reported `Paired=true` for that peer while `Pairable=false` and
`Discoverable=false`. The bond survived a disconnect and reconnect after the
pairing window closed. Target bond state remained in the volatile
`/usr/var/lib/bluetooth` directory, and the private D-Bus state remained under
`/tmp`.

With MCU amplifier and DAC mute asserted and host volume limited to one percent,
an A2DP stream opened ALSA card 1 as stereo `S16_LE` at 44.1 kHz. The hardware
pointer advanced from `192000` to `238080` while the PCM remained `RUNNING`.
This proves the muted Bluetooth transport, SBC decode, BlueALSA, ALSA, and DMA
pipeline. For the attended acceptance on 2026-09-03, the owned MCU WAMP
procedures explicitly unmuted the amplifier and DAC, the paired Mac mini sent a
440 Hz tone at 20 percent host volume, and the operator confirmed audible
speaker output. The host volume was then restored to one percent; the physical
outputs remain under the MCU mute procedures.
Provenance is in [P1-045](../metadata/P1-045.json).

## Rebuilding the RAM platform

No proprietary image is committed. Build the sanitized initramfs from held
artifacts:

```bash
tools/usb-boot/build-native-initramfs.sh \
  --source-initramfs ../reinvoke-archive/extracted/ota2/OTA2/82_IMAGE \
  --donor-rootfs ../reinvoke-archive/hardware/dumps/20260902T215700Z-native-ram/rootfs-extracted/primary \
  --kernel-modules ../reinvoke-archive/build/artifacts/invoke-kernel-acast/modules \
  --output ../invoke-boot/82_IMAGE.native-ram
```

Build the hardware-verified GCC 4.9 kernel profile with a checksum-gated DTB:

```bash
tools/kernel/build-native-kernel.sh \
  --profile spi-gpio \
  --dtb ../reinvoke-archive/build/artifacts/reinvoke-spi-gpio.dtb \
  --dtb-sha256 <reviewed-dtb-sha256> \
  --output-dir ../reinvoke-archive/build/artifacts/invoke-native-spi-gpio
```

After ADB returns, reconstruct the tested RAM-only diagnostic service graph:

```bash
tools/usb-boot/start-native-services.sh \
  --rootfs ../reinvoke-archive/hardware/dumps/20260902T215700Z-native-ram/installed-rootfs-region.bin
```

This launcher never starts the stock supervisor or updater and leaves physical
outputs muted.

Stage the reviewed `81_IMAGE` and generated initramfs as `81_IMAGE` and
`82_IMAGE` in the external boot directory. From the live U-Boot prompt:

```text
usbload 0x81 0x0c400000
usbload 0x82 0x08000000
set bootargs console=ttyS0,115200 loglevel=8 debug root=/dev/ram rdinit=/init init=/init initrd=0x08000000,<generated-size>
bootm 0x0c400000
```

The builder removes the vendor `flash_custk` executable and
`/home/galois/run.sh`, replaces PID 1, and never mounts NAND. The inherited
BusyBox still contains low-level applets, so operator discipline remains part
of the safety boundary.

## Replacement architecture

The target architecture is:

1. A maintained Linux kernel or the minimum compatible donor kernel layer
2. An initramfs or persistent rootfs owned by reInvoke
3. Board firmware and calibration treated as immutable donor assets
4. Native Wi-Fi and a maintained Bluetooth userspace
5. A small MCU driver that preserves mute-first power sequencing
6. A DSP/audio service that exposes stable local APIs
7. An application layer for local voice, automation, media, and updates
8. USB yellow mode retained as recovery

The current hybrid retains Bonefish and the donor `dsp-client` only as
comparison points. The reInvoke-owned MCU service has replaced donor
`mcu-interface` in a live RAM session. It reproduced mute-first expander and
DAC initialization, denied unmute by default, sent the recovered five-second
MCU heartbeat, returned version `000116`, and published physical volume-up and
volume-down events. Provenance is in [P1-048](../metadata/P1-048.json).

## Connectivity and provisioning model

SSH is not a product requirement. The platform needs at least one reachable
control plane, with different transports serving different lifecycle stages:

| Stage | Transport | Purpose |
|-------|-----------|---------|
| Recovery | Yellow-mode USB and U-Boot | Load a known RAM kernel and initramfs |
| Development | USB ADB or ACM | Root shell, logs, file transfer, and control API forwarding |
| Onboarding | Temporary SD8887 access point | Receive network selection and credentials locally |
| Normal operation | Wi-Fi station mode | Authenticated local API, media control, and automation |
| Maintenance | Optional SSH | Administrative access, not required for normal use |

The installed firmware contains the shape of the original access-point flow:

* SD8887 station and uAP interfaces named `wlan0` and `p2p0`
* dnsmasq on `p2p0` with DHCP range `192.168.43.100` through
  `192.168.43.155`
* browser endpoints for scan results, connect, forget, and save operations
* a setup page that posts the selected SSID and passphrase

The original page includes `TODO: Encrypt password` and sends JSON credentials
to a local HTTP endpoint. reInvoke should reuse the user flow, not that security
design. Credentials should be accepted only during an explicit physical
provisioning window, protected by an ephemeral session secret or authenticated
encrypted channel, written to a root-only configuration store, and never
logged.

The current dependency-free WAMP client proves that USB and Wi-Fi can carry the
same logical control API. The persistent platform may replace WAMP with a
smaller authenticated protocol while retaining a compatibility bridge for
tested MCU and audio procedures.

The first replacement control-plane component is
[native-provisioning.md](native-provisioning.md). Its static ARM daemon provides
ephemeral TLS and token-authenticated credential delivery without exposing
radio or shell privileges to the network parser.
