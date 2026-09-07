// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import { once } from "node:events";
import net from "node:net";
import test from "node:test";

import {
  PROCEDURES,
  SpeakerControlController,
  invokeProcedure,
  serveSpeakerControl,
} from "./speaker-control-service.mjs";
import { SpeakerControlState } from "./speaker-control-state.mjs";
import {
  SocketReader,
  readFrame,
  write,
  writeFrame,
} from "./wamp-call.mjs";

test("dispatches the observed procedures without device side effects", () => {
  const state = new SpeakerControlState({ musicVolume: 30 });

  assert.deepEqual(
    invokeProcedure(state, "com.harman.volumeSet", [35, "music"]).args,
    [35, "music"],
  );
  assert.deepEqual(
    invokeProcedure(
      state,
      "com.harman.extStateUpdate",
      ["bluetooth"],
      { state: "playing" },
    ).events[0].args,
    ["bluetooth"],
  );
  assert.throws(
    () => invokeProcedure(state, "com.harman.volumeSet", ["loud", "music"]),
    /invalid argument format/,
  );
});

test("routes live volume, mute, source, and transport through an injected backend", async () => {
  const backendState = {
    pcmAvailable: true,
    volume: 25,
    muted: false,
    sourceConnected: true,
    transportState: "playing",
  };
  const writes = [];
  const backend = {
    async read() {
      return { ...backendState };
    },
    async setVolume(volume) {
      writes.push(["volume", volume]);
      backendState.volume = volume;
    },
    async setMuted(muted) {
      writes.push(["muted", muted]);
      backendState.muted = muted;
    },
  };
  const state = new SpeakerControlState({ musicVolume: 90 });
  const controller = new SpeakerControlController(state, backend);

  const initial = await controller.invoke("com.harman.volumeGet");
  assert.deepEqual(initial.kwargs.music, { mute: 0, volume: 25 });
  assert.deepEqual(state.sourceGetActive().args, ["com.harman.bluetooth"]);
  assert.equal(state.stateGet().kwargs.bluetooth.state, "playing");

  assert.deepEqual(
    (await controller.invoke("com.harman.volumeAdjust", [10, "music"])).args,
    [35, "music"],
  );
  assert.deepEqual(
    (await controller.invoke("com.harman.volumeAdjust", [-0.8, "music"])).args,
    [35, "music"],
  );
  assert.deepEqual(
    (await controller.invoke("com.harman.musicMuteToggle")).args,
    [true, "music"],
  );
  assert.deepEqual(writes, [
    ["volume", 35],
    ["volume", 35],
    ["muted", true],
  ]);
});

test("serializes concurrent live backend adjustments", async () => {
  const backendState = {
    pcmAvailable: true,
    volume: 25,
    muted: false,
  };
  const writes = [];
  const backend = {
    async read() {
      return { ...backendState };
    },
    async setVolume(volume) {
      writes.push(volume);
      backendState.volume = volume;
    },
    async setMuted(muted) {
      backendState.muted = muted;
    },
  };
  const controller = new SpeakerControlController(
    new SpeakerControlState(),
    backend,
  );

  await Promise.all([
    controller.invoke("com.harman.volumeAdjust", [1]),
    controller.invoke("com.harman.volumeAdjust", [1]),
  ]);
  assert.deepEqual(writes, [26, 27]);
});

test("does not use stale volume when the live PCM is absent", async () => {
  const backend = {
    async read() {
      return { pcmAvailable: false };
    },
    async setVolume() {
      throw new Error("unexpected write");
    },
    async setMuted() {
      throw new Error("unexpected write");
    },
  };
  const controller = new SpeakerControlController(
    new SpeakerControlState({ musicVolume: 90 }),
    backend,
  );

  await assert.rejects(
    () => controller.invoke("com.harman.volumeGet"),
    /PCM is not available/,
  );
});

