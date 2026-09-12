---
title: Volatile Wi-Fi provisioning tools
description: Component builds, authenticated credential protocol and NetworkManager client
ms.date: 2026-09-12
ms.topic: how-to
---

The owned provisioning stack replaces donor plaintext HTTP setup. Credentials
remain in RAM regardless of boot medium; candidate-specific evidence and the
physical AP flow are in [native provisioning](../../docs/native-provisioning.md).

## Components and build

| Component            | Responsibility                                           |
| -------------------- | -------------------------------------------------------- |
| `provisiond`         | TLS, bearer authorization, bounded JSON validation       |
| `wifi-applyd`        | Root-only credential application and station association |
| `networkd`           | DHCP, routes, resolver state and cleanup                 |
| `windowd`            | Physical-button AP window and child lifecycle            |
| `configure-wifi.mjs` | NetworkManager client with same-socket TLS pinning       |

From the repository root, build each required component using the retained
Ubuntu Go 1.18 toolchain:

```bash
tools/provisioning/build.sh --component provisiond \
  --archive-root "${REINVOKE_ARCHIVE}" --output "<artifact-dir>/reinvoke-provisiond"
```

`--component` defaults to `provisiond`; alternatives are `wifi-applyd`,
`networkd` and `windowd`. Use distinct fresh outputs; windowd is packaged as
`reinvoke-provision-windowd`. No third-party Go modules are used; proxy and
checksum database access are disabled. The public clone lacks the toolchain
and board-runtime inputs.

## HTTPS contract

`windowd` normally starts the adapter and HTTPS service. For isolated target
integration, start the adapter on a root-owned socket first, then:

```bash
reinvoke-provisiond -listen "<ap-address>:8443" \
  -apply-socket /run/reinvoke/wifi-apply.sock \
  -descriptor /run/reinvoke/provisioning.json -apply-timeout 25s -lifetime 5m
```

The root daemon binds an explicit IP, uses TLS 1.3 with an in-memory ECDSA
certificate, generates a 256-bit bearer token, and writes a root-only descriptor
with URL, token, SHA-256 fingerprint and expiry. Lifetime is monotonic,
default 5 minutes, allowed 30 seconds-15 minutes. Before a sane wall clock,
the descriptor has only `expires_after_seconds`; otherwise it also includes
`expires_utc`. The certificate has broad validity for early-boot clocks.

Pin the descriptor fingerprint and use `Authorization: Bearer <token>`.
`POST /v1/wifi` requires `Content-Type: application/json`, a body no larger
than 4 KiB, no unknown fields or trailing JSON:

```json
{"ssid":"<station-ssid>","passphrase":"<station-passphrase>","security":"wpa2-psk","hidden":false}
```

SSID must be 1-32 valid UTF-8 bytes; passphrase must be 8-63 bytes.
Both reject NUL, CR and LF. Security must be exactly `wpa2-psk`.

One valid request gets HTTP 202 `{"accepted":true}` before asynchronous radio
changes. This is request acceptance, not association. Adapter failure leaves
the window retryable; adapter success shuts the daemon down.
Authenticated `GET /v1/status` returns `ready` and expiry fields.
Rejections include 401 authorization, 400 input, 409 in-progress/completed,
405 method and 415 content type.

`-apply-timeout` must exceed adapter `-connect-timeout` (default 20s) and be
at least five seconds shorter than the window. Adapter acknowledgment is
`wpa_state=COMPLETED`, not DHCP completion.

## Application and trust boundary

The daemon checks adapter directory/socket ownership, rejects symlinked or
group/world-writable paths and verifies the connected root peer with
`SO_PEERCRED`. It sends credentials without logging them. `wifi-applyd`
derives the WPA2 PSK in memory, writes mode-`0600` configuration to ramfs/tmpfs
without plaintext passphrase, and starts fixed root-controlled supplicant/CLI
paths without a shell. The derived PSK is still a credential.

A later window replaces the existing root-owned supplicant within the same
bounded connection timeout. `networkd` supervises `udhcpc`, validates leases,
updates RAM resolver state atomically, and removes addresses/routes/resolver
and lease state on disconnect. It reacquires after replacement, failure or
restart. A lifetime lock rejects duplicates; stale records cannot authorize
signaling unrelated PIDs. These components run as root, not privilege-separated
from one another.

Descriptor bootstrap uses root USB ADB in RAM recovery or HTTP on the
physically opened credential-controlled native AP. The TLS pin derives trust
from that bootstrap, not a public CA. BLE/DPP are not implemented.

## NetworkManager client

Prepare private AP and destination profiles; credentials are read into memory,
not passed as CLI arguments:

```bash
node tools/provisioning/configure-wifi.mjs \
  --ap-profile "<provisioning-ap-profile>" --target-profile "<station-profile>" \
  --descriptor-url "http://<ap-address>:8080/provisioning.json" --timeout-seconds 30
```

> [!WARNING]
> This switches the host's Wi-Fi and submits credentials. Use the intended
> physical AP; keep descriptors, tokens and filled requests private.

The client verifies the fingerprint on the same TLS socket as the POST and
restores the prior host profile in cleanup. `--nmcli` overrides the executable;
timeout accepts 1-300 seconds, default 30. Accepted credentials and restored
host Wi-Fi do not establish device DHCP or SSH login.

## Offline tests

```bash
tools/provisioning/test.sh --archive-root "${REINVOKE_ARCHIVE}"
node --test tools/provisioning/configure-wifi.test.mjs
```
