# Release validation

## Why this list exists

The boot chime shipped in a state where every signal said it worked. The
renderer exited zero, the log said `CUE_PLAYED Power_On`, the amplifier and DAC
were both unmuted, and the samples were scaled to the level chosen by ear.
Nothing was audible. The DSP sits between the DAC and the speaker and had not
yet accepted a volume, so the whole path was open into a gain nobody had set.

No test in the tree was wrong. The tests covered what the code did, and the
code did it. The gap was that nothing observed the speaker.

So this list separates two kinds of check, and the distinction is the point:

- **Log-verifiable** means a state change is recorded that could not appear if
  the feature were broken. `CUE_PLAYED` does not qualify: it only proves a
  process exited. `completed com.harman.dsp.volumeSet` does qualify, because
  the DSP acknowledged a transaction.
- **Needs a person** means the only honest observer is a human ear, eye, or
  hand. No amount of logging substitutes.

An item is not validated because it has a test. It is validated when something
outside the code changed state and was observed.

## What the current build has actually demonstrated

Taken from observation on the running unit, not from intent. Candidate
05.8.11, 2026-09-18.

| Capability | Status | How it was observed |
| --- | --- | --- |
| Boots from NAND unattended | verified | 34 service pid files, no host attached |
| Wi-Fi associates and gets a lease | verified | `FRONT_INDICATOR online` and SSH reachable |
| USB ADB from cold boot | verified | `adb shell` with no network configured |
| USB identity is this unit | verified | host reads `aabbccddeeff` / `reInvoke_AABBCC` / `Harman Kardon` |
| DSP accepts a volume | verified | `completed com.harman.dsp.volumeSet` |
| Startup chime audible | verified | heard by the owner on the 05.8.11 boot |
| No volume arc at boot | verified | no `RING_ARC` logged, and none seen |
| Reboot without a power cycle | verified | `adb reboot` returned the unit repeatedly |
| Front lamp state changes | verified | amber at boot, white once online |
| Bluetooth pairing from the host | verified | `LinkKey` written, survived a reboot |
| Microphone capture | stale | last exercised in the RAM era, not on this build |
| Bluetooth audio playback | **not tested** | pairing is not playback; no stream was started |
| Music at the shipped volume | **not tested** | no stream has ever been played on a NAND build |
| No pops during startup | **failed** | heard on 05.8.11; fix written, not yet flashed |

This table was wrong for several releases: it recorded eight services when the
build ran thirty-four, the chime as failed after it had played, the reboot fix
as never exercised after it had been, and the ring count as unexplained after
it was explained. It is now generated from the same conditions that
[`tools/release-criteria`](../tools/release-criteria/criteria.json) checks, so
the two cannot drift apart silently.

## Checks that need a person

Batched so they can be done in one sitting at the speaker. Each one names what
would count as a pass, because "seems fine" is how the chime passed.

### Audio

1. **Startup chime.** Power cycle. A chime should sound a few seconds after the
   front lamp turns white, not before. Pass is hearing it at a level that does
   not startle. This is the fix that has never been confirmed by ear.
2. **Bluetooth playback level.** Play music and confirm it is neither faint nor
   alarming at the volume shown. The same DSP gap that silenced the chime also
   meant the DSP never received a level, so playback loudness has only been
   judged on boots where the retry happened to land.
3. **Volume actually tracks the dial.** Turn from low to high while music
   plays. Pass is a smooth change in loudness, not a jump at one end.

### Controls

Seven donor events exist. Press each and report what happens, including
nothing:

| Control | Expected |
| --- | --- |
| Action button, short | Pause or resume playback |
| Action button, long | Nothing, or a voice prompt |
| Bluetooth button | Enters pairing |
| Mic-Mute button | Mic indicator changes, capture stops |
| Mic-Mute button, long | Enters Wi-Fi setup |
| Volume up and down | Arc grows and shrinks, loudness follows |

### Indicators

1. **Ring at boot.** Watch the ring from power-on. It should not draw the
   volume arc at all now; only a boot animation. Any arc means the change
   check regressed.
2. **Mic mute.** Mute and confirm the indicator matches the capture state.

### Recovery

1. **Reboot.** Issue a reboot over SSH and confirm the unit comes back without
   touching power. The fix for this shipped untested: the old code stopped the
   runtime and then sat in a sleep loop, never asking the kernel to restart.

## Open questions

### Ring illuminations at boot (resolved)

The ring lights once per volume apply attempt. `ShowVolume` writes MCU opcode
`0x03`, and it was called on every pass of the apply loop, before the error was
checked, so attempts that never reached the DSP drew it too. Since the DSP
registers its procedures seconds after this service starts and the loop retries
every 5 seconds, the number of illuminations at boot was a readout of how slow
the DSP had been that time.

