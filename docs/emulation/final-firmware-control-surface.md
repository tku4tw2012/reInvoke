# Local Control Surface of the Final Firmware

What can and cannot be reached on an Invoke running `Barracuda_libre-12.2134.0`,
the last build Harman shipped. The owner's unit is confirmed to be on this
build.

Everything below is static analysis of the preserved rootfs, corroborated by
running the same binaries under emulation. None of it has been tested against
the physical unit.

## Summary

On stock final firmware the device is a Bluetooth speaker with no supported
local control channel beyond Bluetooth itself. The rich WAMP control API that
the services expose listens on all interfaces but is blocked externally by the
stock firewall. `adbd` is present but not started, and the debug gate that would
open those paths is read from a DCT record in the factory-setting partition.

Controlling the device programmatically therefore requires code execution on it,
which returns to the USB download-mode question that the observation procedure
treats as its decisive gate.

## What the services expose

The WAMP bus carries a complete media control API. See
[control-plane-emulation.md](control-plane-emulation.md) for the recovered
procedures, which include pairing, transport control, track selection, volume,
mute, and shutdown.

The router listens on all interfaces. Port 9999 carries rawsocket and 9998
carries WebSocket. The device firewall, rather than the router bind address,
prevents remote access on stock firmware.

## Why that API is not reachable

`usr/sbin/firewall.sh` explicitly drops the bus ports:

```sh
iptables -A INPUT -p tcp --dport 9998 -j DROP
iptables -A INPUT -p tcp --dport 9999 -j DROP
iptables -A INPUT -p tcp --dport 22   -j DROP
```

Those rules are preceded by a conditional block that would instead accept
9998, 9999, and SSH:

```sh
DEBUG=$(/usr/bin/dctflag nofw)
if [[ "$DEBUG" ]]; then
    iptables -A INPUT -p tcp --dport ssh  -j ACCEPT
    iptables -A INPUT -p tcp --dport 9998 -j ACCEPT
    iptables -A INPUT -p tcp --dport 9999 -j ACCEPT
fi
```

The gate is `dctflag`. `etc/podium-env` documents its source:

```sh
# Get debug flag from device certificate
DEBUG=$(/usr/bin/dctflag exdata)
```

`dctflag` is a small ARM executable that formats `dct_%s=%s`, reads the `wlan0`
hardware address, and references `/factory_setting/%d.dct`. The
`factory_setting` YAFFS2 partition is not mounted read-only, so the evidence
does not establish that this record is immutable. Its format, integrity checks,
and relationship to secure boot remain unresolved. Changing it would still be
a persistent flash modification and is outside the observation procedure.

Ports left open by the firewall are mDNS, `bootps`, UDP 48301, HTTPS, TCP
12345, and ICMP echo.

## USB

`init.rc` configures the USB gadget at boot:

```text
write /sys/class/android_usb/android0/idProduct "0d02"
write /sys/class/android_usb/android0/iProduct  "MRVL USB SDK"
write /sys/class/android_usb/android0/functions "adb"
write /sys/class/android_usb/android0/enable    "1"
```

So the device is expected to enumerate over the Micro-USB service port and
advertise an ADB function.

However, the service that would answer is not running:

```text
service adbd /sbin/adbd
    disabled
```

and its startup line is commented out:

```text
    #start adbd
```

The gadget is configured, the daemon is not started. A host should therefore see
a USB device but get no ADB shell. Confirming that empirically is step 5 of the
observation procedure, and the result is worth recording either way.

## SSH

`sshd` is started at boot as `dropbear`, but port 22 is dropped by the firewall
outside the debug path. A comment in `init.rc` notes that the executable is
expected to be removed from secure system images; it is still present in this
build.

## Network reachability

The final build adds `wifi-blocker`, a supervised process listed in
`podium.conf` with a configuration file dated 2021-09-11. Its behaviour is not
established from strings alone, but its name, its addition in the same release
that removed the cloud assistant, and that date together indicate the Wi-Fi path
is intended to be suppressed.

`serviceport.sh` still configures `eth0` from `/data/service-ip.txt`, defaulting
to `172.20.20.20`, and announces it with gratuitous ARP. Whether any `eth0`
exists on this hardware is unresolved. No external Ethernet jack is documented
in the regulatory photographs.

## Practical consequences

Bluetooth is the supported path. Pairing and A2DP playback need no
modification, and AVRCP transport controls map onto the same operations the
internal API exposes.

The WAMP API is real and fully documented, but on stock firmware it is reachable
only from software already running on the device.

Every route to that API requires code execution: a RAM boot over USB download
mode, or a modified rootfs. Both are gated by whether the BootROM downloader can
be reached without opening the enclosure, which is unresolved and cannot be
settled by analysis. No BootROM, OTP, or secure-boot source exists anywhere in
the corpus.

## Capability versus access

The evidence supports separating two questions. The first is whether the
hardware can be more than a Bluetooth speaker. Confidence is high: it is an ARM
Linux computer with Wi-Fi and Bluetooth radios, USB gadget support, microphone
and DSP audio paths, persistent storage, and a local service bus whose volume
and mute procedures now run under emulation.

The second question is whether those capabilities can be reached
non-destructively on a closed unit. Confidence is moderate because the answer
depends on USB observation. An unexpected ADB response would provide immediate
local execution. A reachable Marvell downloader could support a RAM-only
environment without writing NAND. If neither path appears, the stock external
surface offers little beyond Bluetooth.

Plausible outcomes after safe code execution include a network-controlled
speaker, a USB audio endpoint, a local automation target, or a custom voice
front end. These are feasibility directions, not established physical-device
results.

## Why NAND writing is not the next step

A persistent rootfs modification has substantial rewards. It could start a
local control service at boot, expose the WAMP bus intentionally, change the
USB gadget profile, and support custom audio or automation software without an
external boot host.

The failure modes are also persistent. A wrong partition target, interrupted
erase or write, incorrect bad-block handling, invalid boot-status transition,
or secure-boot rejection could leave the unit unable to start. Recovery might
then require opening the enclosure or using board-level access. The final
firmware contains A/B boot components, but the exact status and fallback
semantics are only partly recovered. The update marker is documented in
[boot-update-state.md](boot-update-state.md), while active-slot selection
remains unresolved.

The safe progression is stock observation followed by testing for unexpected
ADB and BootROM enumeration during an ordinary boot. Button-triggered entry is
excluded until a specific non-destructive sequence is established. If a
download path appears, a RAM environment can validate drivers, service startup,
networking, and recovery tooling before any NAND write. Persistent modification
belongs after those tests and only with a verified readback and recovery path.

## What would change this picture

If the USB port yields an ADB shell despite the disabled service, the bus
becomes reachable immediately by forwarding a local port, and no firmware
modification is needed.

If the BootROM downloader appears, a RAM-booted initrd could run the stock
services with the firewall's debug branch taken, exposing the API on the network
without writing to NAND.

If neither appears, the device remains a Bluetooth speaker, which is what its
final firmware was built to be.
