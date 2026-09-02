---
title: No-disassembly observation procedure
description: Safe physical observations for an Invoke that remains closed
ms.date: 2026-09-02
ms.topic: how-to
---

Operator procedure for a physical Invoke that will not be opened. The procedure
does not flash firmware or write raw storage. Bluetooth pairing can update
ordinary device settings and is called out separately.

This procedure is human-executed. Record results as you go; negative results
matter as much as positive ones.

## Why the order changed

Static analysis established that Harman's final firmware, `Barracuda_libre-12.2134.0`
shipped in the OTA2 bundle, removes Cortana and Spotify and adds a `wifi-blocker`
service. See `docs/bundle-contents/invoke-ota2/ota2-analysis.md`.

The unit's Bluetooth-only behavior is consistent with this firmware line, but
the exact installed version has not been read from the device.

The device is already the Bluetooth speaker its last firmware was built to be.
The open questions are no longer about what it is, but about what can be
reached on it. Analysis of that build's firewall, USB gadget, and debug gate is
in `docs/emulation/final-firmware-control-surface.md`, and it predicts specific
observable outcomes that the steps below are designed to confirm or refute.

## Predictions to test

Recording these in advance, so the observations either confirm or falsify them
rather than being fitted to whatever is seen.

| Prediction | Basis |
|---|---|
| Pairs over Bluetooth and plays audio | Final build is a Bluetooth-speaker configuration |
| Enumerates over USB as an Android gadget, `idProduct 0d02`, `MRVL USB SDK` | `init.rc` enables the gadget with function `adb` |
| No ADB shell despite the gadget | `service adbd` is `disabled`, `start adbd` commented out |
| Quiet on the network, little or no Wi-Fi activity | `wifi-blocker` added in the same release that removed the assistant |
| Ports 22, 9998, 9999 time out or appear filtered if reachable | `firewall.sh` uses `DROP` outside the DCT-gated debug branch |

A falsified prediction is a more valuable result than a confirmed one. If ADB
answers, the recovered control API becomes reachable immediately.

## Safety rules

- Do not open the enclosure.
- Do not flash, write, or run any update.
- Do not test unknown button combinations. The supported runtime factory reset
  erases user settings and is a separate, explicit experiment.
- Stop and record if anything overheats, smells, or behaves erratically.

## Step 1: Identify and photograph

Assign the unit a sample identifier such as `INVOKE-01`. Photograph the base
label, the model and serial numbers, and the connector area. Record the power
adapter's printed rating.

Expected from regulatory evidence: a barrel DC jack and a Micro-USB service
port on the lower connector board, both reachable without opening the case.

Record: sample ID, serial, adapter rating, and whether a Micro-USB port is
visible.

## Step 2: Power-on behaviour

Power the unit with nothing else connected. Observe and record:

- Time from power to first LED activity
- LED colours, positions, and animation
- Any spoken prompt or tone, and its wording
- Whether it settles into a steady state, reboots, or stalls

Wording matters. The rootfs carries prompt audio under
`usr/share/sounds/tts_en-US/`, so a recognisable phrase can identify the
firmware generation.

## Step 3: Passive network observation

Do not attempt to join it to Wi-Fi yet. On a machine attached to the same
network segment, capture broadcast traffic while the unit boots.

```bash
sudo tcpdump -i <iface> -n -s0 -w invoke-boot.pcap \
  '(port 67 or port 68 or port 5353 or port 1900) or arp'
```

Record: whether the unit requests DHCP, what hostname it presents, whether it
publishes mDNS or SSDP records, and any outbound destinations it attempts.

Two firmware generations should look different here. The older build reaches
for Cortana and RedBend OTA infrastructure. The final build has those services
removed and adds `wifi-blocker`, so it should be markedly quieter.

Also watch for `172.20.20.20`. The rootfs `serviceport.sh` configures that
address on `eth0` and announces it with gratuitous ARP.

## Step 4: Bluetooth and acoustic assessment

This is the step most likely to satisfy the end goal outright.

