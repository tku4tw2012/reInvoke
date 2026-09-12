---
title: reInvoke control tools
description: WAMP diagnostics, offline DSP decoding and gated Bluetooth helper builds
ms.date: 2026-09-12
ms.topic: how-to
---

Host diagnostics and contract references live here; the target runs static
ARM MCU/DSP services, not Node.js. Commands use the repository root.
Builds require retained donor inputs, toolchains, sysroot and D-Bus archives,
not supplied by a public clone. Use fresh private outputs.

## Query and monitor WAMP

`wamp-call.mjs` is a dependency-free MessagePack RawSocket client:

```bash
node tools/control/wamp-call.mjs com.harman.vui.getmcustatus \
  --host 192.0.2.10 --port 9999
node tools/control/wamp-monitor.mjs --host 192.0.2.10 --port 9999 --duration 60
```

| Call option     | Default               |
| --------------- | --------------------- |
| `--host HOST`   | `127.0.0.1`           |
| `--port PORT`   | `19999`               |
| `--realm REALM` | `default`             |
| `--args JSON`   | Positional array `[]` |
| `--kwargs JSON` | Keyword object `{}`   |
| `--timeout MS`  | `8000`                |
| `--help`        | Usage                 |

Host port 19999 is a forwarding convention. For an already available RAM
target, `adb -s "$REINVOKE_ADB_SERIAL" forward tcp:19999 tcp:9999` establishes
it. Native RawSocket uses 9999 directly. The client does not implement
WebSocket, even though candidate 02 independently passed a `wamp.2.msgpack`
handshake on 9998. No native USB/ADB or shell is implied.

The monitor sends only HELLO/SUBSCRIBE, defaults to six MCU topics and emits
JSON Lines. It opens no hardware or upgrade path; subscription confirmations
are not physical events. See [MCU contracts](../../docs/emulation/mcu-boundary.md).

> [!WARNING]
> WAMP is unauthenticated; restrict it to trusted peers. Setters change device
> state. USB ADB is a root diagnostic channel in the reviewed RAM workflow.

## Host reference services

Run these only on an isolated router: registrations conflict with target
services. The fixed service returns one configured response; the speaker
reference exposes the recovered audio/source subset:

```bash
node tools/control/wamp-fixed-service.mjs com.example.identity \
  --kwargs '{"product":"Invoke"}'
node tools/control/speaker-control-service.mjs --music-volume 20 --bluetooth-active
```

`speaker-control-state.mjs` is the side-effect-free core.
`speaker-control-backend.mjs` adds an injectable BlueALSA 4 CLI adapter with
explicit PCM path, source/transport observers, stereo volume/mute and polling.
It never guesses a live object path. The
[speaker boundary](../../docs/emulation/owned-speaker-control.md) distinguishes
this reference from the MCU-owned target bridge.

## Offline DSP decoding

```bash
node tools/control/dsp-frame-decode.mjs --log "<services.log>"
```

Other modes are `--readmsg <bytes>` for donor id/payload tuples,
`--device <bytes>` for full device wire frames,
`--command <procedure> [arguments]` for encoding without transmission, and
`--list` for vocabulary. None opens a device. The
[SPI comparator](../emulation/spi-capture-label.mjs) reuses the decoder for
byte-exact capture/image comparison. Frame details and side effects are in the
[DSP reference](../../docs/emulation/dsp-boundary.md).

## Build Bluetooth helpers

The HCI initializer, pairing agent and media-control helper are static ARM
binaries. Build from pinned inputs:

```bash
tools/control/build-hci-init.sh \
  --bluez-archive "<source-dir>/bluez-5.55.tar.xz" \
  --sysroot "<armhf-sysroot>" --output "<artifact-dir>/hci-init"
tools/control/build-bluez-pairing-agent.sh \
  --dbus-source "<built-dbus-1.12.20>" \
  --sysroot "<armhf-sysroot>" --output "<artifact-dir>/bluez-pairing-agent"
tools/control/build-bluez-media-control.sh \
  --dbus-source "<built-dbus-1.12.20>" \
  --sysroot "<armhf-sysroot>" --output "<artifact-dir>/bluez-media-control"
```

