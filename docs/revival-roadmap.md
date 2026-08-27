# reInvoke Revival Roadmap

## End state

Reach **repurposing completeness (L2)**: a documented, reproducible way to
reuse the Invoke enclosure, speakers, microphones, UI, and/or compute module,
with every required electrical and protocol assumption marked as proven,
measured, or unresolved. The first practical target is not a complete Cortana
replacement; it is a safe local network/audio/control stack that can preserve
the useful hardware.

## Stages

### 1. Preservation and firmware map — complete

Acquire, hash, mirror, classify, and extract the firmware without executing or
flashing it. Maintain the claim/evidence ledger and keep binaries outside Git.

### 2. Software boundary map — complete for static evidence

Documented boot, storage, OTA, services, ports, IPC, audio, Bluetooth, Wi-Fi,
MCU, and UI boundaries from static evidence. The hardware-side transport and
electrical details remain unproven.

### 3. Safe observation on one physical sample — ready to start

Donor device(s) are available. Assign a sample ID and collect board/connector
photographs, labels, and non-invasive power/continuity measurements. Capture
serial boot output and read-only runtime observations. Do not flash until a
recovery and image-integrity procedure is independently established. See
`docs/hardware-validation-plan.md` for the full procedure and the
no-disassembly fallback path if a given unit cannot be opened.

### 4. Interface validation

Resolve the daughterboard connector pinout, rails, reset/enable signals,
digital-audio transport, MCU control protocol, UI transport, and microphone
path. Use a logic analyzer or oscilloscope only after the wiring and voltage
limits are known.

### 5. Reuse decision

- **Keep BG2CDP:** pursue a maintained userland or minimal service replacement
  if boot, storage, network, and audio paths are usable.
- **Replace compute:** design a compatible module only after connector,
  power, audio, and control interfaces are measured.
- **Bypass electronics:** use only if the stock audio/control path cannot be
  commanded and driver/acoustic parameters justify a new design.

### 6. Minimal revival demonstrator

Build the smallest testable stack: local playback, volume/mute, LED/UI
feedback, Bluetooth or network input, and safe shutdown. Keep voice assistant
and cloud dependencies optional. Record reproducible setup and test results.

### 7. Hardening and preservation release

Publish interface evidence, scripts, measurements, compatibility limits,
recovery procedures, and a clear list of unknowns. Keep proprietary binaries
as referenced evidence rather than presenting them as a replacement software
distribution.

## Autonomous boundary

Repository analysis, static reverse engineering, metadata extraction,
documentation, and public-source discovery can proceed autonomously. Physical
measurements, device modification, firmware flashing, credential use, and
redistribution decisions require an explicit human-controlled test setup.
