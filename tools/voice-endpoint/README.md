---
title: Donor wake endpoint and fixed-reply host
description: Run the retained Hey Cortana detector with an outward host connection and existing reInvoke controls
---

## Scope

The speaker reuses the retained donor executable and original US wake model.
It does not run the original Cortana application or contact the retired service.
The owned worker supplies microphone audio, handles the wake and reply, and
rearms. The host catcher returns one supplied WAV; it performs no transcription,
assistant reasoning, or speech generation.

The earlier fixed-five-second batch candidate is withdrawn. The device-side
contract is continuing audio with backend-controlled speech and turn endings.
The donor's five-second history ring is pre-wake storage, not a command timer.

The approved release target is 2.3.0. Earlier private build labels were
experimental iterations, not approved releases or permission to install them.

`reinvoke-voice` connects outward to a configured host. It does not require the
host to know the speaker's DHCP address or launch a command over SSH. The
existing microphone, playback and MCU services retain hardware ownership.

## Host setup

The catcher needs Python 3.11 or newer. OpenSSL is needed once to generate its
private identity. The identity directory must not already exist:

```bash
python3 tools/voice-endpoint/init_host.py \
  --directory "$HOST_STATE" \
  --endpoint "reinvoke-host:8444" \
  --device "invoke-living-room"
```

This creates `host-cert.pem`, `host-key.pem`, `token` and `device.json` with
private permissions. Standard output contains only the certificate fingerprint.
Do not commit these files or include the token in command lines.

Use a supplied, previously auditioned WAV:

```bash
python3 tools/voice-endpoint/catcher.py \
  --bind "$HOST_LAN_ADDRESS" --port 8444 \
  --cert "$HOST_STATE/host-cert.pem" \
  --key "$HOST_STATE/host-key.pem" \
  --token-file "$HOST_STATE/token" \
  --reply-wav "$APPROVED_REPLY_WAV" \
  --stub-end-after-ms 8000
```

`--stub-end-after-ms` is an explicit test-backend decision, not device
configuration or voice-activity detection. The stub sends a speech-end event
after that much post-wake audio. A real backend replaces this decision with its
own speech endpointing without changing the speaker's recording policy.

The catcher does not save received audio. It logs bounded per-turn metadata and
results. Normal mode accepts only live-microphone sessions with playback enabled.
`--allow-diagnostics` additionally permits fixture input or validation without
playback; status `8` then means validated-only, never audible success.

## Host names and configuration

The device configuration contains a stable endpoint name, not an application
constant for one computer's IP. Supply its address through the speaker's
ordinary `/etc/hosts`, for example:

```text
127.0.0.1 localhost
192.0.2.10 reinvoke-host
```

The example address must be replaced with the actual host address. Ensure the
alias exists before enabling voice. No DNS service or discovery protocol needs
to be installed.

Native startup retains `/etc/hosts -> /etc/tmpfs/hosts`, populating the RAM file
from the image seed or an existing `/persist/hosts` override. It does not write
that override. Changing the RAM file is temporary; changing the persistent
override is a settings write to NAND, not a firmware reflash, and requires the
owner's approval.

Keep the generated host identity when moving the catcher to another computer.
Then only the address mapping changes. Replacing its certificate or token
requires updating the device configuration as well.

The private `device.json` supplies `endpoint`, `certificate_sha256`, `token`,
`device` and `io_timeout_ms`. It installs as
`/etc/reinvoke-voice/voice.json` with mode `0600`, a real file in its own
`/etc` directory rather than a link. It deliberately avoids `/etc/reinvoke`,
which the bootstrap bind mounts read-only from the image before chroot: a
configuration written there is invisible at runtime. The first 2.3.0 flash
made that mistake and the endpoint stayed stopped. The connection uses TLS with an exact
leaf-certificate pin and token authentication. The pin replaces CA/date
validation because the speaker does not have a trustworthy wall clock.

Old `post_ms` configuration is rejected instead of silently retaining a device
recording cutoff.

## Building and starting

Provide the original checksum-manifested private donor bundle. Its executable
and model must match the reviewed pins; libraries are checked against its
manifest. No vendor payload is downloaded or placed in the source repository.

```bash
tools/voice-endpoint/build.sh \
  --donor-bundle "$DONOR_BUNDLE" \
  --output-dir "$VOICE_PAYLOAD" \
  --go "$GO"
```

The output directory must be absent or empty. The helper builds the owned
connector and ARM worker, and stages only the required isolated donor inputs.
It requires Go 1.18 or newer and ARM hard-float GCC.

Native composition accepts `PILOT_VOICE_DONOR_BUNDLE`, `PILOT_VOICE_CONFIG` and
`PILOT_HOSTS_FILE`; see the [native builder](../nand-pilot/README.md).
The image installs the connector under `/opt/reinvoke/bin`, isolated inputs
under `/opt/reinvoke/voice`, and starts it through the existing supervisor.
Building a candidate does not install it or authorize a NAND write.

