# Runtime Interface Inventory

Static inventory from the extracted `83_IMAGE` rootfs. This is an interface
map, not proof that any service is safe to expose on a modern network.

| Boundary | Evidence | Current interpretation |
|---|---|---|
| Message bus | `logwrapper bonefish -r default -t 9999 -w 9998 -d` in `system-manager` | A `bonefish` WAMP router owns both ports: realm `default`, rawsocket on 9999, WebSocket on 9998 |
| MCU control | `mcu-interface 127.0.0.1 9999`; `MCUInterface::register_wamp` symbols in the binary | A WAMP *client* dialing the router, not a listener on 9999. Reaches the MCU over `/dev/i2c-0` with sysfs GPIO handshake. See [mcu-boundary.md](../../emulation/mcu-boundary.md) |
| Audio/UI | `audio-ui 127.0.0.1 9999 $boot_mode $run_mode` | Another WAMP client of the same router; the shared argument is the router endpoint |
| DSP | `dsp-client`, `com.harman.dsp.*` URIs | WAMP client exposing DSP volume, mic, and version procedures |
| Network services | Firewall rules for 22, 9998, 9999, HTTPS, UDP 48301, DHCP, mDNS | Recovery/setup and maintenance surface; authentication semantics unresolved |
| Process supervision | `system-manager /etc/podium/podium.conf` | Podium starts and restarts MCU, audio, connection, music, Bluetooth, Cortana, and OTA processes |
| OTA | `ota_rbua_install.sh` and RedBend RB_UA config | Separate update orchestration targeting `bootimgs` and `rootfs` |
| Service Ethernet | `serviceport.sh` | Configures `eth0`, defaulting to `172.20.20.20`, and announces it with gratuitous ARP |
| Bluetooth | `bluetooth.sh`, `btmrvl.ko`, `sd8887` firmware | Marvell SDIO Bluetooth path; exact host-controller wiring remains unresolved |
| Wi-Fi | `mlan.ko`, `sd8xxx.ko`, `sd8887_wlan_a2_p78.bin` | Marvell SDIO Wi-Fi path with LS9 calibration profiles |
| Cortana | `cortana.sh`, `cortana-harness` | Voice-assistant process boundary; cloud/backend dependencies unknown |

## Correction to the earlier reading

An earlier revision of this file recorded `mcu-interface` and `audio-ui` as
sharing a local service endpoint on port 9999 with the wire protocol unknown.
That reading was wrong in a way worth recording: two servers cannot both bind
the same port. Both processes take `127.0.0.1 9999` as an *argument*, and both
are clients.

The listener is `bonefish`, an open-source C++ WAMP router started by
`system-manager`. The transport is therefore not proprietary. WAMP is a
published protocol carrying JSON or MessagePack over WebSocket or rawsocket,
so the command format does not require bench capture to establish.

## Recovered control surface

Static extraction of URI literals from the service binaries yields 130 distinct
`com.harman.*` and `com.cortana.*` procedure and topic names. The set includes
the boundaries most relevant to local reuse:

- Audio path: `com.harman.volume.set`, `com.harman.dsp.volume`,
  `com.harman.dsp.mic`, `com.harman.source.start`, `com.harman.source.flush`
- Power and mute gating: `com.harman.vui.muteampcontrol`,
  `com.harman.vui.mutedaccontrol`, `com.harman.vui.powerdspcontrol`,
  `com.harman.vui.setmcupowermode`
- Input and indication: `com.harman.vui.keypress`, `com.harman.vui.uicommand`,
  `com.harman.aui.*`
- Transport control: `com.harman.bluetooth.{resume,pause,next,prev,stop}`,
  `com.harman.music.{resume,pause,stop}`
- Liveness: `com.harman.heartbeat.{mcu,audio,music}` and matching
  `com.harman.ready.*` topics

These are names, not signatures. Argument shapes and return types are not
established by string extraction and remain unresolved.

## MCU boundary

The MCU is a separate microcontroller reached over I2C rather than a serial
port. `mcu-interface` opens `/dev/i2c-0` and drives interrupt or handshake
lines through the sysfs GPIO interface (`/sys/class/gpio/export`,
`gpio%d/direction`, `gpio%d/edge`, `gpio%d/value`).

Its firmware ships in the rootfs as `usr/share/mcu/cortana_mcu.bin`, 13,312
bytes. Recoverable strings expose a small command interpreter with the prompt
`cortana_mcu #` and verbs including `rgb r`, `rgb g`, `rgb b`, `rgb w`,
`led bt`, `led wf`, `led pw`, `ver`, `up app`, and `flash_libre`. The host side
carries a matching firmware update path
(`com.harman.vui.requestmcuupgrade`, `startmcuupgrade`, `sendfirmwaredata`,
`mcuupgraderesult`).

The I2C slave address is not recoverable from strings alone and is not asserted
here. The exact MCU part number remains unknown; the firmware size and command
set are consistent with a small microcontroller, but that is an inference, not
an identification.

## Revival implication

The strongest near-term revival target remains the local audio, UI, and MCU
boundary. The evidence above narrows what that requires. Because the control
plane is an open protocol spoken to an open-source router, driving the audio
path does not depend on reverse engineering a proprietary format. It depends on
reaching the bus and learning each procedure's argument shape.

Two consequences follow. Bus access on hardware is the gating problem, not
protocol discovery. And the same router and client binaries can be exercised
off-device, which allows argument shapes to be recovered without risking the
unit.
