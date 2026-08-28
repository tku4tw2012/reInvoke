# Control Plane Emulation

Results from running the Invoke's own userland binaries on an x86 host under
`qemu-user` ARM emulation, with the device's WAMP router live and answering
calls. No physical unit was involved and no hardware was at risk.

This document records what was executed, what the device software actually
did, and which prior claims it corrected.

## Why this was possible

The Invoke's control plane is not a proprietary protocol. `system-manager`
starts `bonefish`, an open-source C++ WAMP router, and every subsystem
connects to it as an `autobahn-cpp` client. Because both the router and the
clients ship as ordinary ARM Linux executables inside the preserved rootfs,
the entire control plane can be reconstituted off-device.

## Environment

| Component | Value |
|---|---|
| Emulator | `qemu-arm-static` (qemu-user) |
| Isolation | `bwrap` rootless sandbox, no root privileges required |
| Sandbox root | `reinvoke-archive/emulation/sandbox` (a copy, never the evidence tree) |
| Launcher | `reinvoke-archive/emulation/run.sh` |
| Source rootfs | `extracted/phase3/stockroot/rootfs` from `83_IMAGE` |

Services run against a copy so runtime state never mutates preserved material.

Two runtime facts had to be reproduced before any service would stay up:

- `/data` is a symlink to `/lsync/data1`, so `/lsync` must exist and be
  writable or `audio-ui` aborts in `boost::filesystem::create_directory`.
- `qemu-user` does not redirect filesystem syscalls through its `-L` sysroot.
  Absolute paths resolve against the host, which is why a real sandbox rather
  than a library search path is required.

## Verified router behaviour

`bonefish` accepts the documented options `-r realm`, `-t rawsocket-port`,
`-w websocket-port`, `--no-json`, `--no-msgpack`, and `-d`. Started as
`bonefish -r default -t 9999 -w 9998`, it listens on both ports and announces
the `dealer` and `broker` roles in its WELCOME.

Serializer negotiation was probed across the full rawsocket matrix:

| Requested serializer | Result |
|---|---|
| 1 (JSON) | Rejected, error code 1 |
| 2 (MsgPack) | Accepted at every tested max-length |
| 3 | Rejected, error code 1 |

The bus is therefore MsgPack-only over rawsocket in this build. Any future
client must speak MsgPack; a JSON client is refused at handshake.

## Correction to the recovered URI list

An earlier pass reported 130 control URIs extracted from service binaries.
That count was produced with a lowercase-only pattern, which truncated every
camelCase identifier at its first capital letter. The real name
`com.harman.volumeSet` was being recorded as `com.harman.volume`.

Re-extracting with a case-correct pattern yields 165 URIs, and the corrected
forms are confirmed against live router traffic. A probe of the truncated list
against the running bus returned zero registered procedures, which is what
exposed the error. The truncated names were not merely incomplete, they were
names that do not exist.

## Procedures registered by audio-ui

Observed directly in router debug output as `audio-ui` joined the realm:

| Procedure |
|---|
| `com.harman.volumeGet` |
| `com.harman.volumeSet` |
| `com.harman.volumeAdjust` |
| `com.harman.musicMuteSet` |
| `com.harman.musicMuteToggle` |
| `com.harman.stateGet` |
| `com.harman.aui.alertPlay` |
| `com.harman.aui.alertCancel` |
| `com.harman.aui.registerVoiceAgent` |
| `com.harman.aui.demo-action` |
| `com.harman.extStateUpdate` |
| `com.harman.demoIntro` |

It subscribes to `com.harman.test.inputEvent` and
`com.harman.music.stateChanged`, and publishes `com.harman.ready.audio-ui`
followed by a periodic `com.harman.heartbeat.audio-ui` carrying
`{"thread_id": <int>}`.

## Recovered payload shapes

These are observed messages, not inferred schemas.

`com.harman.stateGet` returns no positional arguments and a keyword map of
stream states:

```json
{"alert": {"priority": "5", "state": ""},
 "alert-type": {"priority": "", "state": ""},
 "bluetooth": {"priority": "5", "state": ""},
 "call": {"state": ""},
 "microphone": {"priority": "4", "state": ""},
 "music": {"state": ""},
 "system": {"state": ""},
 "voice": {"priority": "5", "state": ""}}
```

`com.harman.volumeGet` returns per-stream volume and mute:

```json
{"music": {"mute": 0, "volume": 50},
 "system": {"mute": 0, "volume": 70}}
```

`com.harman.stateChanged` is published with one positional argument naming the
changed stream and a keyword map of the full state. `com.harman.volume.setDuck`
was observed published as `["music", "hard"]`, establishing that ducking takes
a stream name and a strength.

## A control call that actually worked

`com.harman.musicMuteToggle` was called with no arguments and returned
`[true, "music"]` with keyword state showing `music.mute` transitioned from
`0` to `1`. A follow-up `com.harman.volumeGet` confirmed the new value.

This is the first end-to-end demonstration that the Invoke's audio control
surface can be driven by a client Harman did not write, using only preserved
software.

`volumeSet`, `volumeAdjust`, and `musicMuteSet` return
`wamp.error.runtime_error` in the current sandbox. The cause is visible in the
service log: `snd_mixer_attach()` fails because the sandbox has no mixer
carrying the expected control names. Mute toggling succeeds because it is
tracked in service state, while volume changes must reach ALSA. This is an
environment limitation, not evidence that the procedures are unusable.

## The final firmware on the bus

The same method was applied to `Barracuda_libre-12.2134.0`, the last build
Harman shipped, recovered from the OTA2 bundle. This is the build in which
Cortana and Spotify are removed and a `wifi-blocker` service is added, so its
control surface is the closest thing to a factory-sanctioned repurposed device.