For an explicitly staged RAM test, copy `device.json` to `$DEVICE_CONFIG` on
the speaker. Do not copy the host's private key:

```bash
"$VOICE_PAYLOAD/bin/reinvoke-voice" \
  --config "$DEVICE_CONFIG" \
  --bundle "$VOICE_PAYLOAD/voice" \
  --runtime-dir "$RAM_RUNTIME"
```

Paths in that command are on the speaker. Normal operation has no experiment
run timer. Diagnostic `--once`, `--seconds`, `--fixture` and `--no-playback`
options are for bounded tests, not startup configuration.

The connector paces diagnostic file input at microphone rate. An unpaced file
can otherwise overrun the bounded transport queue even though live input does
not. This affects fixture replay only, not normal microphone capture.

## Turn behavior and limits

The connector reports voice phases through the original
`com.harman.extStateUpdate` interface. The MCU owner drives the retained
`L_101_c_listening`, `L_104_c_thinking` and `L_105_c_cortanaspeaking` animations
and plays the original `listening.wav` and `processing.wav` cues. The processing
cue waits 1,350 ms and is cancelled if speaking or idle arrives first.

The connector renews active voice state every two seconds. A ten-second lease
lets the MCU clean up cues, lights and music attenuation if the connector
disappears. This local lease is distinct from the backend session watchdog.

Music attenuation uses only the existing music softvol control, not shared
DSP gain or the voice output. Its soft attenuation strength is an existing
project choice, not a recovered original ratio. The previous music level is
restored without overwriting a later independent change. Master volume remains
unchanged.

The connector subscribes to the existing resolved WAMP button-action topic.
A long top press triggers capture; a short top press while voice is active
cancels the turn. Only the verified managed worker receives the corresponding
signal. Ordinary music pause remains with the existing media owner. Microphone
mute still takes priority over voice cues and lights; the boundary is polled,
not a synchronous privacy fence.

After a local wake the worker sends the donor's buffered PCM and then continues
streaming live audio. It does not wait to fill a five-second batch and does not
decide that a spoken command has ended. The backend supplies speech-end,
turn-end, cancellation and optional follow-up-listening controls.

The recovered client enables KWS on speech end, when it moves to thinking.
Its speaking-state transition preserves that setting. The worker follows that
division: a new accepted wake can replace an outstanding turn. It does not
claim to reproduce the retired server's endpoint algorithm or the original
acoustic echo-suppression behavior.

The existing polled microphone authority still applies. Muting clears pending
audio and stops playback. A transport or capture failure cancels the session
rather than fabricating continuity. The session watchdog is refreshed by
backend activity; it is not a fixed maximum spoken-command duration.

Reply bytes are not interpreted as words or checked against the test sentence.
The worker validates the message identity, PCM/WAV structure and configured
safety bounds, then plays the audio. A backend `turn.end` arriving before that
audio finishes must not truncate playback.

### Stream protocol

The authenticated hello declares `protocol: RIWAKE03`, device, session nonce,
token, source and playback mode. There is no post-wake duration field.
Version 2 batch peers are not compatible with this streaming contract.

Messages remain little-endian and length-delimited:

| Message     | Direction       | Payload |
| ----------- | --------------- | ------- |
| `RIWAKE03`  | Device to host  | Original 64-byte metadata layout, version 3, followed by the buffered wake PCM only |
| `RIAUDIO3`  | Device to host  | 24-byte header: magic, turn ID, sequence, byte count; then continuing S16 PCM |
| `RICTRL03`  | Host to device  | 24-byte header: magic, turn ID, control code, zero byte count |
| `RIREPLY3`  | Host to device  | 24-byte header: magic, turn ID, verdict, WAV byte count; then reply bytes |
| `RIRESULT`  | Device to host  | Existing 24-byte playback/cancellation receipt and reply hash |

Audio is mono 16 kHz S16 PCM. The wake window is bounded to 160,000 bytes;
continuing frames are bounded to 3,200 bytes and carry increasing sequence
numbers. A zero-length audio frame marks end of audio, including the explicit
EOF of a diagnostic fixture. It is not a transcript or a silence judgment.

Control codes are `1` speech start, `2` speech end, `3` turn end, `4` cancel,
`5` follow-up listen and `6` speech progress. Their semantics model the
recovered client's session events, not the retired Microsoft wire protocol.
A follow-up turn uses a fresh ID and metadata flag bit 2, with no wake PCM or
invented keyword confidence.

Speech replies use mono 16 kHz signed 16-bit PCM in a WAV container, a format
explicitly requested by a recovered original Cortana TTS path. This is a
speech-interface choice, not the Invoke hardware's only supported format.
The worker plays through the existing `voice` PCM, including its donor speech
EQ, and uses a 64 KiB streaming ring instead of waiting for the complete reply.
There is no 60-second reply cap or test-waveform peak rejection. Legal S16
samples are passed unchanged; user volume remains with its existing owner.