test("dispatches interleaved startup acknowledgements by type and request", async (t) => {
  const controller = new AbortController();
  const server = net.createServer();
  t.after(() => {
    controller.abort();
    server.close();
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const address = server.address();

  const serviceSocketPromise = once(server, "connection").then(
    ([socket]) => socket,
  );
  let notifyReady;
  const ready = new Promise((resolve) => {
    notifyReady = resolve;
  });
  const servicePromise = serveSpeakerControl({
    host: "127.0.0.1",
    port: address.port,
    musicVolume: 30,
    signal: controller.signal,
    onReady: notifyReady,
  });
  const socket = await serviceSocketPromise;
  const reader = new SocketReader(socket);

  assert.deepEqual(await reader.read(4), Buffer.from([0x7f, 0xf2, 0, 0]));
  await write(socket, Buffer.from([0x7f, 0xf2, 0, 0]));
  assert.equal((await readFrame(reader))[0], 1);
  await writeFrame(socket, [2, 9001, { roles: { dealer: {}, broker: {} } }]);

  const requests = [];
  for (let index = 0; index < PROCEDURES.length + 1; index += 1) {
    requests.push(await readFrame(reader));
  }
  const subscribe = requests.find((message) => message[0] === 32);
  const registers = requests.filter((message) => message[0] === 64);
  assert.equal(registers.length, PROCEDURES.length);
  assert.equal(subscribe[3], "com.harman.test.inputEvent");

  await writeFrame(socket, [33, subscribe[1], 500]);
  await writeFrame(socket, [36, 500, 800, {}, ["volumeup", "1"], {}]);
  const registrationIds = new Map();
  for (const [index, register] of registers.reverse().entries()) {
    const registrationId = 1000 + index;
    registrationIds.set(register[3], registrationId);
    await writeFrame(socket, [65, register[1], registrationId]);
  }

  await ready;
  const publication = await readFrame(reader);
  assert.equal(publication[0], 16);
  assert.equal(publication[3], "com.harman.volumeChanged");
  assert.deepEqual(publication[4], ["music", 31]);

  await writeFrame(socket, [
    68,
    700,
    registrationIds.get("com.harman.volumeGet"),
    {},
    [],
    {},
  ]);
  const result = await readFrame(reader);
  assert.deepEqual(result.slice(0, 2), [70, 700]);
  assert.deepEqual(result[4].music, { mute: 0, volume: 31 });

  controller.abort();
  await servicePromise;
  socket.destroy();
});

test("registers and serves the compatibility surface over WAMP", async (t) => {
  const controller = new AbortController();
  const server = net.createServer();
  t.after(() => {
    controller.abort();
    server.close();
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const address = server.address();

  const serviceSocketPromise = once(server, "connection").then(
    ([socket]) => socket,
  );
  let notifyReady;
  const ready = new Promise((resolve) => {
    notifyReady = resolve;
  });
  const servicePromise = serveSpeakerControl({
    host: "127.0.0.1",
    port: address.port,
    musicVolume: 30,
    bluetoothActive: true,
    signal: controller.signal,
    onReady: notifyReady,
  });
  const socket = await serviceSocketPromise;
  const reader = new SocketReader(socket);

  assert.deepEqual(await reader.read(4), Buffer.from([0x7f, 0xf2, 0, 0]));
  await write(socket, Buffer.from([0x7f, 0xf2, 0, 0]));
  const hello = await readFrame(reader);
  assert.equal(hello[0], 1);
  await writeFrame(socket, [2, 9001, { roles: { dealer: {}, broker: {} } }]);

  const registrations = new Map();
  for (let index = 0; index < PROCEDURES.length; index += 1) {
    const register = await readFrame(reader);
    assert.equal(register[0], 64);
    const registrationId = 100 + index;
    registrations.set(register[3], registrationId);
    await writeFrame(socket, [65, register[1], registrationId]);
  }
  const subscribe = await readFrame(reader);
  assert.deepEqual(subscribe.slice(0, 2), [32, PROCEDURES.length + 1]);
  assert.equal(subscribe[3], "com.harman.test.inputEvent");
  await writeFrame(socket, [33, subscribe[1], 500]);
  await ready;

  await writeFrame(socket, [
    68,
    700,
    registrations.get("com.harman.volumeSet"),
    {},
    [35, "music"],
    {},
  ]);
  const publication = await readFrame(reader);
  assert.equal(publication[0], 16);
  assert.equal(publication[3], "com.harman.volumeChanged");
  assert.deepEqual(publication[4], ["music", 35]);
  const volumeResult = await readFrame(reader);
  assert.deepEqual(volumeResult.slice(0, 2), [70, 700]);
  assert.deepEqual(volumeResult[3], [35, "music"]);
  assert.deepEqual(volumeResult[4].music, { mute: 0, volume: 35 });

  await writeFrame(socket, [
    68,
    701,
    registrations.get("com.harman.source.get-active"),
    {},
    [],
    {},
  ]);
  const sourceResult = await readFrame(reader);
  assert.deepEqual(sourceResult, [
    70,
    701,
    {},
    ["com.harman.bluetooth"],
    {},
  ]);

  await writeFrame(socket, [36, 500, 800, {}, ["volumedown", "1"], {}]);
  const rotaryPublication = await readFrame(reader);
  assert.equal(rotaryPublication[3], "com.harman.volumeChanged");
  assert.deepEqual(rotaryPublication[4], ["music", 34]);

  controller.abort();
  await servicePromise;
  socket.destroy();
});
