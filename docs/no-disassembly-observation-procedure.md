# No-Disassembly Observation Procedure

Operator procedure for a physical Invoke that will not be opened. Every step
here is non-invasive and reversible. Nothing in this document writes to the
device.

This procedure is human-executed. Record results as you go; negative results
matter as much as positive ones.

## Why the order changed

Static analysis established that Harman's final firmware, `Barracuda_libre-12.2134.0`
shipped in the OTA2 bundle, removes Cortana and Spotify and adds a `wifi-blocker`
service. See `docs/bundle-contents/invoke-ota2/ota2-analysis.md`.

The practical consequence is that a unit on late firmware may already behave as
a local Bluetooth speaker. If the goal is a working speaker rather than a
research result, that outcome may be reachable with no intervention at all.
Determining which firmware a unit carries is therefore the cheapest high-value
observation available, and it comes first.

## Safety rules

- Do not open the enclosure.
- Do not flash, write, or run any update.
- Do not press-and-hold reset combinations until step 4, and only then with the
  intent of observing, not resetting.
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

Record: whether it pairs, whether audio plays, perceived quality, whether
volume buttons work, whether the mute control works, and whether the LED ring
responds.

If audio plays cleanly, the speaker, amplifier, DSP, and MCU control path are
all functioning. That is a working device, and the invasive research paths
become optional rather than necessary.

## Step 5: USB service port enumeration

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
| `1286:8100` or `1286:8101` | Marvell BootROM USB download mode |
| `1286:8174` | Post-handoff ramdisk gadget with ADB |
| Anything else | Record verbatim |

Repeat the power-on capture while holding each externally reachable button, one
at a time, then in pairs. Three switches are reachable through the enclosure's
normal buttons.

This step is a hard gate. If `1286:8100` or `1286:8101` never appears, the
USB RAM-boot path is not reachable without opening the case, and that avenue
closes. Recording that clearly is a genuine result.

## Step 6: If a normal gadget appears

If the unit enumerates as an ordinary USB device with ADB rather than the
BootROM downloader, try a read-only connection.

```bash
adb devices
adb shell cat /proc/mtd
adb shell cat /etc/build.info
adb shell dmesg
```

`etc/build.info` identifies the firmware generation precisely. Compare
`BUILD_GIT_TAGS` against the two known builds, `Barracuda_libre-11.1842.0` and
`Barracuda_libre-12.2134.0`.

Do not mount anything read-write, do not modify the U-Boot environment, and do
not invoke recovery or update commands.

## What to bring back

Record for each step: what you did, what happened, and what did not happen.
Photographs, the pcap file, `lsusb` output, and any console text.

The two results that most change the project's direction are whether Bluetooth
audio works, and whether the BootROM downloader ever appears on USB.
