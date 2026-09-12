---
title: Control-plane emulation
description: Recovered donor WAMP contracts, stock reachability and isolated emulation
ms.date: 2026-09-12
ms.topic: reference
---

Preserved ARM services run under `qemu-user` with synthetic ALSA/I2C responses.
The results below establish software contracts, not physical audio. reInvoke
retains Bonefish as a narrow compatibility router and replaces donor policy
services; see the [current contract](../current-product-contract.md) and
[owned speaker boundary](owned-speaker-control.md).

## Router and emulation boundary

`bonefish -r default -t 9999 -w 9998` exposes dealer/broker roles on RawSocket
9999 and WebSocket 9998. Options also include `--no-json`, `--no-msgpack` and
`-d`. The held RawSocket build accepts MessagePack serializer 2 and rejects
JSON serializer 1 and serializer 3 with error code 1. That experiment did not
test WebSocket; candidate 02's later native WebSocket evidence is separate.

URI names are case-sensitive. The corrected inventory has 165 names:
`com.harman.volumeSet` is real; the previously truncated `com.harman.volume`
is not. A binary string proves vocabulary, not registration or successful
execution.

The guest shim intercepts unsupported ALSA control ioctls before qemu returns
`ENOSYS` (`SNDRV_CTL_IOCTL_CARD_INFO` is `0x81785501`). It supplies card,
element-list/info/read/write and event subscription operations plus raw
`I2C_RDWR`. Its ARM EABI5 hard-float build requires only `GLIBC_2.4`,
compatible with donor glibc 2.23. A host loopback card cannot fix qemu's missing
ioctl implementation. Bluetooth still needs HCI/rfkill, not synthetic ALSA.

## Audio and source contracts

Observed final-firmware `audio-ui` registrations, all under `com.harman.`:

| Procedures                                                                                   | Contract                                                                                  |
| -------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| `volumeGet`, `stateGet`                                                                      | No positional input; full state in result kwargs                                          |
| `volumeSet`                                                                                  | `[value, "music"]`; clamp to 0-100; result `[effective, "music"]` plus full volume kwargs |
| `volumeAdjust`                                                                               | `[delta, "music"]`; clamp result; same result shape                                       |
| `musicMuteSet`                                                                               | `[boolean, "music"]`; result `[boolean, "music"]` plus full volume kwargs                 |
| `musicMuteToggle`                                                                            | No arguments; same result shape as `musicMuteSet`                                         |
| `extStateUpdate`                                                                             | `["bluetooth"]` with `{"state":"playing"}` kwargs; empty result                           |
| `aui.alertPlay`, `aui.alertCancel`, `aui.registerVoiceAgent`, `aui.demo-action`, `demoIntro` | Registered; not required for owned Bluetooth operation                                    |

Volume/mute calls succeeded under emulation: setting 30, adjusting by 5 and
muting returned `[30,"music"]`, `[35,"music"]` and `[true,"music"]`; follow-up
queries confirmed each change. `volumeGet` returns a map such as:

```json
{"music":{"mute":0,"volume":50},"system":{"mute":0,"volume":70}}
```

`stateGet` returns no positional arguments and this keyword-map shape:

```json
{"alert":{"priority":"5","state":""},
 "alert-type":{"priority":"","state":""},
 "bluetooth":{"priority":"5","state":""},
 "call":{"state":""},
 "microphone":{"priority":"4","state":""},
 "music":{"state":""},
 "system":{"state":""},
 "voice":{"priority":"5","state":""}}
```

`stateChanged` publishes `[stream]` plus the full stream-map kwargs.
`volumeChanged` publishes `["music", value]`. Muting stored volume 35 publishes
`volumeChanged ["music",0]` then `musicMuteChanged [true]`; stored volume stays
35. Observed `volume.setDuck ["music","hard"]` supplies stream and strength.
`audio-ui` subscribes to `test.inputEvent` and `music.stateChanged`, publishes
`ready.audio-ui`, and emits `heartbeat.audio-ui` with `{"thread_id":<int>}`.

Historical `music-source-manager` registrations, also under `com.harman.`:

| Group                   | Procedures                                                                                               |
| ----------------------- | -------------------------------------------------------------------------------------------------------- |
| Transport forwarding    | `music.{next,pause,prev,repeat,resume,shuffle,skipto,stop}`                                              |
| Source registry/routing | `source.{flush,get-active,get-registered,nowPlayingUpdate,register,start,trackPositionUpdate,volumeSet}` |
| Lifecycle               | `music-source-manager.shutdown`                                                                          |

