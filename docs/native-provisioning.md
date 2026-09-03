---
title: Native Wi-Fi provisioning boundary
description: Authenticated RAM-only onboarding architecture for the reInvoke platform
ms.date: 2026-09-03
ms.topic: concept
---

The replacement onboarding path separates untrusted network parsing from radio
and credential application. The HTTPS parser is implemented and verified on the
physical Invoke. SD8887 access-point mode and station credential application
remain separate hardware adapters.

## Stock boundary

Held `Barracuda_libre-12.2050.3` artifacts establish these reusable hardware
facts:

* The Marvell Wi-Fi library names station interface `wlan0` and AP interface
  `p2p0`
* Its hostapd template supports SSID visibility and WPA passphrases
* Its control vocabulary includes `UAP_BSS_CTRL`
* The dnsmasq configuration binds `p2p0` and leases
  `192.168.43.100` through `192.168.43.155`
* A long microphone-mute press maps to `wifisetup-enter`
* `audio-ui` publishes `com.harman.networkConfiguration`
* `connection-manager` and its Libre Wi-Fi plugin consume that boundary

The retained Chromium `setup.html` is not evidence of an active Invoke server.
No held executable owns its `/setup/*` endpoint strings. The page is also
unsuitable for reuse because it posts the network passphrase over
unauthenticated HTTP and contains `TODO: Encrypt password`.

## Replacement components

The onboarding design has four independent components:

1. A physical gate opens a bounded provisioning window.
2. A radio adapter creates an isolated WPA2 AP on `p2p0`, without forwarding to
   another interface.
3. `reinvoke-provisiond` accepts one authenticated TLS request.
4. A privileged station adapter receives the request over a root-owned Unix
   socket and applies it without shell interpolation.

Only the final adapter may write a persistent network configuration. During RAM
validation it must use a tmpfs configuration and leave NAND unmounted.

## Authenticated parser

`tools/provisioning/` contains a dependency-free Go service with these
properties:

* An explicit bind address, intended to be `192.168.43.1:8443`
* An in-memory ECDSA P-256 certificate with TLS 1.3 minimum
* A random 256-bit bearer token
* A root-only descriptor containing the URL, token, certificate fingerprint,
  and relative expiry
* A monotonic five-minute default lifetime and 15-minute hard maximum
* A 4 KiB JSON body limit and unknown-field rejection
* WPA2-PSK validation for a 1-32 byte UTF-8 SSID and 8-63 byte passphrase
* No credential logging or credential file
* One successful request per process
* A bounded Unix-socket handoff with an explicit adapter acknowledgement
* Root ownership checks on the socket directory, socket node, and connected
  peer through `SO_PEERCRED`

The separate `reinvoke-wifi-applyd` adapter:

* Requires root and a root-only ramfs/tmpfs runtime directory
* Accepts only a UID-0 Unix-socket peer
* Revalidates request bounds and fields
* Derives the 256-bit WPA2 PSK with PBKDF2-HMAC-SHA1 and 4,096 iterations
* Writes SSID and derived PSK as hexadecimal values, never the passphrase
* Invokes fixed root-controlled `wpa_supplicant` and `wpa_cli` binaries without
  a shell
* Acknowledges success only after `wpa_state=COMPLETED`
* Terminates the supplicant and removes the RAM config after a failed attempt

The TLS key never leaves process memory. The descriptor is removed when the
request succeeds, the timer expires, or the process receives a termination
signal.

Before network setup, the Invoke wall clock may still be 1970. The process
lifetime therefore uses a monotonic clock. When wall time is unset, the
certificate uses a broad 2000-2100 validity window and clients authenticate it
by the descriptor's SHA-256 fingerprint. A sane clock adds `expires_utc` to the
descriptor.

## Bootstrap transport

The current trusted bootstrap is yellow-mode USB:

1. Start the AP and provisioning daemon after a physical long press.
2. Read `/run/reinvoke/provisioning.json` through root ADB.
3. Join the temporary WPA2 AP.
4. Pin the descriptor's certificate fingerprint.
5. Send its bearer token and one credential request.