Put the unit into pairing mode using its normal buttons. Attempt to pair from a
phone or laptop, then play audio.

Pairing is non-destructive but not strictly read-only: it can persist a
Bluetooth bond in ordinary device settings.

Record: whether it pairs, whether audio plays, perceived quality, whether
volume buttons work, whether the mute control works, and whether the LED ring
responds.

If audio plays cleanly, the speaker, amplifier, DSP, and MCU control path are
all functioning. That is a working device, and the invasive research paths
become optional rather than necessary.

## Optional controlled factory reset

Harman's
[September 2021 release notes](https://web.archive.org/web/20240822092012id_/https://support.harmanaudio.com/us/en/howto/invoke-final-software-update-release-notes-us/000018514.html)
enable factory reset through the recessed Reset pinhole beside Mic Mute. With
the unit fully booted and USB disconnected, hold the pinhole for five seconds,
release it, and allow the restart to finish. This deletes pairings and other
writable user state.

Use this once as a controlled state-reset experiment, then repeat the ordinary
and yellow-mode USB captures. Do not hold Reset while applying power; that is a
separate early-boot action.

## Step 5: USB service-port enumeration

This step is read-only. Do not run any flashing tool.

Connect the Micro-USB service port to a Linux host and watch enumeration while
power-cycling the unit.

```bash
udevadm monitor --udev &
sudo dmesg -w &
watch -n1 lsusb
```

Record every USB identifier that appears, and the state the unit was in when it
appeared.

Identifiers of interest:

| Vendor:Product | Meaning |
|---|---|
| `1286:8100` or `1286:8101` | Marvell BootROM download mode for Monahans P/L parts |
| `1286:8174` | **BG2CDP Boot Device.** Verified: this unit presents this ID for roughly four seconds on every power-on, and `marvell_flash_tool/run.sh` targets exactly `usb_boot 1286 8174`. `Mrvl_WinUSB.inf` names it `"Marvell(R) WTP: Tools package USB Driver for BG2CDP Boot Device"`. It is a boot/download endpoint; the emitting device stage remains unknown |
| `????:0d02` | The normal runtime gadget. `init.rc` sets this product ID with the string `MRVL USB SDK`. Expected on a booted unit |
| Anything else | Record verbatim |

Record the full descriptor, not just the identifier:

```bash
lsusb -v -d <vid>:<pid> 2>/dev/null | head -40
```

Do not hold random buttons during power-on. One reachable control can invoke
factory reset. The documented service-mode entry is recorded in
[usb-service-mode.md](./usb-service-mode.md) and is the only button sequence
that should be attempted.

The `1286:8174` window appears on ordinary power-on, so the boot/download
endpoint is externally reachable without opening the case. On this unit the
observed `0xFE` session requests `08_IMAGE`, disconnects, and continues normal
boot. The component that performs that exchange remains unidentified.

## Step 6: If a normal gadget appears

If the unit enumerates as an ordinary USB device rather than the boot
downloader, try a read-only connection. The prediction is that this fails,
because `adbd` is disabled in this build.

```bash
adb devices
```

If it does answer, that falsifies the prediction and is the single most useful
outcome available from this procedure. It would make the entire recovered
control API reachable with a port forward:

```bash
adb shell cat /etc/build.info     # confirm Barracuda_libre-12.2134.0
adb shell cat /proc/mtd
adb forward tcp:9999 tcp:9999     # bus becomes reachable from the host
```

With that forward in place, the same MsgPack WAMP client used against the
emulator would drive the real speaker. See
`docs/emulation/control-plane-emulation.md` for the client and
`docs/emulation/final-firmware-control-surface.md` for why this is otherwise
blocked.

Do not mount anything read-write, do not modify the U-Boot environment, and do
not invoke recovery or update commands.

## What to bring back

Record for each step: what you did, what happened, and what did not happen.
Photographs, the pcap file, `lsusb` output, and any console text.

The two results that most change the project's direction are whether Bluetooth
audio works, and whether the BootROM downloader ever appears on USB.