In isolated execution, `source.register ["com.harman.bluetooth"]` and
`source.start ["com.harman.bluetooth"]` returned no args.
`source.get-registered []` returned `["com.harman.bluetooth"]`;
`source.get-active []` changed from `[""]` to `["com.harman.bluetooth"]`.
The service subscribes to `volumeChanged` and publishes
`ready.music-source-manager` and `heartbeat.music-source-manager`.
BlueZ/BlueALSA do not require this registry or donor process.

## Vendor control surface

The final `Barracuda_libre-12.2134.0` rootfs removes Cortana/Spotify and adds
`wifi-blocker`; the historical unit rootfs capture was `12.2050.3`.
Static configuration and emulated registrations establish the following
stock surface, not a supported reInvoke administration API:

| Service     | Additional `com.harman.` registrations                                                               |
| ----------- | ---------------------------------------------------------------------------------------------------- |
| `bluetooth` | `bluetoothPairing`, `bluetooth.{resume,pause,stop,next,prev,skipTo,repeat,shuffle}`, `deviceNameGet` |
| `oobe-ui`   | `oobe-ui.shutdown`                                                                                   |

These Bluetooth calls registered but timed out without HCI and a paired
session. `bluetooth` still subscribes to `com.cortana.device.nameChanged`.
See [Bluetooth stack](bluetooth-stack.md) for the unresolved donor decoder.

Bonefish binds all interfaces, but `usr/sbin/firewall.sh` drops TCP 9998,
9999 and 22. An earlier branch accepts them when `/usr/bin/dctflag nofw`
returns nonempty output. `etc/podium-env` also reads `dctflag exdata`.
The executable formats `dct_%s=%s`, reads the `wlan0` hardware address and
references `/factory_setting/%d.dct`. The YAFFS2 partition is not mounted
read-only; DCT format, integrity checks and secure-boot relationship remain
unknown. Editing it would be a persistent flash change, not a transient
firewall adjustment. No working stock debug-gate modification was established.

The firewall permits mDNS, `bootps`, UDP 48301, HTTPS, TCP 12345 and ICMP echo.
`sshd` starts as Dropbear but is externally blocked. `init.rc` configures USB
product `0d02`, name `MRVL USB SDK`, function `adb`, and enable `1`; its
`adbd` service is `disabled` and `#start adbd` remains commented. Gadget
configuration alone proves neither enumeration nor a usable daemon.

`wifi-blocker`'s name does not establish radio shutdown or total network
isolation. `serviceport.sh` configures `eth0` from `/data/service-ip.txt`,
defaulting to vendor address `172.20.20.20`, and announces gratuitous ARP.
An actual `eth0` on Invoke is unresolved; regulatory photographs show no
external Ethernet jack. Bluetooth is the evidence-supported stock user-facing
path. Later [closed-unit recovery](../uboot-access.md) provided local execution
without validating the DCT branch or proving every USB stage is BootROM.

## Donor ALSA configuration

`etc/asound.conf` includes `asound-product.conf`. Its DSP sink is hardware
card 1, 48 kHz, stereo `S32_LE`; default playback is `hw:Loopback,0,5`,
default capture is `mic`. Capture uses `dsp_dsnoop` then `softvol mic`.
Per-stream `dmix` instances slave to `dsp`; card-0 softvol controls are
`system`, `music`, `timer`, `call`, `voice` and `mic`. `alarm` inherits
`timer`; voice routes through LADSPA `mbeq_1197.so`.
This is donor configuration, not the current BlueALSA route or board wiring.

## Isolated reproduction

Requires an existing writable private
`${REINVOKE_ARCHIVE}/emulation/sandbox-final` rootfs copy, `qemu-arm-static`,
`bwrap` and the ARM compiler. `/data -> /lsync/data1` needs writable `/lsync`.
Qemu's `-L` does not redirect absolute filesystem paths, so use the sandbox.

> [!WARNING]
> Keep the network namespace loopback-only. The launcher rejects other
> interfaces and uses synthetic `/dev/null` placeholders; never expose a
> host I2C device. Bonefish is unauthenticated.

From the repository root, compile the shim, enter the namespace, then launch:

```bash
arm-linux-gnueabihf-gcc -shared -fPIC -O2 -Wall -Wextra -Werror \
  -o "${REINVOKE_ARCHIVE}/emulation/invoke-ioctl-shim.so" \
  tools/emulation/invoke-ioctl-shim.c
unshare --user --map-root-user --net
ip link set lo up
tools/emulation/run-final-shim.sh /usr/bin/bonefish -r default -t 9999 -w 9998 -d &
tools/emulation/run-final-shim.sh /usr/bin/audio-ui 127.0.0.1 9999 &
```

Run the [MessagePack client](../../tools/control/README.md) in that namespace
against `127.0.0.1:9999`, realm `default`. The public clone supplies the shim
and launcher, not the sandbox or vendor binaries.
