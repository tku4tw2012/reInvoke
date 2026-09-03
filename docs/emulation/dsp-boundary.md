---
title: DSP boundary
description: Recovered SPI, GPIO, I2C, and WAMP behavior of the donor dsp-client and what a replacement must honor
ms.date: 2026-09-03
ms.topic: concept
---

What the audio-DSP boundary looks like without opening the unit: the host-side
transport recovered from the preserved `usr/bin/dsp-client` binary, the wire
framing and event vocabulary, the parts of it already confirmed on the physical
unit, and what a replacement service would have to reproduce.

This is the DSP counterpart to [MCU boundary](mcu-boundary.md). No hardware was
opened, no probe was attached, and no provisioning file was touched to produce
it.

## Evidence classification

Verified facts:

* Every preserved rootfs holds the same 715,964-byte `usr/bin/dsp-client`,
  SHA-256
  `a6ce3ff85ff04d9978e3f60acfe1339c561148254e610c7f392f2eb8fe5c72b8`. All six
  held copies are byte-identical, spanning StockRoot 11.1842.0, the flashing
  bundle, OTA2 12.2134.0, and the 12.2050.3 NAND dump.
* The 160,484-byte `usr/share/dsp/dsp-img.ldr` is likewise byte-identical
  across all six held copies, SHA-256
  `e76f6ce7c53bb5b508507354fb08523089c136b3731d5ad4f4488a50526a44c8`.
* The binary is ARM EABI5, dynamically linked against `libboost_system` and
  `libboost_thread` 1.65.1 and `libstdc++`, with GNU build ID
  `bee96ed4a94512944506d92660f1043df0a99385`. It is stripped of debugging
  information but exports 1,628 defined dynamic symbols, including
  `dspopen`, `msgread`, `msgwrite`, `msgproc`, `Dsp_msg_process`,
  `Dsp_msg_handle`, `call_mcu_unmute`, `set_dsp_reset_control_pin`,
  `reset_dsp_reset_control_pin`, `I2C_Read_IO_Expander`,
  `request_dump_dsp_memory`, and `save_dsp_memory_dump`.
* On the physical unit, booted entirely from RAM with the SPI plus `base-gpio`
  kernel and nothing written to flash, `dsp-client` drove GPIOs 4, 5, 12, 13,
  and 15, received `EVENT_DSP_BOOTUP`, completed `com.harman.dsp.getVer`, and
  published `com.harman.dsp.version [25688]`.
* The captured device log contains the literal frame
  `readmsg: 0x00 0x01 0x04` immediately followed by `EVENT_DSP_BOOTUP` and
  `dsp call mcu unmute!!!`.
* A byte-exact `SPI_IOC_MESSAGE` log-and-forward capture was taken on the
  physical unit on 2026-09-03, archived as
  `hardware/software-captures/20260903T191657Z-dsp-ioctl-record/dsp-ioctl.log`
  (SHA-256 `d867a4dc…7732ab72ba`) with the matching service log
  `dsp-service.log` (SHA-256 `9f3f1cb7…a7d084c3b0`). It holds 40,144 SPI
  transfers: 40,121 four-byte image words followed by 23 one-byte message
  transfers. Concatenating the 40,121 transmit payloads yields 160,484 bytes
  whose SHA-256 is
  `9e3d85f37ac62e191616f558359e7b4ec46ce6499167da991994ea0b944f34f2`, which is
  exactly the per-byte bit-reversal of `dsp-img.ldr` (source SHA-256
  `e76f6ce7…526a44c8`). The DSP boot event was observed and the outputs were
  remuted afterwards.
* **The DSP program is host-loaded and volatile.** The host pushes the entire
  160,484-byte image over SPI on every service start, before any message
  traffic is possible. Nothing in the capture reads a DSP-side memory, and no
  code path skips the download. A replacement host must ship and send this
  image or the DSP does nothing.

Artifact-backed findings:

* The SPI parameters, frame layout, checksum, GPIO handshake order, I2C reset
  path, and image-download sequence in this document were read out of the
  donor binary's instructions and initialized data. They are reproducible from
  the held artifact by disassembly.
