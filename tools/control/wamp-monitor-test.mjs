#!/usr/bin/env node
// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT

import { EventEmitter, once } from "node:events";
import net from "node:net";
import process from "node:process";

import {
  SocketReader,
  readFrame,
  write,
  writeFrame,
} from "./wamp-call.mjs";
import { monitorTopics } from "./wamp-monitor.mjs";

const TEST_TOPIC = "com.harman.test.inputEvent";

async function withServer(handler, test) {
  const sockets = new Set();
  const server = net.createServer((socket) => {
    sockets.add(socket);
    socket.once("close", () => sockets.delete(socket));
    handler(socket).catch((error) => socket.destroy(error));
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  try {
    await test(server.address().port);
  } finally {
    for (const socket of sockets) {
      socket.destroy();
    }
    await new Promise((resolve, reject) => {
      server.close((error) => (error ? reject(error) : resolve()));
    });
  }
}

async function expectBounded(operation, limit = 1000) {
  const started = Date.now();
  await operation();
  if (Date.now() - started > limit) {
    throw new Error("duration did not bound monitor setup");
  }
}

async function testConnectExpiration() {
  class PendingSocket extends EventEmitter {
    constructor() {
      super();
      this.destroyed = false;
    }

    destroy() {
      if (!this.destroyed) {
        this.destroyed = true;
        this.emit("close");
      }
    }
  }

  await expectBounded(() =>
    monitorTopics({
      host: "unused",
      port: 1,
      realm: "default",
      topics: [TEST_TOPIC],
      duration: 20,
      maxEvents: 0,
      createConnection: () => new PendingSocket(),
    }),
  );
}

async function testHandshakeExpiration() {
  await withServer(
    async (socket) => {
      await once(socket, "close");
    },
    (port) =>
      expectBounded(() =>
        monitorTopics({
          host: "127.0.0.1",
          port,
          realm: "default",
          topics: [TEST_TOPIC],
          duration: 20,
          maxEvents: 0,
        }),
      ),
  );
}

async function testWelcomeExpiration() {
  await withServer(
    async (socket) => {
      const reader = new SocketReader(socket);
      await reader.read(4);
      await write(socket, Buffer.from([0x7f, 0xf2, 0, 0]));
      await readFrame(reader);
      await once(socket, "close");
    },
    (port) =>
      expectBounded(() =>
        monitorTopics({
          host: "127.0.0.1",
          port,
          realm: "default",
          topics: [TEST_TOPIC],
          duration: 20,
          maxEvents: 0,
        }),
      ),
  );
}

async function testEvent() {
  const records = [];
  await withServer(
    async (socket) => {
      const reader = new SocketReader(socket);
      await reader.read(4);
      await write(socket, Buffer.from([0x7f, 0xf2, 0, 0]));
      await readFrame(reader);
      await writeFrame(socket, [2, 42, { roles: { broker: {} } }]);
      const subscribe = await readFrame(reader);
      await writeFrame(socket, [33, subscribe[1], 100]);
      await writeFrame(socket, [36, 100, 200, {}, ["volumeup", "2"], {}]);
    },
    (port) =>
      monitorTopics({
        host: "127.0.0.1",
        port,
        realm: "default",
        topics: [TEST_TOPIC],
        duration: 1000,
        maxEvents: 1,
        onRecord: (record) => records.push(record),
      }),
  );
  if (
    records.length !== 2 ||
    records[1].topic !== TEST_TOPIC ||
    records[1].args.join(",") !== "volumeup,2"
  ) {
    throw new Error(`unexpected records: ${JSON.stringify(records)}`);
  }
}

await testConnectExpiration();
await testHandshakeExpiration();
await testWelcomeExpiration();
await testEvent();
console.log("wamp-monitor tests passed");

process.exitCode = 0;
