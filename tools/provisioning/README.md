---
title: Secure native provisioning service
description: Ephemeral authenticated Wi-Fi credential delivery for the reInvoke RAM platform
ms.date: 2026-09-03
ms.topic: concept
---

`reinvoke-provisiond` is a RAM-only HTTPS control plane for delivering one Wi-Fi
configuration to a separate network adapter. It replaces the retained
Chromecast setup page, which posts plaintext credentials over unauthenticated
HTTP.

`reinvoke-wifi-applyd` is the separate privileged adapter. It accepts only a
root peer on a root-owned Unix socket, derives the WPA2 PSK in memory, writes a
mode-0600 configuration containing no plaintext passphrase to ramfs/tmpfs, and
starts fixed root-controlled `wpa_supplicant` and `wpa_cli` paths without a
shell.

Opening a later physical provisioning window cleanly terminates an existing
root-owned supplicant before applying the replacement network. The total
replacement, launch, and association path is bounded by `-connect-timeout`.

## Security boundary

The daemon:

* Binds only to an explicit IP address
* Generates an in-memory ECDSA certificate and requires TLS 1.3
* Generates a 256-bit bearer token from the kernel random source
* Writes the URL, token, certificate fingerprint, and expiry to a root-only
  descriptor in `/run`
* Expires after five minutes by default and refuses lifetimes over 15 minutes
* Accepts one successful WPA2-PSK request with a 4 KiB body limit
* Never logs an SSID or passphrase
* Sends credentials to a root-owned Unix socket instead of writing them to disk
* Shuts down after the adapter acknowledges the request
* Verifies root ownership of the adapter directory, socket node, and connected
  peer through `SO_PEERCRED`
* Rejects symlinked or group/world-writable adapter paths

The descriptor is intended to cross an already trusted transport such as root
ADB over USB. A product onboarding flow can later replace that bootstrap with
Bluetooth LE or DPP without changing the credential API.

The service lifetime uses a monotonic timer. Before network setup the Invoke's
wall clock may still be 1970; in that case the descriptor reports only
`expires_after_seconds`, and the ephemeral certificate uses a broad validity
range so a client with a correct clock can verify its pinned fingerprint. When
the device clock is sane, the descriptor also includes `expires_utc`.

## Build

The builder uses the locally extracted, repository-signed Ubuntu Go 1.18
toolchain and no third-party Go modules. Module proxy and checksum database
access are disabled during the build:

```bash
tools/provisioning/build.sh \
  --output ../reinvoke-archive/build/artifacts/reinvoke-provisiond

tools/provisioning/build.sh \
  --component wifi-applyd \
  --output ../reinvoke-archive/build/artifacts/reinvoke-wifi-applyd

tools/provisioning/build.sh \
  --component networkd \
  --output ../reinvoke-archive/build/artifacts/reinvoke-networkd
```

## Run

Create a root-only runtime directory and start the network adapter's Unix
socket first. The daemon itself must run as root. Then run:

```bash
reinvoke-provisiond \
  -listen 192.168.43.1:8443 \
  -apply-socket /run/reinvoke/wifi-apply.sock \
  -descriptor /run/reinvoke/provisioning.json \
  -apply-timeout 25s \
  -lifetime 5m
```

Read `/run/reinvoke/provisioning.json` over USB. Clients must pin the listed
certificate SHA-256 fingerprint and send its token as:

```text
Authorization: Bearer <token>
```

The credential request is:

```json
{
  "ssid": "example-network",
  "passphrase": "example-passphrase",
  "security": "wpa2-psk",
  "hidden": false
}
```

The daemon does not configure `p2p0`, dnsmasq, hostapd, or
`wpa_supplicant`. Those hardware-specific adapters remain separate so this
internet-facing parser never receives shell or raw driver privileges.

The parser's `-apply-timeout` must be at least five seconds shorter than its
window and longer than the adapter's `-connect-timeout`. Defaults are 25 and
20 seconds respectively.

The adapter acknowledges success at `wpa_state=COMPLETED`.
`reinvoke-networkd` owns the separate DHCP and resolver boundary so the
credential adapter does not gain route or DNS policy. It monitors the
root-controlled supplicant socket, supervises BusyBox `udhcpc`, validates lease
values before invoking fixed commands without a shell, and atomically points
`/etc/resolv.conf` at RAM-only resolver state.

The service removes its IPv4 address, default route, resolver link, and lease
state on disconnect or shutdown. It reacquires after association, credential
replacement, DHCP failure, or daemon restart. A full-lifetime lock rejects a
second supervisor, and stale process records never authorize signaling an
unrelated PID on kernels without process file descriptors.
