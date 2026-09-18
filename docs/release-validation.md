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
| mic mute | Mic indicator changes, capture stops |
| mic mute, long | Enters Wi-Fi setup |
| Volume up and down | Arc grows and shrinks, loudness follows |

### Indicators

1. **Ring at boot.** Watch the ring from power-on. Report how many LEDs light
   and when. This is the open question below.
2. **Mic mic mute.** Mute and confirm the indicator matches the capture state.

### Recovery

1. **Reboot.** Issue a reboot over SSH and confirm the unit comes back without
   touching power. The fix for this shipped untested: the old code stopped the
   runtime and then sat in a sleep loop, never asking the kernel to restart.

## Open questions

### Three ring LEDs at boot

Observed by eye at the last boot, unexplained. The volume worker writes the arc
on every apply attempt, including the ones that fail, at the displayed level of
80. An arc at 80 was confirmed on hardware to light most of the ring, so three
LEDs does not match what the code asks for.

Two possibilities, and the evidence does not choose between them: the observed
lights were a different indicator, or the arc is drawn before the level is
known. Resolving it needs the boot watched deliberately rather than recalled.

Worth questioning separately: the donor drew the arc in response to
`com.harman.volumeChanged`, that is, when a listener changed something. This
runtime also draws it at startup, as a side effect of applying the initial
volume, and redraws it on every failed retry. Whether a stock unit lit its ring
at boot is unknown, and adopting donor behaviour would mean not drawing it
until something changes.

### Why the DSP service restarts during boot

On the last boot the DSP interface registered its procedures at 31 seconds,
lost its session, and registered again at 43. The runtime absorbed it and the
volume applied at 46 seconds. Nothing explains the restart. It is benign today
only because the retry outlasts it.

### Why reloading the gadget module panics

Loading `g_android` after removing it panics the kernel. No panic log survives,
so the cause is unknown. It does not affect a normal boot, which loads the
module once.

## What is not covered here

Persistence across power loss, behaviour when Wi-Fi is absent at boot, and the
provisioning window have all been exercised at some point but not since the
current build. They are not listed as verified because that evidence is older
than the code.