Confirmed on hardware: four volume applies produced four illuminations, one
each.

The donor never had this. Its `mcu-interface` subscribed to
`com.harman.volumeChanged`, published by `audio-ui`, and its handler
(`Receive volume change notify event: %d!`) range-checked the level and wrote
`0x03`. It did not apply volume itself, so it had no retries and no failures to
draw. The runtime now matches that: the ring is written when the level changes
and not when it is merely asserted or retried, so a normal boot draws none.

### Bluetooth pairing, tested host-to-speaker

Tested from the Ubuntu host rather than a phone, because the host is on both
ends of this project and a phone is not.

Established on 05.8.11:

| Fact | Evidence |
| --- | --- |
| Bluedroid stores bonds in `/home/galois_rwdata/misc/bluedroid/bt_config.conf` | bind mount of mtdblock11, written during the boot under test |
| The speaker advertises `Advanced Audio Sink` and AVRCP **only while the pairing window is open** | `sdptool browse` returned GATT alone with the window shut, and the audio services with it open |
| Pairing inside the window stores a real bond | `bt_config.conf` grew 778 to 1219 bytes with a `[the-paired-host]` section carrying `LinkKey` |
| The bond survives a reboot | 1218 bytes and the same section after `adb reboot` |

An earlier revision of this section said pairing was not persisted at all.
That was wrong twice over: it read `/persist/data1/misc/bluedroid`, which is
not the path Bluedroid uses, and the one pairing attempt behind it was made
after the window had already closed, which is why it failed with
`br-connection-unknown`. The correction is recorded rather than quietly
replaced because this is the same failure the audit exists to catch: a claim
that was checked carelessly and then written down as fact.

Still unestablished:

* **Reconnection after a reboot.** The speaker kept its bond; the host did not
  keep its own, so there was nothing to reconnect from. Whether that is a
  BlueZ-side artefact of pairing over LE (`bluetoothctl` resolved only the
  `0x1800`/`0x1801` GATT services) or a real defect is unknown.
* **Whether audio flows.** No stream was ever started. Pairing is not playback.

The persistence service is a separate matter and the earlier note about it
stands: it saves and restores BlueZ-format bonds from `/usr/var/lib/bluetooth`,
which is empty, because Bluedroid does not use that path. That code is
vestigial, not load-bearing, and Bluetooth pairing persists without it.

### Donor claims in code comments that cite no evidence

A comment saying "the donor does X" is load-bearing when the code does X
because of it. One such claim was wrong: `controller.go` said the donor
"does not keep them muted" after initialisation, which justified opening the
amplifier there. Disassembly showed the donor's initialisation contains no
unmute at all, and the opening was audible as pops.

That claim was checkable only because someone went and checked. Others in the
same form are not cited and have not been re-verified:

| Claim | Where | Load-bearing |
| --- | --- | --- |
| Configure writes then reads back each parameter, as the donor does | `tools/dsp-interface/spi_linux.go` | yes, it justifies the readback |
| The strobe sequence is the last thing the donor does before every transfer | `tools/dsp-interface/link.go` | yes, it justifies the ordering |
| The donor never queues a payload longer than three bytes | `tools/dsp-interface/frame.go` | no, `maxPayloadBytes` bounds it regardless |

The method that settled the mute question works here too: find the log string
or symbol in the donor binary, locate the code that references it, and read
what it does. Until that is done these are inherited assumptions, not
established facts, and the DSP link is the part of this runtime with the least
independent verification behind it.

### Why the DSP service restarts during boot

On one boot the DSP interface registered its procedures at 31 seconds, lost its
session, and registered again at 43. The runtime absorbed it and the volume
applied at 46 seconds. Nothing explains the restart.

It is intermittent: on the boot of 18 September the service registered its
seven procedures once, at 31 seconds, with no session loss, and the startup
chime was audible. That is the same boot that produced a single ring
illumination. This restart is the variable behind both symptoms, and the
runtime now tolerates it rather than depending on it not happening: the chime
waits for the DSP to accept a level, and the ring follows changes only.

### Why reloading the gadget module panics

Loading `g_android` after removing it panics the kernel. No panic log survives,
so the cause is unknown. It does not affect a normal boot, which loads the
module once.

## What is not covered here

Persistence across power loss, behaviour when Wi-Fi is absent at boot, and the
provisioning window have all been exercised at some point but not since the
current build. They are not listed as verified because that evidence is older
than the code.