This is sufficient for development and recovery. A product flow can later
deliver the same descriptor over Bluetooth LE or DPP without weakening the
HTTPS API or teaching the parser about radio drivers.

## Physical validation

The hardened static ARMv7 binary is 4,784,128 bytes with SHA-256
`5bde5aefdb21a9caf605fb57e9a62cf9597b8ebddd1fc9d65938441d04678b07`.
Two clean builds were byte-identical. It keeps status requests responsive
during a network apply and interrupts post-connect Unix socket I/O when the
request context is canceled.

The separate static ARMv7 Wi-Fi adapter has SHA-256
`6697df000d130a6461d1e3f57b6ebe8b1ad1742984a94250bc1e243dca097610`.
Two clean adapter builds were also byte-identical.

On Linux `3.8.13-reinvoke-audio`, a loopback-only test verified:

* ARM binary execution on the physical SoC
* A mode-0600 descriptor under a mode-0700 runtime directory
* Certificate fingerprint equality from the host
* HTTP 401 without the bearer token
* HTTP 200 for authenticated status
* HTTP 502 when the privileged adapter socket was absent
* No fake SSID or passphrase in the service log
* Clean monotonic expiry with a 1970 wall clock
* Descriptor deletion and process exit after 30 seconds
* Refusal to run as an unprivileged host user
* Root ownership checks before any Unix-socket credential write
* Descriptor deletion after a live termination signal
* A mode-0600 root-owned apply socket on the physical Invoke
* No station config before a credential request
* Apply-socket removal on a live termination signal
* End-to-end root peer checks and strict JSON framing between both daemons
* Derived-config removal and HTTP 502 when a fake supplicant failed

Unit and race tests also verify successful delivery to a trusted same-UID Unix
peer, rejection when the adapter directory is group/world writable, the
standard IEEE WPA2 PSK vector, and replacement of an existing 0770
`wpa_supplicant` control socket for a second provisioning window.

The adapter-rejection test used only fake credentials and did not touch a radio
or persistent storage.

## Attended AP validation

The AP candidate remains staged outside the normal boot directory:

| Artifact | SHA-256 |
|----------|---------|
| `3.8.13-reinvoke-audio-sd8887` kernel | `4dbfc484c3ff99325b293aa02810d9e97396252a70d89fdf47dead5443135c4c` |
| Native SD8887 provisioning initramfs | `8e087c98d8823544a2a004c46d5868fed0106cc788b029b022c3b48d24a549c6` |

The kernel contains loadable native `mlan.ko`, `sd8xxx.ko`, `bt8xxx.ko`, and
`btmrvl.ko` modules with matching
`3.8.13-reinvoke-audio-sd8887` vermagic. The initramfs includes those modules
and both checksum-gated provisioning daemons. Station-only remains its default;
`reinvoke.wifi_mode=sta-uap` is required to request `p2p0`.

An attended yellow-mode test booted this pair with
`reinvoke.wifi_mode=sta-uap`. The USB gadget returned in five seconds. The
native driver reported `drv_mode=3` and exposed both `mlan0` and `p2p0` while
preserving HCI, GPIO, SPI, ALSA, and the read-only NAND node. NAND remained
unmounted, and the kernel log contained no panic, oops, or fault signature.

The test then:

* Ran the retained hostapd as its numeric UID/GID 1008 with a
  `0770 root:1008` runtime directory and `0640 root:1008` configuration
* Created a random-key WPA2 AP on `p2p0`
* Bound dnsmasq DHCP only to `p2p0` with DNS disabled
* Kept IPv4 and IPv6 forwarding disabled
* Assigned the Mac mini an address in `192.168.43.0/24` with no gateway or DNS
* Verified the TLS certificate fingerprint over the AP
* Received HTTP 202 from the complete parser-to-adapter path
* Removed the host connection profile, AP key, derived station configuration,
  daemons, sockets, and RAM runtime after the test

The success-path adapter test used root-controlled fake station executables, so
it exercised authenticated AP delivery and both daemon boundaries without
joining `mlan0` to an external network. No real network credential was used or
retained. The proven default boot pair was restored to active host staging
afterward.

## Real station validation