A second sandbox at `reinvoke-archive/emulation/sandbox-final` runs it via
`run-final.sh`. Its `bonefish`, `audio-ui`, `oobe-ui`, and `bluetooth` services
all join the realm.

Procedures registered by the final build:

| Procedure | Provided by |
|---|---|
| `com.harman.bluetoothPairing` | bluetooth |
| `com.harman.bluetooth.resume` | bluetooth |
| `com.harman.bluetooth.pause` | bluetooth |
| `com.harman.bluetooth.stop` | bluetooth |
| `com.harman.bluetooth.next` | bluetooth |
| `com.harman.bluetooth.prev` | bluetooth |
| `com.harman.bluetooth.skipTo` | bluetooth |
| `com.harman.bluetooth.repeat` | bluetooth |
| `com.harman.bluetooth.shuffle` | bluetooth |
| `com.harman.deviceNameGet` | bluetooth |
| `com.harman.oobe-ui.shutdown` | oobe-ui |
| `com.harman.volumeGet` | audio-ui |
| `com.harman.volumeSet` | audio-ui |
| `com.harman.volumeAdjust` | audio-ui |
| `com.harman.musicMuteSet` | audio-ui |
| `com.harman.musicMuteToggle` | audio-ui |
| `com.harman.stateGet` | audio-ui |
| `com.harman.aui.alertPlay` | audio-ui |
| `com.harman.aui.alertCancel` | audio-ui |
| `com.harman.aui.registerVoiceAgent` | audio-ui |
| `com.harman.aui.demo-action` | audio-ui |
| `com.harman.extStateUpdate` | audio-ui |
| `com.harman.demoIntro` | audio-ui |

That set is a complete media-player control surface: pairing, transport,
track selection, repeat and shuffle, volume, mute, and shutdown. It is the
practical API for treating an Invoke as a controllable Bluetooth speaker.

One vestige is worth recording. The `bluetooth` service still subscribes to
`com.cortana.device.nameChanged` even though every Cortana binary was removed,
so the device-name path was never fully decoupled from the assistant.

## Results by call

Tested against the final firmware with a third-party MsgPack client.

| Call | Result |
|---|---|
| `com.harman.stateGet` | Returns full stream state |
| `com.harman.volumeGet` | Returns per-stream volume and mute |
| `com.harman.musicMuteToggle` | Succeeds and mutates state |
| `com.harman.volumeSet` | `wamp.error.runtime_error` |
| `com.harman.bluetooth.*` | No reply within timeout |
| `com.harman.deviceNameGet` | No reply within timeout |

The failures are environmental and each has an identified cause. Volume
setters need an ALSA mixer exposing the per-stream control names, which the
sandbox does not provide. The Bluetooth procedures block because BlueZ has no
HCI adapter to talk to. Neither result indicates a protocol problem, and
neither should be read as evidence that the calls fail on real hardware.

Restoring those two dependencies, a mixer with matching control names and a
Bluetooth adapter, is the obvious next increment.

## Audio topology

`etc/asound.conf` loads `etc/asound-product.conf`, which describes the real
signal path.

| Element | Value |
|---|---|
| DSP device | `hw` card 1, 48000 Hz, 2 channels, `S32_LE` |
| Default playback | `hw:Loopback,0,5` (ALSA loopback) |
| Default capture | `mic` |
| Mixing | `dmix` instances per stream, all slaving to `dsp` |
| Microphone | `dsp_dsnoop` then `softvol` named `mic` |

Each stream is a `softvol` plugin whose control name matches the stream and
whose control card is 0:

| Stream | Control name | Slave |
|---|---|---|
| `system` | `system` | `volmix_system` |
| `music` | `music` | `volmix_music` |
| `timer` | `timer` | `volmix_timer` |
| `call` | `call` | `volmix_call` |
| `voice` | `voice` | `mbeq` |
| `alarm` | inherits `timer` | `timer` |

The `voice` stream routes through `mbeq`, a LADSPA equalizer present in the
rootfs as `usr/lib/ladspa/mbeq_1197.so`. The DSP carries its own loadable
image at `usr/share/dsp/dsp-img.ldr`.

## Build provenance

Service logs leak the original build path:

```text
/usr/src/debug/audio-ui/1.0-r0/jenkins_slave/workspace/
PodiumCustomerRelease-Microsoft/podium/source/audio-ui/alsa_volume.c
```

This confirms Podium as the platform name, names the Microsoft customer
release branch, and gives source file names with line numbers for the audio
volume implementation.

## What this establishes and what it does not

Established by execution:

- The control plane is reproducible off-device with no hardware access.
- The transport is WAMP over MsgPack rawsocket on port 9999.
- The audio control surface responds to third-party calls and changes state.
- The full ALSA routing, including DSP format and per-stream controls, is
  documented in preserved configuration.

Not established:

- Whether the same calls drive real speakers on real hardware. Emulation
  exercises the software path only.
- The argument shapes of `volumeSet` and `volumeAdjust`, which need a working
  mixer before they will accept input.
- Anything about the daughterboard connector, electrical behaviour, or the
  MCU's physical I2C wiring. Those remain measurements, not inferences.

## Reproducing this

```bash
EMU=reinvoke-archive/emulation
$EMU/run.sh /usr/bin/bonefish -r default -t 9999 -w 9998 -d &
$EMU/run.sh /usr/bin/audio-ui 127.0.0.1 9999 normal normal &
```

Then connect a MsgPack WAMP client to `127.0.0.1:9999`, realm `default`.
