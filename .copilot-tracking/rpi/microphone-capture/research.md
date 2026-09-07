---
title: Microphone capture research
description: Evidence for a privacy-gated local microphone capture owner
---

## Research question

Add a supported local microphone stream without weakening the existing
Mic-Mute privacy boundary. The capture owner must be the only process that opens
the ALSA capture PCM, must survive DSP restarts, and must never expose
post-mute audio to a consumer.

## Proven hardware path

The live unit exposes one capture substream at card 1, device 0. The accepted
parameters are:

* stereo;
* 48,000 Hz;
* signed 32-bit little-endian samples;
* 256-frame, 2,048-byte periods; and
* 16 periods, for a 4,096-frame, 32 KiB hardware buffer.

This is not a seven-channel raw-array endpoint. The installed donor ALSA
configuration calls card 1 `dsp` and calls its capture side `dsp_mic`. It
routes:

* the left channel to `mic_reco`, described as the voice-recognition
  microphone; and
* the right channel to `mic_call`, described as the call microphone.

The installed and OTA2 `asound-product.conf` files are byte-identical. The
donor also opens `dsp_mic` with `arecord` during ALSA initialization. The
normal DSP boot route produces capture without a diagnostic `micTest*` command.

The service should therefore select the left donor voice-recognition channel.
It must describe the result as a **DSP-originated voice-recognition stream**,
not as proven beamformed or echo-cancelled audio. Harman advertised
beamforming, echo cancellation, and noise reduction, and Skype disabled its
own AGC/AEC, but no retained A/B test proves which algorithms are active in
the normal firmware route.

## Privacy authority

The MCU privacy controller is the only policy owner:

1. Physical Mic-Mute events and the compatibility WAMP API enter the same
   process-lifetime controller.
2. The controller sends DSP opcode `0x09` through the private
   `/run/reinvoke/dsp-mic-control.sock`.
3. A matching `EVENT_MIC_MUTE` response confirms the DSP transition.
4. Confirmed state is atomically written to the mode-0600
   `/run/reinvoke/microphone-state`.
5. The red privacy animation follows confirmed state.

Mute is fail-closed. The state file is written `muted` before the DSP command,
so it can temporarily mean "mute required" rather than "mute confirmed".
Unmuted is written only after DSP confirmation.

## Capture hazard

ALSA `hw_params` and trigger setup reprogram the MIC/SEC clock and I2S path.
On hardware, opening/configuring capture after an earlier startup mute restored
nonzero audio. Reasserting mute after configuration and waiting for
`EVENT_MIC_MUTE` produced exactly zero nonzero samples out of 244,736.

The safe ordering is therefore fixed:

1. configure and start capture while delivery is blocked;
2. request a fresh mute through the MCU privacy owner;
3. wait for the MCU owner to receive DSP confirmation;
4. discard every byte captured before confirmation;
5. restore the prior confirmed privacy state through the MCU owner; and
6. allow delivery only after confirmed unmute.

## DSP generation boundary

Each supervised DSP process generation clears its old boot marker, reloads
the DSP image, receives `EVENT_DSP_BOOTUP`, creates a new mic-control socket,
restores required mute, and only then publishes session readiness.

Neither the empty `dsp-booted` marker nor the WAMP `["ready"]` publication
contains a generation identifier. The capture owner must fence generations by
the DSP process identity and the mic-control socket inode. On disappearance or
change it must:

* stop delivery immediately;
* close consumer connections;
* stop and reopen capture; and
* complete the post-configuration privacy transaction before serving a new
  generation.

## Transport decision

### Unix stream socket

Selected. The native mono stream is 192,000 bytes per second. One 256-frame
period is 1,024 bytes and 5.33 ms, which is small for a local Unix stream.
A socket provides peer credentials, bounded per-client queues, explicit
generation framing, and immediate connection teardown on privacy revocation.

### Shared ring buffer

Rejected for the first version. It saves a copy that has not been measured as a
problem, while mapped readers retain access after unlink/restart and unread
pre-mute audio remains visible. Reader reclamation, memory ordering, and
generation fencing would add new privacy-critical code.

### ALSA plugin

Rejected as the privacy authority. ALSA opens and `hw_params` are the operation
that can invalidate an earlier mute route, and shared ALSA buffering has poor
revocation semantics. A plugin may later consume the owner socket for
compatibility, but it must never open the hardware node.

## Delivered format decision

The service will convert stereo interleaved `S32_LE` to the left
voice-recognition channel and deliver:

* mono;
* 48,000 Hz;
* `S32_LE`;
* one 256-frame block per packet; and
* no sample-rate conversion.

Channel selection belongs in the platform because the donor identifies the
channel semantics. Sample-rate and sample-width conversion belong in the
consumer because wake-word engines differ, conversion quality is policy, and
the platform should preserve the native DSP output.

## Capture backend

The donor ALSA 1.1.0 `arecord` binary successfully opened the live PCM with:

```text
arecord -D hw:1,0 -t raw -f S32_LE -r 48000 -c 2 \
  --period-size=256 --buffer-size=4096 -
```

Its stdout produced exactly 384,000 bytes for one second, matching
48,000 frames x 2 channels x 4 bytes. The ALSA status reported the exact
required format and `RUNNING`.

The first implementation may supervise this checksum-gated capture helper and
consume its stdout. The Go service remains the capture owner: consumers see
only its privacy-gated Unix socket and never receive the ALSA file descriptor.

## Unknowns

* Whether normal DSP output has active beamforming, AEC, AGC, or noise
  reduction.
* The physical microphone ADC and electrical topology.
* The meaning and safe restoration behavior of diagnostic `micTest*` and
  bypass controls.
* The low-level reason ALSA configuration invalidates an earlier DSP mute.

None of these unknowns blocks exposing the donor-designated voice-recognition
channel. The service and documentation must not claim those algorithms are
active until a directional or acoustic-echo A/B test proves it.
