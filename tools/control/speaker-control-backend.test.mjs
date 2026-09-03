// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import test from "node:test";

import {
  a2dpVolumeToPercent,
  BackendUnavailableError,
  BlueAlsaCliBackend,
  parseBlueAlsaPcmInfo,
  percentToA2dpVolume,
} from "./speaker-control-backend.mjs";

const STEREO_INFO = `Device: /org/bluez/hci0/dev_00_11_22_33_44_55
Sequence: 1
Transport: A2DP-sink
Mode: source
Format: S16_LE
Channels: 2
Sampling: 44100 Hz
SoftVolume: Y
Volume: L: 64 R: 64
Muted: L: N R: N
`;

test("maps every public percentage to a stable A2DP round trip", () => {
  for (let percent = 0; percent <= 100; percent += 1) {
    const raw = percentToA2dpVolume(percent);
    assert.ok(raw >= 0 && raw <= 127);
    assert.equal(a2dpVolumeToPercent(raw), percent);
  }
});

test("maps every A2DP value with nearest-integer rounding", () => {
  for (let raw = 0; raw <= 127; raw += 1) {
    assert.equal(
      a2dpVolumeToPercent(raw),
      Math.round((raw * 100) / 127),
    );
  }
});

test("rejects values outside either volume domain", () => {
  for (const value of [-1, 0.5, 101, Number.NaN]) {
    assert.throws(() => percentToA2dpVolume(value), RangeError);
  }
  for (const value of [-1, 0.5, 128, Number.NaN]) {
    assert.throws(() => a2dpVolumeToPercent(value), RangeError);
  }
});

test("parses BlueALSA 4 stereo and mono info", () => {
  assert.deepEqual(parseBlueAlsaPcmInfo(STEREO_INFO), {
    channels: 2,
    transport: "A2DP-sink",
    rawVolume: 64,
    volume: 50,
    muted: false,
  });
  assert.deepEqual(
    parseBlueAlsaPcmInfo(
      STEREO_INFO.replace("Channels: 2", "Channels: 1")
        .replace("Volume: L: 64 R: 64", "Volume: 127")
        .replace("Muted: L: N R: N", "Muted: Y"),
    ),
    {
      channels: 1,
      transport: "A2DP-sink",
      rawVolume: 127,
      volume: 100,
      muted: true,
    },
  );
});

test("rejects ambiguous, malformed, and non-A2DP PCM information", () => {
  assert.throws(
    () => parseBlueAlsaPcmInfo(STEREO_INFO.replace("R: 64", "R: 63")),
    /channel volumes differ/,
  );
  assert.throws(
    () => parseBlueAlsaPcmInfo(STEREO_INFO.replace("R: N", "R: Y")),
    /channel mute states differ/,
  );
  assert.throws(
    () => parseBlueAlsaPcmInfo(STEREO_INFO.replace("A2DP-sink", "HFP-HF")),
    /unsupported/,
  );
  assert.throws(
    () => parseBlueAlsaPcmInfo(STEREO_INFO.replace("Muted:", "Unknown:")),
    /incomplete/,
  );
});

test("uses only an explicit PCM path and updates both channels", async () => {
  const calls = [];
  const run = async (command, args) => {
    calls.push([command, args]);
    return { code: 0, stdout: STEREO_INFO, stderr: "" };
  };
  const backend = new BlueAlsaCliBackend({
    pcmPath: "/verified/pcm/path",
    command: "/verified/bluealsactl",
    dbusSuffix: "hci0",
    run,
    observe: async () => ({
      sourceConnected: true,
      transportState: "playing",
    }),
  });

  assert.deepEqual(await backend.read(), {
    pcmAvailable: true,
    volume: 50,
    muted: false,
    sourceConnected: true,
    transportState: "playing",
  });
  await backend.setVolume(50);
  await backend.setMuted(true);

  assert.deepEqual(calls, [
    [
      "/verified/bluealsactl",
      ["--dbus=hci0", "info", "/verified/pcm/path"],
    ],
    [
      "/verified/bluealsactl",
      ["--dbus=hci0", "volume", "/verified/pcm/path", "64", "64"],
    ],
    [
      "/verified/bluealsactl",
      ["--dbus=hci0", "mute", "/verified/pcm/path", "y", "y"],
    ],
  ]);
});

test("accepts an injected target-verified volume mapping", async () => {
  const calls = [];
  const backend = new BlueAlsaCliBackend({
    pcmPath: "/verified/pcm/path",
    run: async (_command, args) => {
      calls.push(args);
      return { code: 0, stdout: STEREO_INFO, stderr: "" };
    },
    toBackendVolume: (volume) => volume,
    fromBackendVolume: (volume) => volume - 4,
  });

  assert.equal((await backend.read()).volume, 60);
  await backend.setVolume(60);
  assert.deepEqual(calls[1], [
    "volume",
    "/verified/pcm/path",
    "60",
    "60",
  ]);
});

test("reports a missing PCM without inventing source or transport state", async () => {
  const backend = new BlueAlsaCliBackend({
    pcmPath: "/verified/pcm/path",
    run: async () => ({ code: 1, stdout: "", stderr: "not found" }),
  });

  assert.deepEqual(await backend.read(), { pcmAvailable: false });
  await assert.rejects(() => backend.setVolume(20), BackendUnavailableError);
  await assert.rejects(() => backend.setMuted(false), BackendUnavailableError);
});

test("validates injected source and transport observations", async () => {
  for (const observation of [
    null,
    { sourceConnected: "yes" },
    { transportState: false },
  ]) {
    const backend = new BlueAlsaCliBackend({
      pcmPath: "/verified/pcm/path",
      run: async () => ({ code: 0, stdout: STEREO_INFO, stderr: "" }),
      observe: async () => observation,
    });
    await assert.rejects(() => backend.read(), TypeError);
  }
});
