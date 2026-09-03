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

The static ARMv7 binary is 4,784,128 bytes with SHA-256
`2948300b5be513e57ec26302f3f393b15759344b5e3c5cabdb84061a3b8e1b70`.
Two clean builds were byte-identical.

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

## Remaining validation

The unbooted AP candidate is staged outside the normal boot directory:

| Artifact | SHA-256 |
|----------|---------|
| `3.8.13-reinvoke-audio-sd8887` kernel | `4dbfc484c3ff99325b293aa02810d9e97396252a70d89fdf47dead5443135c4c` |
| Native SD8887 provisioning initramfs | `8e087c98d8823544a2a004c46d5868fed0106cc788b029b022c3b48d24a549c6` |

The kernel contains loadable native `mlan.ko`, `sd8xxx.ko`, `bt8xxx.ko`, and
`btmrvl.ko` modules with matching
`3.8.13-reinvoke-audio-sd8887` vermagic. The initramfs includes those modules
and both checksum-gated provisioning daemons. Station-only remains its default;
`reinvoke.wifi_mode=sta-uap` is required to request `p2p0`.

This pair has not been booted. It remains in isolated staging until an operator
is present to perform yellow mode and observe the 30-second USB criterion.

The next hardware increment must:

1. Boot the isolated native SD8887 profile and confirm that it exposes `p2p0`.
2. Create a WPA2 AP with a random per-window key.
3. Bind dnsmasq only to `p2p0`.
4. Confirm that no forwarding or upstream DNS path exists.
5. Run the HTTPS test through the AP instead of USB loopback.
6. Run the implemented root-owned station adapter against a disposable test
   network using RAM-only `wpa_supplicant` state.
7. Verify that credentials never appear in
   logs, process arguments, or repository artifacts.

Persistent storage remains out of scope until backup, rollback, and recovery
are independently proven.