The current wire header still declares the WAV's total byte count with a
32-bit length. Known-length replies can stream; unknown-length TTS is not
implemented. RIFF structure and mono 16 kHz S16 format remain validated.
Malformed or stalled streams stop playback and report failure, even if an
earlier valid prefix has already played. That is not a successful full reply.

A new connection receives a fresh nonce. Broken sessions are discarded, not
replayed. The connector retries with backoff; a missing host is not a reason to
reboot or change Wi-Fi settings. The stub and a future daemon must agree on
these turn semantics, not merely on field names.

## Earlier transport prototype verification

The version 2 batch payload was staged in RAM on the unit running 2.2.11.
The following were observed without installing new firmware:

* The speaker resolved the configured alias through `/etc/hosts`, authenticated
  the host, and sent fixture-triggered audio with an exact matching PCM digest.
* One approved reply completed playback at unchanged volume 30. Hardware
  playback status changed from `closed` to `RUNNING` and back, and host/worker
  reply hashes matched. This was not a new human audibility test.
* An eight-second live-microphone run consumed exactly 128,000 converted
  samples and ended cleanly. No positive utterance was requested during it.
* Stopping the host reaped the donor worker. The same connector stayed alive,
  backed off through connection refusals, then created a new worker and reopened
  capture when the host returned.
* The actual MCU rejected boolean animation repetition and accepted numeric
  `1`. The connector now uses that existing numeric contract. All three
  animations were accepted; human visual confirmation remains separate.

The private 2.3.0 main filesystem, paired BSL and complete bundle were built
and checked offline, then withdrawn because the fixed capture policy did not
match the owner's original-experience requirement. Those measurements remain
valid for that prototype; they do not validate the new streamed lifecycle.
Installed 2.2.11 remains unchanged. Temporary listeners, RAM configuration and
staged payloads were removed after testing.

## Streamed lifecycle verification

The corrected payload was tested from RAM on the same native 2.2.11 unit.
No firmware installation was performed.

* Changing only the host's endpoint decision from eight to twelve seconds
  produced 8.00 and 12.01 seconds of post-wake audio, respectively. The extra
  10 ms frame was in flight before the stop was processed. Both complete PCM
  digests matched the source fixture; the device binary and configuration were
  unchanged.
* The worker logged backend `speech.endDetected` as the stopping event and
  rearmed KWS while thinking. It did not stop because a local batch filled.
* An early backend `turn.end` did not cut off the approved reply. The player
  completed all 59,214 PCM bytes, hardware playback went
  `closed -> RUNNING -> closed`, and volume stayed at 30.
* Changing only the backend reply to a different, silent WAV also completed
  playback with its own matching hash. The speaker did not require the test
  sentence or its original hash.
* A final live-microphone check consumed exactly 128,000 converted samples in
  eight seconds. The streamed positive checks used a retained fixture; they
  are not a new human-spoken acceptance test.

The first unpaced fixture test failed explicitly at the 320,000-byte outgoing
queue bound. It was retained as a failure. The correction was diagnostic
real-time pacing, not a larger queue or silent audio loss.

That prototype's worker passed 29 actual-donor QEMU tests, including endpoint,
progress, pending turn-end, follow-up, interruption, mute, backpressure and
playback controls. Those isolate external boundaries and do not establish
the retired backend's speech endpoint algorithm or acoustic echo cancellation.

Those native-tested binaries were packaged in a private iteration labelled
2.3.1. It was not an approved release. Its offline checks remain evidence for
that exact snapshot, not acceptance of the subsequently approved 2.3.0 scope.
Installed 2.2.11 remains unchanged.

## Approved 2.3.0 build verification

The completed build includes the streamed reply worker, physical-action
subscriber, MCU feedback owner, original cues and music-only attenuation.
Native validation staged these components in RAM on the existing installation:

* The existing resolved WAMP long-action started capture without a keyword,
  and its short-action cancelled the turn. An unrelated event did not trigger
  capture. This exercised the action route, not a new physical button press.
* A 70-second silent response played all 2,240,000 PCM bytes, exceeding the
  removed test limit. Playback started before receipt of the whole response.
  The host and worker reported the same hash.
* Music softvol attenuated and then returned to its exact prior value. Voice
  and system controls stayed unchanged, as did master volume 30. This was a
  control measurement, not an acoustic music-mixing assessment.
* A thinking update produced no immediate cue, then started the original
  processing cue after the delay. An idle update stopped and reaped it.
* A shutdown-only control-call race was reproduced in a failing unit test and
  fixed without treating a cancelled attempt as completed cleanup. Background
  cleanup still runs, and actual control failures remain reported.

The original MCU binary was restored byte-for-byte after the RAM test, and all
new test files and listeners were removed. The complete candidate was built
offline, with two byte-identical main filesystem builds and component hashes
matching the native-tested snapshot. It is not installed or cold-boot verified;
firmware flashing requires separate approval.
