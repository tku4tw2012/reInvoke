---
title: DSP boundary
description: Owned DSP policy and recovered SPI frames, image loading, GPIO handshake and WAMP contracts
ms.date: 2026-09-12
ms.topic: reference
---

`reinvoke-dsp-interface` replaces donor `dsp-client` for image loading,
SPI/control framing, handshake GPIOs and the shared-expander DSP reset bit.
The [MCU service](mcu-boundary.md) owns power and speaker authorization.
SPI carries firmware/control, not PCM; see the
[speaker diagram](owned-speaker-control.md#pcm-and-speaker-safety).

Byte-exact captures and pinmux/restart tests below are historical RAM evidence.
Candidate 02 independently returned native version event `[25688]`, not a
native microphone/privacy or shell-level kernel audit. The
[current contract](../current-product-contract.md) defines product ownership.

## Current owned boundary

The service checksum-gates and reloads `dsp-img.ldr` at every start. It never
copies the donor's automatic amplifier/DAC unmute on DSP boot.
Seven public WAMP procedures remain: `com.harman.dsp.micTestSingle`,
`micTestPair`, `micTestNormal`, `volumeSet`, `getVer`, `dumpDspMemory`, and
`com.harman.test.dspBypassMode`.

Raw microphone opcode `0x09` is available only through DSP-owned
`/run/reinvoke/dsp-mic-control.sock`, mode `0600`.
The MCU registers `com.harman.dsp.micMute` and funnels physical and
compatibility requests through its process-lifetime privacy controller.

Every DSP restart reads the mode-`0600` RAM privacy state and restores required
mute before readiness; failed reconciliation fails startup. External WAMP and
subscribed-state commands wait for the private socket and privacy restoration.
Only internal startup mute bypasses that barrier.

Opening ALSA capture can reconfigure the DSP route: RAM tests obtained an
all-zero stream after mute following capture `hw_params`, while startup mute
did not constrain a later raw root-level open. Capture owners must obey the
privacy-state contract and confirm post-configuration mute before consumption.
See [microphone capture](../microphone-capture.md).

## Evidence and required artifacts

All six held donor copies are byte-identical across StockRoot 11.1842.0,
the flashing bundle, OTA2 12.2134.0 and the unit's 12.2050.3 capture:

| Artifact                    | Bytes   | SHA-256                                                            |
| --------------------------- | ------: | ------------------------------------------------------------------ |
| `usr/bin/dsp-client`        | 715,964 | `a6ce3ff85ff04d9978e3f60acfe1339c561148254e610c7f392f2eb8fe5c72b8` |
| `usr/share/dsp/dsp-img.ldr` | 160,484 | `e76f6ce7c53bb5b508507354fb08523089c136b3731d5ad4f4488a50526a44c8` |

The ARM EABI5 donor exports transport symbols despite stripped debug data.
Disassembly establishes frames, GPIO order, reset, retries and dispatch tables;
one 2026-09-03 physical ioctl capture corroborates image download, boot event
and `getVer`. The capture has 40,121 four-byte image transfers and 23 one-byte
message transfers. The version reply is truncated in that particular log;
the separate service log confirms the full version.

The observed program is host-loaded at each start. Preserve the image as a
runtime input, not just the protocol implementation. This does not establish
DSP part identity, image functionality or the absence of internal ROM/storage.
Pin roles below are inferred from software ordering, not electrical probing.

## Transport

| Property           | Value                                                  |
| ------------------ | ------------------------------------------------------ |
| Device             | `/dev/spidev0.0`, `O_RDWR`                             |
| Mode               | 3: CPOL 1, CPHA 1                                      |
| Word width         | 8 bits                                                 |
| Speed              | 1,000,000 Hz                                           |
| Call               | `SPI_IOC_MESSAGE(1)`, one transfer per ioctl           |
| Chip-select change | `cs_change=0`                                          |
| Image transfer     | 4 TX bytes, null RX, `delay_usecs=0`                   |
| Message transfer   | 1 TX or RX byte, opposite buffer null, `delay_usecs=1` |

`dspopen` writes and reads back mode, word width and maximum speed.
An eight-byte frame costs eight ioctls; image words and message bytes must not
be counted as equivalent frames. Hardware established the message delay of 1,
correcting the earlier static prediction of zero.

## Frame format

Both directions use a five-byte header:

```text
0       message id, high byte
1       message id, low byte
2       payload length, high byte
3       payload length, low byte
4       checksum
5...    payload and padding

wire_length = round_up(payload_length + 5, 4)
checksum = ((id >> 8) + (id & 255) + (len >> 8) + (len & 255)
            + sum(payload)) & 255
```

The donor uses a 614,400-byte staging buffer. `msgwrite` queues id, length,
checksum and a copied payload; `msgproc` builds the header. The receiver
reads at least three bytes after the header, including padding to the same
four-byte boundary, and includes every byte read in its checksum. It rejects
first two bytes `ff ff`, zero length and checksum mismatch.

The first payload byte is the host opcode or device event code, not a separate
header field. Donor `readmsg:` output contains the two id bytes followed by
payload, not a wire frame:

```text
Boot wire frame:  00 01 00 01 06 04 00 00
Donor readmsg:    00 01 04
getVer command:  00 00 00 01 09 08 00 00
```

The physical capture matches both complete frames. The `getVer` reply has
id 0, payload opcode `08` and length 5. Multi-block behavior is code-derived:
the captured donor never transmitted a payload larger than three bytes.

### Retry and re-download

The donor ready-line counter permits six misses, each sleeping 500 ms.
At zero it resets the counter, reloads the full image and returns an error.
Two image runs can therefore indicate a three-second ready stall; normal
occurrence of this retry path was not established by the retained capture.

## Control lines

The donor uses sysfs GPIO `export`, `direction` and `value`:

| GPIO | Direction/use                    | Inferred role    |
| ---- | -------------------------------- | ---------------- |
| 4    | Output, low/high pulse           | DSP strobe       |
| 5    | Exported during download, output | Boot chip select |
| 12   | Input, wait for 0                | Active-low ready |
| 13   | Output around transfers          | Transfer active  |
| 15   | Input before transmission        | Busy             |

The recovered queued-message handshake is:

1. Set GPIO 13 output, value 0; return if the transmit ring is empty.
2. Read GPIO 15; return without transfer if high.
3. Set GPIO 13 to 1, sleep 1 microsecond, build the frame.
4. Sleep 1 microsecond, set GPIO 13 to 0.
5. Poll GPIO 12 until 0.
6. Pulse GPIO 4 low, sleep 1 microsecond, then raise it.
7. Set GPIO 13 to 1, sleep 1 microsecond, perform the transfer.
8. Release the ring slot, sleep 2 microseconds, set GPIO 13 to 0.

The receive path repeats the ready/strobe/transfer sequence without a queued
frame for unsolicited events. The donor message loop sleeps 200 ms when idle.
Its nominal microsecond sleeps measured 8.6-9.4 ms on the old target kernel;
owned handshake and release waits are 10 ms.

### GPIO5 pinmux

After manual image chip-select release, GPIO5 must return to its SPI pin
function. The owned service clears only bit `0x01000000` at `0xF7EA8008`
before each download, sets it afterward and verifies readback. It invokes
BusyBox `devmem` directly, without a shell, and preserves unrelated bits,
including MCU GPIO3.

The causal RAM control was `0x0038D249` -> image download and boot event worked,
but host commands returned `synchronization failed: header=0000000000`;
`0x0138D249` -> `getVer` and microphone commands worked. Restoring the
pre-pinmux image reproduced failure. Timing/affinity experiments did not fix it.

Donor whole-register constants are reference evidence, not replacement values:

| Register     | Donor restored constant | Use                    |
| ------------ | ----------------------- | ---------------------- |
| `0xF7EA8008` | `0x0118D249`            | SPI/GPIO5 pin function |
| `0xF7E80400` | `0x00000A08`            | GPIO data              |
| `0xF7E80404` | `0x00000F28`            | GPIO direction         |

## Reset line

DSP reset is I2C expander address `0x20`, register `0x01`, bit 0, not a SoC
GPIO. The donor reads then sets/clears that bit via `/dev/i2c-0` raw
`I2C_RDWR`. It shares the register with MCU amplifier/DAC controls.
Owned services preserve unrelated bits and lock `/run/reinvoke/expander.lock`
around each update. See [I2C backend](../../tools/dsp-interface/i2c_linux.go).

## Boot image

Donor search order is `/media/usb/dsp-img.ldr`, `/data/test/dsp-img.ldr`, then
`/usr/share/dsp/dsp-img.ldr`, with a 614,400-byte read limit. Loading:

1. Reverse the bits within every source byte.
2. Pulse reset: set, clear, sleep 20 ms, set, sleep 10 ms.
3. Switch GPIO5 pin function, export output, raise then lower chip select.
4. Save speed, force 1 MHz, stream four-byte words with no inter-word delay.
5. At byte offset 1536, restore saved speed and pause 10 ms.
6. Raise chip select, unexport GPIO5 and restore pin function.

All 40,121 captured image transfers were at 1 MHz, so speed restoration was a
no-op on this unit; the pause was not. Concatenating TX words produces exactly
160,484 bytes, SHA-256
`9e3d85f37ac62e191616f558359e7b4ec46ce6499167da991994ea0b944f34f2`,
the byte-bit-reversed held image. There is no extra framing, header or padding.

Offset 1536 is 256 48-bit words. The first stage parses as such words before
changing to block-like data with ASCII. A boot kernel plus payload is an
interpretation of that shape, not an identified DSP family or decoded program.

## Historical donor WAMP surface

The donor connects to `127.0.0.1:9999`, realm `default`: connect, join,
register, `dspopen`, then message loop. Host-to-device encoding:

| `com.harman.` procedure/topic | Message id | Payload        |
| ----------------------------- | ---------- | -------------- |
| `dsp.micTestSingle`           | 2          | `00 <mic>`     |
| `dsp.micTestPair`             | 2          | `01 <pair>`    |
| `dsp.micTestNormal`           | 2          | `02`           |
| `test.dspBypassMode`          | 2          | `03 <mode>`    |
| `dsp.volumeSet`               | 0          | `04 <volume>`  |
| `dsp.getVer`                  | 0          | `08`           |
| `dsp.micMute`                 | 0          | `09 <mute>`    |
| `stateChanged` (subscription) | 0          | `0b <state>`   |
| `dsp.dumpDspMemory`           | 0          | `0c <lo> <hi>` |

Only `getVer` is side-effect free. The owned public DSP surface excludes
`micMute`; the MCU/private-socket split above replaces it.

Device-to-host dispatch:

| Id   | Code | Donor event             | Effect                                |
| ---- | ---- | ----------------------- | ------------------------------------- |
| 0    | `04` | `EVENT_NEW_DAC_GAIN`    | Log                                   |
| 0    | `05` | `EVENT_EXPECT_SPEECH`   | Log                                   |
| 0    | `06` | `EVENT_CANCEL_TRIGGER`  | Log                                   |
| 0    | `07` | `EVENT_SW_UPGRADE`      | Log                                   |
| 0    | `08` | `EVENT_DSP_VERSION`     | Publish `com.harman.dsp.version`      |
| 0    | `09` | `EVENT_MIC_MUTE`        | Log                                   |
| 0    | `0b` | `EVENT_CORTANA_SKYPE`   | Log                                   |
| 0    | `0c` | Memory dump             | Append through `save_dsp_memory_dump` |
| 0    | `ff` | `EVENT_ERR`             | Log                                   |
| 1    | `00` | `EVENT_TRIGGER_FOUND`   | Log                                   |
| 1    | `01` | `EVENT_PAYLOAD_DEGIN`   | Log                                   |
| 1    | `02` | `EVENT_PAYLOAD_END`     | Log                                   |
| 1    | `03` | `EVENT_PAYLOAD_TIMEOUT` | Log                                   |
| 1    | `04` | `EVENT_DSP_BOOTUP`      | Donor calls MCU unmute                |
| 1    | `ff` | `EVENT_WRITE_ERR`       | Log                                   |
| 2    | `00` | `EVENT_MIC_TEST_SINGLE` | Log                                   |
| 2    | `01` | `EVENT_MIC_TEST_PAIR`   | Log                                   |
| 2    | `02` | `EVENT_MIC_NORMAL`      | Log                                   |
| 2    | `03` | `EVENT_HW_PERFORM_TEST` | Log                                   |
| 2    | `ff` | `EVENT_TEST_ERR`        | Log                                   |

Version data after opcode `08` is four bytes, printed `%X.%X.%X.%X` and packed
big-endian into an integer. `00 00 64 58` is `0.0.64.58`, or decimal `25688`.
The donor's `call_mcu_unmute` calls `com.harman.vui.mutedaccontrol` and
`muteampcontrol`; the owned service deliberately does not.

## Capture and comparison

The [offline decoder](../../tools/control/dsp-frame-decode.mjs) interprets
frames or donor `readmsg` tuples without opening devices. For a saved capture:

```bash
node tools/emulation/spi-capture-label.mjs "<capture.log>" \
  --image "<retained-dsp-img.ldr>" --frames
```

The comparator labels image/TX-byte/RX-byte traffic, checks settings and frame
checksums, and reports exact image match, clean truncated prefix or first
mismatch. The retained capture has 8 transmitted and 15 received message
bytes after the image.

An attributable new log requires `INVOKE_IOCTL_MODE=record`, `INVOKE_IOCTL_LOG`
on RAM-backed storage, the pinned image, logging before `dspopen`, one SPI
owner, at least `EVENT_DSP_BOOTUP`, and roughly 20 MB free per download.
Keep the window free of state-changing WAMP calls.

> [!WARNING]
> Record mode forwards real reset/download/unmute operations and adds timing
> overhead. Donor startup is not passive; do not interleave a second SPI
> opener or stop the process mid-transfer. RAM boot avoids intentional NAND
> writes, not hardware side effects.

## Open questions

Unresolved details are DSP identity/image contents, id-0 event codes below 4,
unsolicited header/payload-id correspondence, the binary write that sets
message delay to 1, and normal occurrence of retry/re-download. A boot event
does not establish AEC, beamforming, microphone privacy or acoustic output.
