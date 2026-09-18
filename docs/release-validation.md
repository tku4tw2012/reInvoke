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

Taken from `/run/reinvoke/logs/runtime.log` on the running unit, not from
intent.

| Capability | Status | How it was observed |
| --- | --- | --- |
| Boots from NAND unattended | verified | Eight services up, no host attached |
| Wi-Fi associates and gets a lease | verified | SSH reachable at a DHCP address |
| SSH administration | verified | Every check in this table arrived over it |
| USB ADB from cold boot | verified | `adb shell` with no network configured |
| DSP accepts a volume | verified | `completed com.harman.dsp.volumeSet` |
| Volume arc on the ring | verified | Four distinct arcs seen at 10, 50, 80, 100 |
| Front lamp state changes | verified | Amber at boot, white once online |
| Bluetooth pairing and playback | verified | Phone paired, audio heard |
| Microphone capture | verified | Recorded audio reviewed |
| Startup chime audible | **failed** | Logged as played, heard by nobody |
| Reboot without a power cycle | **unverified** | Fix written, never exercised |
| Three ring LEDs seen at boot | **unexplained** | See open questions |

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