* The event and command tables were recovered from the two dispatch functions
  (`Dsp_msg_handle` for device-to-host, the nine `msgwrite` call sites for
  host-to-device) and cross-checked against the strings they print.

Inference:

* Pin *roles* below (attention, ready, strobe) are inferred from the order in
  which the binary reads and writes them, not from a schematic or a probe.
* The `.ldr` container is inferred to be a 48-bit-word DSP boot image, see
  [Boot image](#boot-image). No DSP part number is established by any of this,
  and `HKI-AUD-008` remains UNKNOWN.

Current limits:

* The capture covers one service start: a full image download, the boot event,
  and one `com.harman.dsp.getVer` exchange. It ended while the version reply
  was still being read, so that one frame is truncated in the log. No other
  procedure has been seen on the wire.
* The donor binary was never observed writing a payload larger than three
  bytes, so the multi-block path of `msgproc` is understood from code only.
* Nothing here establishes what the image *contains*, only that the host sends
  it verbatim. The `.ldr` container remains inference and `HKI-AUD-008` is
  still UNKNOWN.

## Transport

| Property | Value | Source |
| --- | --- | --- |
| Device | `/dev/spidev0.0`, `O_RDWR` | `dspopen` |
| Mode | 3, that is CPOL 1 and CPHA 1 | `.data` at `0xce5dc`, written with `SPI_IOC_WR_MODE` |
| Bits per word | 8 | `.data` at `0xce5cc` |
| Speed | 1,000,000 Hz | `.data` at `0xce5c8` |
| Transfer | `SPI_IOC_MESSAGE(1)`, exactly one transfer per call | shared helper at `0x8dc1c` |

`dspopen` issues the write and read pair of each of `SPI_IOC_WR_MODE`,
`SPI_IOC_WR_BITS_PER_WORD`, and `SPI_IOC_WR_MAX_SPEED_HZ`, so the values above
are the ones the donor actually programs. They independently corroborate the
conservative 1 MHz `spidev0.0` child chosen for the replacement DTB in
[Native RAM platform](../native-ram-platform.md).

## Frame format

Both directions use the same five-byte header. The frame is built in the
614,400-byte staging buffer at `0xd8f90`:

```text
byte 0  message id, high byte
byte 1  message id, low byte
byte 2  payload length, high byte
byte 3  payload length, low byte
byte 4  checksum
byte 5+ payload, padded
```

The wire length is `payload_length + 5` rounded up to a multiple of four, in
both directions. The checksum is the low byte of the sum of the four header
bytes and every payload byte:

```text
checksum = (id>>8) + (id&0xff) + (len>>8) + (len&0xff) + sum(payload)  (mod 256)
```

`msgwrite` computes that checksum when it queues a command, storing it at
offset 8 of its sixteen-byte ring entry alongside the id at offset 4, the
length at offset 6, and a malloc'd payload copy at offset 12. `msgproc` copies
the entry into the header and sends it.

The receive path reads the same five header bytes, then reads
`max(length, 3)` payload bytes, rounded by the same rule. It rejects a frame
whose first two bytes are `0xFF`, whose length is zero, or whose checksum does
not match. Its checksum is computed over the four header bytes and every
payload byte it read, padding included.

Two consequences matter for reading captures:

* The **payload's first byte is the opcode** on the host side and the **event
  code** on the device side. There is no separate opcode field.
* What `dsp-client` prints as `readmsg:` is **not the wire frame**. The
  receive path stores the two header id bytes followed by the payload, and
  `msgread` hands that tuple to `Dsp_msg_handle`. So the logged
  `0x00 0x01 0x04` is header id 1 plus payload byte `0x04`, which is
  `EVENT_DSP_BOOTUP`. The frame that produced it is
  `00 01 00 01 06 04 00 00`: id 1, length 1, checksum 6, one payload byte,
  padded to eight. The 2026-09-03 byte-exact capture shows exactly those eight
  bytes on the wire, so this is observed, not predicted.

### One transfer per byte

The donor does not send a frame in one `SPI_IOC_MESSAGE`. Its transfer helper
takes a length, and every caller except the image loader passes 1:

| Caller | Length | `tx_buf` | `rx_buf` |
| --- | --- | --- | --- |
| Image download | 4 | image + offset | NULL |
| Frame transmit | 1 | one byte of the frame | NULL |
| Frame receive | 1 | NULL | one byte of the frame |

So an eight-byte command costs eight ioctls, and receiving that
`EVENT_DSP_BOOTUP` frame costs eight more. This is the single most important
fact for interpreting a capture, and it is why raw ioctl counts are much
larger than frame counts. The 2026-09-03 capture bears it out directly: 40,121
four-byte image transfers, then 23 one-byte transfers carrying one received
event, one sent command, and a partial reply.

`cs_change` is never set, and `speed_hz` and `bits_per_word` come from
the two `.data` words above. A capture should therefore show
`speed_hz=1000000 delay_usecs=0 bits_per_word=8 cs_change=0` on every
transfer.

### Retry and re-download

`msgproc` polls the ready line with a six-deep counter at `.data` `0xce5d8`.
Each miss sleeps 500 milliseconds and decrements it. When it reaches zero the
donor resets the counter and **downloads the whole image again**, then returns
an error. A capture that contains two download runs has hit this path, which
is a three-second stall the log will show as a gap.

## Control lines

`dsp-client` uses sysfs GPIO for the handshake, `/sys/class/gpio/export`,
`gpio%d/direction`, and `gpio%d/value`.

| GPIO | Direction in the handshake | Inferred role |
| --- | --- | --- |
| 4 | written, pulsed low then high | Strobe to the DSP |
| 5 | exported only during image download, driven as a chip select | Boot chip select |
| 12 | read | Device ready, active low |
| 13 | written, framed around every transfer | Transfer active |
| 15 | read before queuing a transmit | Device busy |

`msgproc` runs this order for a queued message:

1. Set GPIO 13 direction out, value 0.
2. Compare the transmit ring head and tail. Bail out if empty.
3. Read GPIO 15. If it is high, return without transferring.
4. Set GPIO 13 to 1, sleep 1 microsecond, build the frame in `genbuf`.
5. Sleep 1 microsecond, set GPIO 13 to 0.
6. Poll GPIO 12 until it reads 0.
7. Pulse GPIO 4 low, sleep 1 microsecond, raise GPIO 4.
8. Set GPIO 13 to 1, sleep 1 microsecond.
9. Perform one `SPI_IOC_MESSAGE(1)` transfer.
10. Release the ring slot, sleep 2 microseconds, set GPIO 13 to 0.

The receive path repeats steps 6 through 9 without a queued frame, which is how
unsolicited events arrive. `Dsp_msg_process` calls `msgproc` in a loop and
sleeps 200 milliseconds whenever it reports no traffic.

The binary additionally shells out to `/system/bin/toolbox devmem` through
`popen` and `system` to read and modify three registers:

| Register | Restored constant | Use |
| --- | --- | --- |
| `0xF7EA8008` | `0x0118D249` | Pin function switch between SPI and GPIO 5 |
| `0xF7E80400` | `0x00000A08` | GPIO data |
| `0xF7E80404` | `0x00000F28` | GPIO direction |

The reInvoke initramfs has no `/system/bin/toolbox`, so these calls fail
silently, yet the DSP still booted and answered on hardware. The register
writes are therefore not required when the device tree already selects the
right pin functions, which is an artifact-backed conclusion about this
platform, not a general one.

## Reset line

The DSP reset is not a SoC GPIO. `set_dsp_reset_control_pin` and
`reset_dsp_reset_control_pin` open `/dev/i2c-0`, address the peer at `0x20`
with raw `I2C_RDWR`, read register `0x01`, and set or clear bit 0 with a
read-modify-write.

That is the same expander and the same output register that `mcu-interface`
touches when it reports `MCU init io expander. mute amp and dac!!!`. Two
independent services therefore agree that the amplifier mute, the DAC mute, and
the DSP reset all sit on output port 0 of one `0x20` expander. Because both
services read-modify-write, neither clobbers the other's bits. See
[MCU boundary](mcu-boundary.md) for the expander evidence from the MCU side.

## Boot image

`dspopen` loads the DSP program itself. It tries `/media/usb/dsp-img.ldr`,
then `/data/test/dsp-img.ldr`, then `/usr/share/dsp/dsp-img.ldr`, reads up to
614,400 bytes, and then:

1. Bit-reverses every byte, most significant bit to least significant.
2. Pulses reset: set, clear, sleep 20 ms, set, sleep 10 ms.
3. Switches the `0xF7EA8008` pin function, exports GPIO 5, sets it to output,
   raises it, then drives it low as an active-low chip select.
4. Saves the current transfer speed, forces 1,000,000 Hz, and streams the image
   in four-byte `SPI_IOC_MESSAGE(1)` transfers with no inter-word delay.
5. On reaching byte offset 1536, restores the saved speed and pauses 10 ms
   before continuing.
6. Raises the chip select, unexports GPIO 5, restores the pin function.

Step 5 is a no-op on this unit. The saved speed and the forced speed are both
1,000,000 Hz, and the capture shows all 40,121 image transfers at that one
speed. The code path exists and would matter on a unit whose stored speed
differed; here only the 10 ms pause has any effect.

Two structural observations about the held `dsp-img.ldr` shape how it should be
read:

* The stage boundary at offset 1536 is exactly 256 words of 48 bits.
* The file up to that boundary parses cleanly as 48-bit words. The first five
  words are zero, then words such as `a7 c0 08 04 be 06`, `00 00 00 00 7b 0f`,
  `00 00 00 00 7a 0f` repeat an opcode-like final byte. After offset 1536 the
  structure changes to block-like data containing ASCII.

A fixed-size first stage of 256 forty-eight-bit instruction words, followed by
loadable blocks, is the shape of a boot kernel plus payload for the DSP
families that boot as an SPI slave from a host. This document cites no
datasheet for that, so it stays inference. It names no part, and the byte
reversal only says the receiver expects the opposite bit order from this SPI
master.

At 160,484 bytes the image needs exactly 40,121 four-byte transfers, and the
capture contains exactly that many. Concatenating their transmit payloads
reproduces the bit-reversed `dsp-img.ldr` byte for byte, with no header, no
framing, and no chunk accounting around it: the file is bit-reversed and
pushed. That settles the download as a verbatim transfer of a host-held file.

The consequence is that **the DSP holds no program of its own.** It is
reloaded from the host filesystem at every service start, so the image is a
build artifact a replacement must carry, not a device property that survives
reflashing the host. Losing `dsp-img.ldr` silences the speaker even with a
perfect protocol implementation.

Message traffic is separately visible because it moves one byte per ioctl.
This capture's 23 post-boot transfers are the boot event, the `getVer` call,
and its truncated reply. An earlier bounded trace counted 45,476 transfers in
twelve seconds, whose 5,355-transfer remainder over the image count is
single-byte frame traffic from a longer session; that trace was not retained in
this format and is not re-derived here. Counting a capture by direction is what
`tools/emulation/spi-capture-label.mjs` does.

## WAMP surface

`dsp-client` dials the Bonefish router at `127.0.0.1:9999`, realm `default`,
and registers as a client. Startup order, confirmed by both the disassembly and
the captured log, is: connect transport, join realm, register handlers,
`dspopen`, then the message loop.

Host to DSP, one row per `msgwrite` call site:

| Procedure | Message id | Payload |
| --- | --- | --- |
| `com.harman.dsp.micTestSingle` | 2 | `00 <mic>` |
| `com.harman.dsp.micTestPair` | 2 | `01 <pair>` |
| `com.harman.dsp.micTestNormal` | 2 | `02` |
| `com.harman.test.dspBypassMode` | 2 | `03 <mode>` |
| `com.harman.dsp.volumeSet` | 0 | `04 <volume>` |
| `com.harman.dsp.getVer` | 0 | `08` |
| `com.harman.dsp.micMute` | 0 | `09 <mute>` |
| `com.harman.stateChanged` (subscribed) | 0 | `0B <state>` |
| `com.harman.dsp.dumpDspMemory` | 0 | `0C <lo> <hi>` |

DSP to host, from `Dsp_msg_handle`:

| Message id | Code | Name | Effect |
| --- | --- | --- | --- |
| 0 | `04` | `EVENT_NEW_DAC_GAIN` | logged |
| 0 | `05` | `EVENT_EXPECT_SPEECH` | logged |
| 0 | `06` | `EVENT_CANCEL_TRIGGER` | logged |
| 0 | `07` | `EVENT_SW_UPGRADE` | logged |
| 0 | `08` | `EVENT_DSP_VERSION` | publishes `com.harman.dsp.version` |
| 0 | `09` | `EVENT_MIC_MUTE` | logged |
| 0 | `0B` | `EVENT_CORTANA_SKYPE` | logged |
| 0 | `0C` | memory dump payload | appended by `save_dsp_memory_dump` |
| 0 | `FF` | `EVENT_ERR` | logged |
| 1 | `00` | `EVENT_TRIGGER_FOUND` | logged |
| 1 | `01` | `EVENT_PAYLOAD_DEGIN` | logged |
| 1 | `02` | `EVENT_PAYLOAD_END` | logged |
| 1 | `03` | `EVENT_PAYLOAD_TIMEOUT` | logged |
| 1 | `04` | `EVENT_DSP_BOOTUP` | calls `call_mcu_unmute` |
| 1 | `FF` | `EVENT_WRITE_ERR` | logged |
| 2 | `00` | `EVENT_MIC_TEST_SINGLE` | logged |
| 2 | `01` | `EVENT_MIC_TEST_PAIR` | logged |
| 2 | `02` | `EVENT_MIC_NORMAL` | logged |
| 2 | `03` | `EVENT_HW_PERFORM_TEST` | logged |
| 2 | `FF` | `EVENT_TEST_ERR` | logged |

`EVENT_DSP_VERSION` carries four payload bytes. They are printed as
`%X.%X.%X.%X` and packed big-endian into one integer. The hardware run printed
`EVENT_DSP_VERSION=0.0.64.58` and published `[25688]`, and
`0x00 0x00 0x64 0x58` packs to `0x6458`, which is 25688. The packing rule is
therefore confirmed against captured output.

`call_mcu_unmute` calls `com.harman.vui.mutedaccontrol` and
`com.harman.vui.muteampcontrol`. This is why starting the donor DSP adapter
transiently unmutes the DAC and amplifier, and why
`tools/usb-boot/start-native-services.sh` keeps it behind the opt-in
`--start-dsp` flag.

## What a replacement must reproduce

A replacement `dsp-client` is a well-bounded problem. It is the only service
that speaks the SPI link, and its outward dependencies are the router, the SPI
node, five GPIOs, and one expander bit. Power sequencing is not its job:
`mcu-interface` registers `com.harman.vui.powerdspcontrol` and logs
`mcu_interface start power_on_dsp()` before the DSP answers.

Required, in order of risk:

1. WAMP client on `127.0.0.1:9999`, realm `default`, registering the eight
   procedures above and subscribing to `com.harman.stateChanged`, so that
   callers keep working. The captured router log shows exactly those eight
   registrations.
2. Publishing `com.harman.dsp.version` on `EVENT_DSP_VERSION`.
3. The `spidev0.0` configuration, frame layout, and checksum above.
4. The GPIO handshake order in [Control lines](#control-lines).
5. The image download, including the byte reversal, four-byte chunking, the
   10 ms pause at offset 1536, and GPIO 5 as chip select. A replacement must
   also **ship `dsp-img.ldr` itself.** The DSP program is host-loaded and
   volatile, sent in full at every start, so the image is part of the
   replacement's payload rather than something already resident in the
   speaker.
6. The expander read-modify-write on `0x20` register `0x01` bit 0 for reset.

Deliberately not required: the `devmem` shell-outs, the Breakpad minidump
writer that targets `/data/crash`, and the memory-dump path.

The one behavior a replacement should *not* copy blindly is
`call_mcu_unmute`. A replacement can gate it, which would remove the reason
`--start-dsp` has to stay opt-in.

## Non-invasive instrumentation

What can be observed from software alone, with no probe and no writes to any
device:

| Observable | How | Invasiveness |
| --- | --- | --- |
| Decoded id and payload of every received frame | `dsp-client` already prints `readmsg: 0xNN ...` for every frame whose code is not `0x0C` | none, it is stdout |
| Decoded event names | the same log lines | none |
| DSP firmware version | `com.harman.dsp.version` publication, or the `EVENT_DSP_VERSION` log line | none |
| WAMP registrations and calls | Bonefish's own log, or a subscribe-only monitor | none, see `tools/control/wamp-monitor.mjs` |
| Frame validity, checksum, command encoding | offline decoding, see below | none |
| Every byte on the wire, both directions | `LD_PRELOAD` ioctl log-and-forward, see below | log-and-forward only, no substitution |
| Whether the download reaching the DSP matches the held artifact | offline diff of that capture, see below | none, it reads files |

`tools/control/dsp-frame-decode.mjs` implements the last row. It is an offline
decoder for the frame format above. It opens no device node, sends nothing, and
its command mode only prints the bytes a given procedure *would* put on the
wire.

The vehicle for the first four rows is the RAM-booted native platform in
[Native RAM platform](../native-ram-platform.md). Because that platform runs
from an initramfs and writes nothing to flash, running the donor
`dsp-client` under it and reading its stdout is observation, not modification:
power-cycling the unit returns it to the stock image. That is the only reason
any of the verified rows above exist.

## Capturing and comparing a transfer log

Most of the above was recovered from a binary and then checked against reality
by logging the `ioctl` boundary of the donor process and comparing the result
offline. That capture has now been taken; this section is both the procedure
that produced it and the procedure for repeating it.

`tools/emulation/invoke-ioctl-shim.c` already does the capture half. In record
mode it disables every synthetic handler, writes each `SPI_IOC_MESSAGE` request
and result byte for byte, and forwards the call unchanged. It is owned and
maintained outside this document. What follows is the DSP-side use of it and
the separate offline tool that reads what it produces.

### Capture criteria

A capture is usable for DSP analysis only if all of these hold.

| Criterion | Why it matters |
| --- | --- |
| `INVOKE_IOCTL_MODE=record` | Any other mode substitutes synthetic results, so the bytes would not be the DSP's. |
| `INVOKE_IOCTL_LOG` set to a file on a writable RAM-backed path | Record mode buffers; writing to a slow or full filesystem changes timing enough to trip the 500 ms ready timeout. |
| The log starts before `dspopen` runs | The image download is the first SPI traffic. A log opened later cannot be diffed against the image. |
| `/usr/share/dsp/dsp-img.ldr` present and byte-identical to the preserved copy | The comparator's expected stream is derived from that file. Check the SHA-256 first. |
| Only one process has `spidev0.0` open | A second opener interleaves transfers and the reassembly is meaningless. |
| Capture runs to at least one `EVENT_DSP_BOOTUP` | That is the first frame the DSP sends, and the only one already corroborated by a hardware log. |
| Free space for roughly 20 MB per download | One download is 40,121 ioctls and about 160,000 log lines. |
| Nothing calls a state-changing procedure during the window | Keeps the capture attributable. `com.harman.dsp.getVer` is the only safe call. |

The capture is log-and-forward, so it changes what the process *does* only by
slowing it. It writes nothing to flash and substitutes no result. On the
RAM-booted platform a power cycle discards it entirely.

### What to compare

The comparator, `tools/emulation/spi-capture-label.mjs`, reads the log after
the fact and answers four questions:

1. **Does the download match the artifact?** It bit-reverses the preserved
   `dsp-img.ldr`, concatenates the captured four-byte transfers, and reports
   byte-identical, clean prefix with a truncation offset, or the offset of the
   first mismatching byte. This is the direct test of the bit-reversal claim
   and of whether the whole image reaches the part.
2. **Where does every transfer go?** It labels each one `image`, `tx-byte`,
   or `rx-byte` using the response line, since the donor passes a null
   `rx_buf` for everything it sends and a null `tx_buf` for everything it
   reads. The counts have to add up to the total, which is how image traffic
   and message traffic are told apart at all.
3. **Do the frames hold together?** It reassembles the one-byte transfers into
   frames using the length field, verifies each checksum by the donor's own
   rule, and names the procedure or event.
4. **Do the transfer settings match?** It flags any transfer whose speed, word
   size, chip-select behaviour, or inter-word delay departs from what hardware
   showed: 1 MHz, 8 bits, no chip-select change, and `delay_usecs` of 0 for
   image words and 1 for message bytes.

```bash
node tools/emulation/spi-capture-label.mjs capture.log \
  --image /usr/share/dsp/dsp-img.ldr --frames
```

The tool is read-only. It opens the log and the image file, opens no device
node, and sends nothing. It parses only `SPI_IOC_MESSAGE` records, so it does
not duplicate or depend on the shim's I2C, HCI, or ALSA handling.

### What the capture settled

Run against the archived 2026-09-03 log, the comparator reports 40,121 image
transfers, 8 transmit bytes, 15 receive bytes, and the download verdict
"byte-identical to the bit-reversed image".

* **The device-to-host frame does carry the five-byte header.** The predicted
  frame for `EVENT_DSP_BOOTUP` was `00 01 00 01 06 04 00 00`, and that is
  exactly what appeared on the wire. The `readmsg: 0x00 0x01 0x04` line the
  donor prints is the header id followed by the payload, as claimed, not the
  frame. The earlier three-byte-header claim is dead.
* **The image arrives intact and verbatim.** 160,484 bytes in, 160,484 bytes
  out, no padding and no truncation. The bit-reversal claim holds.
* **`com.harman.dsp.getVer` encodes as predicted:** `00 00 00 01 09 08 00 00`,
  checksum valid, reply id 0 with payload opcode `0x08` and length 5.
* **The speed change at offset 1536 is a no-op here.** Every image transfer ran
  at 1,000,000 Hz.
* **Message transfers carry `delay_usecs=1`,** not 0 as static reading of
  `.bss` predicted. Image transfers carry 0.

Still open after this capture: whether an unsolicited event's header id always
equals its payload id, whether the second download path ever fires in normal
operation, and what message id 0 codes below 4 are. None of those appeared in
this window.

Deliberately rejected as invasive or unsafe on a preserved donor unit:

* `ptrace` or `gdb` attachment to the running `dsp-client`, which can stop the
  process mid-transfer with GPIO 13 asserted.
* `/dev/mem` reads of the SPI or GPIO blocks, which need `devmem` and a
  writable mapping to be useful.
* Driving `spidev0.0` from a second process while the donor holds it, which
  would interleave frames.
* Calling `com.harman.dsp.dumpDspMemory`, `com.harman.test.dspBypassMode`, or
  any mic-test procedure, all of which change DSP state. Only
  `com.harman.dsp.getVer` is side-effect free.

## Open questions

* Where `delay_usecs=1` comes from. Static reading found only a `.bss`
  halfword written to zero, but hardware shows 1 for every message transfer
  and 0 for every image word. Something sets it between the download and the
  first frame, and that write has not been located in the binary.
* Whether the header id of an unsolicited event always equals the id that
  `Dsp_msg_handle` reads from the payload. The donor only compares the two for
  solicited replies, so an event could carry a different header id and nothing
  in the log would show it.
* Why the loader saves and restores a transfer speed at all. The capture shows
  every image word at 1,000,000 Hz, so on this unit the save and restore is a
  no-op, but the code exists and implies the two stages were meant to be able
  to differ.
* The DSP part identity, which no software-only evidence can settle.
* The meaning of message id 0 codes below 4, which the donor dispatcher routes
  to its `other event=0x%02X` fallback.