A subsequent RAM-only test cloned the Mac mini's active NetworkManager profile
without printing the SSID or PSK. The shell held both values only in memory,
constructed JSON through standard input, and sent the request through an ADB
loopback forward. The Wi-Fi secret did not enter a process argument or host
file.

The physical Invoke then:

* Returned HTTP 202 through `reinvoke-provisiond` and
  `reinvoke-wifi-applyd`
* Reached `wpa_state=COMPLETED` with the real `wpa_supplicant`
* Matched the source SSID without logging it
* Stored only a hexadecimal SSID and derived PSK in a mode-0600 RAM file
* Acquired and renewed a DHCP lease
* Reached the gateway, a public IPv4 address, and a DNS-resolved host
* Kept IP forwarding disabled and NAND unmounted

Both provisioning daemons removed their sockets and exited after success. The
station supplicant, DHCP renewal client, derived configuration, and resolver
state remain only in the current RAM boot.

The external evidence bundle is
`reinvoke-archive/hardware/usb-attempts/20260903T105444Z-sd8887-sta-uap-reconnect-arm-stock/`.

## Owned network lifecycle

The hardened static ARMv7 `reinvoke-networkd` artifact is 2,293,760 bytes with
SHA-256
`cb61bcdd0b9f4b145619514b9acb41d74d98042f8698419ea37e0c4864340a66`.
Two clean builds were byte-identical. The service uses only fixed,
root-controlled executable paths and does not invoke a shell.

Live RAM-only validation replaced the temporary DHCP hook and proved:

* The service detected the existing `wpa_state=COMPLETED` station.
* Its supervised BusyBox `udhcpc` acquired and renewed the station lease.
* The lease handler validated address, mask, route, DNS, and lifetime values.
* `/etc/resolv.conf` pointed to atomically written RAM-only resolver state.
* Gateway, public IPv4, and DNS-resolved reachability succeeded.
* Graceful shutdown removed the IPv4 address, default route, resolver link,
  lease, and DHCP child.
* A service restart reacquired connectivity without replacing credentials.
* A station disconnect removed network state, and reconnect reacquired it.
* A second supervisor was rejected without disturbing the active service.
* A stale owner record pointing at a reused PID did not block startup or signal
  the unrelated process.
* Authenticated replacement through both provisioning daemons replaced the
  supplicant and DHCP child while the same supervisor remained active.
* The replacement regained association, lease, route, resolver, public IPv4,
  and DNS without exposing or writing the plaintext credential on the host.

The initramfs builder checksum-gates the artifact and PID 1 auto-starts it when
included. PID 1 sends daemon output to the bounded kernel log and restarts a
failed supervisor after five seconds. The `reinvoke.networkd=off` kernel
argument keeps the packaged service disabled for manual recovery. Two clean
builds of the packaged initramfs were byte-identical. The final hardened
external image is 40,068,440 bytes and has SHA-256
`c056d21b0e147fb9fd38a9458952528be1f58b17566f1223a1147eca14d53e21`
and contains the reviewed network daemon, provisioning adapters, pinned kernel
module tree, release manifest, and PID 1. Provenance is recorded in
[P1-046](../metadata/P1-046.json).

## Remaining validation

The final hardened checksum-gated image cold-booted in yellow mode on
2026-09-03.
PID 1 automatically started `reinvoke-networkd`; its live SHA-256 was
`cb61bcdd0b9f4b145619514b9acb41d74d98042f8698419ea37e0c4864340a66`.
The packaged `reinvoke-provisiond` SHA-256 was
`5bde5aefdb21a9caf605fb57e9a62cf9597b8ebddd1fc9d65938441d04678b07`.
The root filesystem contained only `rootfs`, `proc`, `sysfs`, `devtmpfs`,
`devpts`, and `tmpfs` mounts, with no NAND or MTD block mounted. The SD8887
WLAN and Bluetooth drivers loaded, and the supervisor correctly remained
waiting for a root-controlled station supplicant.

This image intentionally contained no station credentials, so association,
DHCP, DNS, and default-route acquisition were not expected in this cold-boot
check. The already validated credentialed station lifecycle remains covered by
the live RAM-only validation above.

Persistent storage remains out of scope until backup, rollback, and recovery
are independently proven.