The HCI helper resets the controller and removes volatile keys.
`--unpair ADDRESS` performs standalone MGMT unpair without `--reset`.
The pairing agent accepts only one configured peer and A2DP/AVRCP UUIDs.
`SIGUSR2` toggles/cancels its window; `SIGUSR1` reopens it.
Its optional fourth positional argument is the state path, default
`/run/reinvoke/bluetooth-state`; it atomically publishes `pairing`, `connected`
or `off`. The MCU maps that to the rear indicator.
The media helper sends Play/Pause on the connected peer's BlueZ
`MediaControl1` for the physical Action key.

Both D-Bus helpers require target static `libdbus-1.a` and generated headers,
not just a source archive. The pairing builder gates compiler internals,
assembler/linker/strip, D-Bus archive/header manifest and the entire resolved
ARM sysroot. An arbitrary distro sysroot is not interchangeable.
Retained inputs are catalogued under private
`toolchains/armhf-sysroot-gcc11/`, `toolchains/armhf-gcc11-debs/` and
`toolchains/dbus-1.12.20-armhf-static/`.

The retained D-Bus build used `--host=arm-linux-gnueabihf --enable-static
--disable-shared --disable-selinux --disable-apparmor --disable-systemd
--disable-tests --disable-doxygen-docs --disable-xml-docs --without-x
--disable-launchd --disable-libaudit ac_cv_have_abstract_sockets=yes`, then
`make -C dbus libdbus-1.la`. Those flags are provenance, not a guarantee of
reproducing the gated archive on another host. The static link warns about
`getpwuid_r`/`getaddrinfo`; the tested helper path uses a private Unix socket
without user/hostname resolution.

## Build patched BlueALSA

```bash
tools/control/build-bluealsa-aplay.sh \
  --source-archive "<source-dir>/bluez-alsa-4.0.0.tar.gz" \
  --sysroot "<armhf-sysroot>" \
  --output "<artifact-dir>/bluealsa-aplay" \
  --daemon-output "<artifact-dir>/bluealsa"
```

The builder gates source, all six patches, compiler, strip tool and both
binaries. Patches cover active-PCM lease, Invoke ALSA recovery, decoded jitter
buffer, SBC RTP-gap concealment, short-clip drain and closed-FIFO drain.
The player buffers two seconds of decoded PCM, drains complete ALSA periods,
recovers partial writes/underruns, and emits the worker-thread lease after
positive PCM. It removes the lease after 100 ms inactivity once buffered data
drains. Timestamp-confirmed RTP gaps receive bounded silence.

## Historical Bluedroid probe

`a2dp-data-probe.c` reads abstract socket `.a2dp_data`. Optional `--start`
performs CHECK_READY/START and is not read-only:

```bash
arm-linux-gnueabihf-gcc -static -O2 -Wall -Wextra -Werror \
  tools/control/a2dp-data-probe.c -o "<artifact-dir>/a2dp-data-probe-armhf"
```

Historical phone/Linux trials connected but received zero decoded bytes;
CHECK_READY returned failure `1`. This is not a working media bridge.

## Offline tests

```bash
node --test tools/control/speaker-control-state.test.mjs \
  tools/control/speaker-control-backend.test.mjs \
  tools/control/speaker-control-service.test.mjs \
  tools/control/dsp-frame-decode.test.mjs
cc -std=c11 -O2 -Wall -Wextra -Werror \
  tools/control/bluez-pairing-policy_test.c -o "<artifact-dir>/pairing-policy-test"
"<artifact-dir>/pairing-policy-test"
```

Build/offline results do not establish native functionality. Candidate-specific
WAMP, Bluetooth and authentication results are in the
[native guide](../../docs/native-nand-platform.md).
