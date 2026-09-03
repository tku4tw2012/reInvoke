---
title: reInvoke control tools
description: Host-side tools for calling the Invoke native control plane
ms.date: 2026-09-03
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
