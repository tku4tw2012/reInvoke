// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import test from "node:test";

import { SpeakerControlState } from "./speaker-control-state.mjs";

test("reports the observed audio-ui volume result shape", () => {
  const state = new SpeakerControlState({ musicVolume: 30 });

  assert.deepEqual(state.volumeGet(), {
    args: [],
    kwargs: {
      music: { mute: 0, volume: 30 },
      system: { mute: 0, volume: 70 },
    },
    events: [],
  });
});

test("clamps volume setters and preserves value-first results", () => {
  const state = new SpeakerControlState({ musicVolume: 30 });

  assert.deepEqual(state.volumeSet(101).args, [100, "music"]);
  assert.deepEqual(state.volumeSet(-1).args, [0, "music"]);
  assert.deepEqual(state.volumeAdjust(200).args, [100, "music"]);
});

test("publishes effective zero volume before mute state", () => {
  const state = new SpeakerControlState({ musicVolume: 35 });
  const result = state.musicMuteSet(true);

  assert.deepEqual(result.args, [true, "music"]);
  assert.deepEqual(result.kwargs.music, { mute: 1, volume: 35 });
  assert.deepEqual(result.events, [
    {
      topic: "com.harman.volumeChanged",
      args: ["music", 0],
      kwargs: {},
    },
    {
      topic: "com.harman.musicMuteChanged",
      args: [true],
      kwargs: {},
    },
  ]);
});

test("updates stream state and publishes the full state map", () => {
  const state = new SpeakerControlState();
  const result = state.extStateUpdate("bluetooth", "playing");
  const expected = {
    alert: { priority: "5", state: "" },
    "alert-type": { priority: "", state: "" },
    bluetooth: { priority: "5", state: "playing" },
    call: { state: "" },
    microphone: { priority: "4", state: "" },
    music: { state: "" },
    system: { state: "" },
    voice: { priority: "5", state: "" },
  };

  assert.deepEqual(result.args, []);
  assert.equal(result.events[0].topic, "com.harman.stateChanged");
  assert.deepEqual(result.events[0].args, ["bluetooth"]);
  assert.deepEqual(result.events[0].kwargs, expected);
  assert.deepEqual(state.stateGet().kwargs, expected);
});

test("models the observed source registry and activation contracts", () => {
  const state = new SpeakerControlState();

  assert.deepEqual(state.sourceGetRegistered().args, []);
  assert.deepEqual(state.sourceGetActive().args, [""]);
  state.registerBluetoothSource();
  assert.deepEqual(state.sourceGetRegistered().args, ["com.harman.bluetooth"]);
  assert.deepEqual(state.sourceGetActive().args, ["com.harman.bluetooth"]);
});

test("rejects activation of an unregistered source", () => {
  const state = new SpeakerControlState();

  assert.throws(
    () => state.sourceStart("com.harman.bluetooth"),
    /source is not registered/,
  );
});

test("reconciles an authoritative backend snapshot and event ordering", () => {
  const state = new SpeakerControlState({ musicVolume: 30 });
  const events = state.reconcileBackend({
    pcmAvailable: true,
    volume: 40,
    muted: true,
    sourceConnected: true,
    transportState: "playing",
  });

  assert.deepEqual(state.volumeGet().kwargs.music, { mute: 1, volume: 40 });
  assert.deepEqual(state.sourceGetActive().args, ["com.harman.bluetooth"]);
  assert.deepEqual(
    events.map((event) => event.topic),
    [
      "com.harman.volumeChanged",
      "com.harman.musicMuteChanged",
      "com.harman.stateChanged",
    ],
  );
  assert.deepEqual(events[0].args, ["music", 0]);

  state.reconcileBackend({
    pcmAvailable: false,
    sourceConnected: false,
    transportState: "",
  });
  assert.deepEqual(state.sourceGetRegistered().args, []);
  assert.deepEqual(state.sourceGetActive().args, [""]);
  assert.deepEqual(state.volumeGet().kwargs.music, { mute: 1, volume: 40 });
});

test("rejects malformed backend snapshots without changing state", () => {
  const invalidSnapshots = [
    {},
    { pcmAvailable: "yes" },
    { pcmAvailable: true, volume: 20 },
    { pcmAvailable: true, volume: 20.5, muted: false },
    { pcmAvailable: true, volume: 20, muted: "no" },
    { pcmAvailable: false, sourceConnected: "yes" },
    { pcmAvailable: false, transportState: false },
  ];

  for (const snapshot of invalidSnapshots) {
    const state = new SpeakerControlState({ musicVolume: 30 });
    assert.throws(() => state.reconcileBackend(snapshot), TypeError);
    assert.deepEqual(state.volumeGet().kwargs.music, { mute: 0, volume: 30 });
  }
});
