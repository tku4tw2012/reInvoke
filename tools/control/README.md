---
title: reInvoke control tools
description: Host-side tools for calling the Invoke native control plane
ms.date: 2026-09-04
ms.topic: how-to
---

`wamp-call.mjs` is a dependency-free MsgPack WAMP rawsocket client. It speaks
directly to Bonefish and does not require Harman's test client, Python, or an
external JavaScript package.

`wamp-fixed-service.mjs` registers one procedure and returns a fixed JSON
response. Use it for narrow RAM-only compatibility contracts instead of
starting a broad donor supervisor:

```bash
node tools/control/wamp-fixed-service.mjs com.example.identity \
  --kwargs '{"product":"Invoke"}'
```

`speaker-control-state.mjs` is the side-effect-free state core for the proposed
owned Bluetooth speaker bridge. `speaker-control-service.mjs` exposes that core
as one dependency-free MsgPack WAMP service, replacing the relevant
`music-source-manager` and `audio-ui` registrations without starting either
donor binary:

```bash
node tools/control/speaker-control-service.mjs \
  --music-volume 20 \
  --bluetooth-active
```

The service is independently runnable and testable.
`speaker-control-backend.mjs` adds an injectable BlueALSA 4 CLI adapter for an
explicit PCM path, stereo volume and mute, BlueZ source and transport
observations, polling, and event projection. The target mapping remains
explicit so the service never guesses a live object path. Run the state,
backend, and WAMP protocol tests with:

```bash
node --test \
  tools/control/speaker-control-state.test.mjs \
  tools/control/speaker-control-backend.test.mjs \
  tools/control/speaker-control-service.test.mjs
```

The WAMP inventory, minimum service boundary, and remaining live target mapping
work are documented in
[Owned Bluetooth speaker control boundary](../../docs/emulation/owned-speaker-control.md).

`wamp-monitor.mjs` is passive MCU instrumentation. It sends only WAMP
`HELLO` and `SUBSCRIBE` messages, never `CALL` or `PUBLISH`, and defaults to
the known MCU liveness, status, upgrade-result, key, and rotary-input topics:

```bash
node tools/control/wamp-monitor.mjs --duration 60
```

Use it through the ADB-forwarded port below. Subscribing changes only volatile
router session state; it does not open I2C, GPIO, `/dev/mem`, MTD, or the MCU
upgrade procedures.

`dsp-frame-decode.mjs` is an offline decoder for the donor `dsp-client` SPI
frame format. It opens no device node and sends nothing; its `--command` mode
prints the bytes a procedure would put on the wire without emitting them.
Decode a captured service log, or one frame, or a command encoding:

```bash
node tools/control/dsp-frame-decode.mjs --log services.log
node tools/control/dsp-frame-decode.mjs --readmsg 0x00 0x01 0x04
node tools/control/dsp-frame-decode.mjs --device 00 01 00 01 06 04 00 00
node tools/control/dsp-frame-decode.mjs --command com.harman.dsp.volumeSet 30
node tools/control/dsp-frame-decode.mjs --list
```

`--readmsg` takes the tuple `dsp-client` prints, which is the header id
followed by the payload. `--device` takes the raw wire frame, which carries
the same five-byte header as the host direction. Decoding the donor's existing
log is fully passive instrumentation of the live DSP link. Of the eight
registered procedures only `com.harman.dsp.getVer` is side-effect free;
`com.harman.stateChanged` is a subscribed topic rather than a registration.
Run the decoder tests with:

```bash
node --test tools/control/dsp-frame-decode.test.mjs
```

The frame format, GPIO handshake, reset path, and replacement contract are
documented in [DSP boundary](../../docs/emulation/dsp-boundary.md).
`tools/emulation/spi-capture-label.mjs` reuses this module to label byte-exact
`SPI_IOC_MESSAGE` captures and diff them against `dsp-img.ldr`.

`a2dp-data-probe.c` is a read-only diagnostic client for Bluedroid's abstract
`.a2dp_data` socket. Its optional `--start` mode performs the standard
`CHECK_READY` and `START` control handshake before reading. Build it for the
Invoke without linking an unreviewed binary:

```bash
arm-linux-gnueabihf-gcc -static -O2 -Wall -Wextra -Werror \
  tools/control/a2dp-data-probe.c \
  -o ../reinvoke-archive/build/tools/a2dp-data-probe-armhf
```

The controlled iPhone and Ubuntu tests connected successfully but received zero
decoded bytes. `CHECK_READY` returned failure acknowledgement `1`; this is an
evidence probe, not a working media bridge.

`hci-init.c` and `bluez-pairing-agent.c` are the owned control components for
the RAM-only BlueZ replacement. No prebuilt copy is committed. Build both as
static ARM binaries against the pinned BlueZ and D-Bus build trees:

```bash
arm-linux-gnueabihf-gcc -std=c11 -O2 -Wall -Wextra -Werror -static \
  -Ipath/to/bluez-5.55 \
  -Ipath/to/armhf-sysroot/usr/include \
  tools/control/hci-init.c \
  path/to/bluez-5.55/lib/.libs/libbluetooth-internal.a \
  -o path/to/hci-init

arm-linux-gnueabihf-gcc -std=c11 -O2 -Wall -Wextra -Werror -static \
  -Ipath/to/dbus-1.12.20 \
  -Ipath/to/dbus-1.12.20/dbus \
  -Ipath/to/armhf-sysroot/usr/include \
  tools/control/bluez-pairing-agent.c \
  path/to/dbus-1.12.20/dbus/.libs/libdbus-1.a \
  -lpthread \
  -o path/to/bluez-pairing-agent
```

The pairing agent limits BlueZ authorization to one supplied peer address and
the A2DP/AVRCP UUID set. The HCI initializer resets the controller and removes
volatile keys before a clean reconstruction. Artifact hashes for the validated
build are recorded in [P1-045](../../metadata/P1-045.json).

`build-bluealsa-aplay.sh` applies the reviewed active-PCM lease patch to the
pinned BlueALSA 4.0.0 source. It checksum-gates the source, patch, compiler,
strip tool, and final static ARM binary:

```bash
tools/control/build-bluealsa-aplay.sh \
  --source-archive path/to/bluez-alsa-4.0.0.tar.gz \
  --sysroot path/to/armhf-sysroot \
  --output ../reinvoke-archive/build/artifacts/bluealsa-aplay
```

The patched player writes its worker thread ID to the configured RAM lease only
after receiving positive PCM data. It removes the lease after 500 ms of
inactivity and during worker cleanup.

Forward the RAM-native device's private WAMP port over USB:

```bash
adb -s 0123456789ABCDEF forward tcp:19999 tcp:9999
```

Call a procedure:

```bash
node tools/control/wamp-call.mjs com.harman.vui.getmcustatus
```

When Wi-Fi is configured, pass the device address and native port instead:

```bash
node tools/control/wamp-call.mjs \
  com.harman.vui.getmcustatus \
  --host 192.0.2.10 \
  --port 9999
```

Do not expose the unauthenticated legacy WAMP listener to an untrusted network.
Prefer a replacement authenticated API for the persistent platform.

SSH is optional. This client can use an ADB-forwarded USB connection during
development and a direct Wi-Fi connection during normal operation. A future
provisioning service can expose a temporary access point without making a shell
part of the product interface.
