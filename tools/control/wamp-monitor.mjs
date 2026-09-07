#!/usr/bin/env node
// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT

import { once } from "node:events";
import net from "node:net";
import process from "node:process";

import {
  SocketReader,
  readFrame,
  write,
  writeFrame,
} from "./wamp-call.mjs";

const WAMP_ERROR = 8;
const WAMP_EVENT = 36;
const WAMP_HELLO = 1;
const WAMP_SUBSCRIBE = 32;
const WAMP_SUBSCRIBED = 33;
const WAMP_WELCOME = 2;
const EXPIRED = Symbol("expired");

export const MCU_TOPICS = [
  "com.harman.ready.mcu-interface",
  "com.harman.heartbeat.mcu-interface",
  "com.harman.test.inputEvent",
  "com.harman.vui.keypress",
  "com.harman.vui.mcustatus",
  "com.harman.vui.mcuupgraderesult",
];

function jsonReplacer(_key, value) {
  if (typeof value === "bigint") {
    return value.toString();
  }
  if (Buffer.isBuffer(value)) {
    return { base64: value.toString("base64") };
  }
  return value;
}

export async function monitorTopics({
  host,
  port,
  realm,
  topics,
  duration,
  maxEvents,
  onRecord = (record) =>
    console.log(JSON.stringify(record, jsonReplacer)),
  createConnection = (options) => net.createConnection(options),
}) {
  const socket = createConnection({ host, port });
  let timer;
  let durationElapsed = false;
  let expire;
  const expiration = new Promise((resolve) => {
    expire = () => resolve(EXPIRED);
  });
  const bounded = (operation) => Promise.race([operation, expiration]);
  if (duration > 0) {
    timer = setTimeout(() => {
      durationElapsed = true;
      socket.destroy();
      expire();
    }, duration);
  }

  try {
    if ((await bounded(once(socket, "connect"))) === EXPIRED) {
      return;
    }
    const reader = new SocketReader(socket);

    if (
      (await bounded(write(socket, Buffer.from([0x7f, 0xf2, 0, 0])))) ===
      EXPIRED
    ) {
      return;
    }
    const handshake = await bounded(reader.read(4));
    if (handshake === EXPIRED) {
      return;
    }
    if (handshake[0] !== 0x7f || (handshake[1] & 0x0f) !== 2) {
      throw new Error(
        `rawsocket handshake rejected: ${handshake.toString("hex")}`,
      );
    }

    if (
      (await bounded(
        writeFrame(socket, [
          WAMP_HELLO,
          realm,
          { roles: { subscriber: {} } },
        ]),
      )) === EXPIRED
    ) {
      return;
    }
    const welcome = await bounded(readFrame(reader));
    if (welcome === EXPIRED) {
      return;
    }
    if (!Array.isArray(welcome) || welcome[0] !== WAMP_WELCOME) {
      throw new Error(`expected WELCOME, received ${JSON.stringify(welcome)}`);
    }

    const pending = new Map();
    const subscriptions = new Map();
    for (const [index, topic] of topics.entries()) {
      const requestId = index + 1;
      pending.set(requestId, topic);
      if (
        (await bounded(
          writeFrame(socket, [WAMP_SUBSCRIBE, requestId, {}, topic]),
        )) === EXPIRED
      ) {
        return;
      }
    }

    let eventCount = 0;
    while (!socket.destroyed) {
      const message = await bounded(readFrame(reader));
      if (message === EXPIRED) {
        break;
      }
      if (!Array.isArray(message)) {
        continue;
      }

      if (message[0] === WAMP_SUBSCRIBED) {
        const topic = pending.get(message[1]);
        if (topic !== undefined) {
          pending.delete(message[1]);
          subscriptions.set(message[2], topic);
          onRecord({ type: "subscribed", topic, subscriptionId: message[2] });
        }
        continue;
      }

      if (
        message[0] === WAMP_ERROR &&
        message[1] === WAMP_SUBSCRIBE &&
        pending.has(message[2])
      ) {
        throw new Error(
          `subscription failed for ${pending.get(message[2])}: ${message[4]}`,
        );
      }

      if (message[0] === WAMP_EVENT && subscriptions.has(message[1])) {
        eventCount += 1;
        onRecord({
          type: "event",
          topic: subscriptions.get(message[1]),
          publicationId: message[2],
          details: message[3] ?? {},
          args: message[4] ?? [],
          kwargs: message[5] ?? {},
        });
        if (maxEvents > 0 && eventCount >= maxEvents) {
          break;
        }
      }
    }
  } catch (error) {
    if (!durationElapsed) {
      throw error;
    }
  } finally {
    clearTimeout(timer);
    socket.destroy();
  }
}

function usage(exitCode = 0) {
  console.log(`Usage: wamp-monitor.mjs [options]

Passively subscribe to MCU WAMP topics. The tool never sends CALL or PUBLISH.

Options:
  --host HOST       WAMP host (default: 127.0.0.1)
  --port PORT       RawSocket port (default: 19999)
  --realm REALM     WAMP realm (default: default)
  --topic URI       Replace defaults; repeat to monitor multiple topics
  --duration SEC    Stop after this many seconds (default: run until interrupted)
  --max-events N    Stop after this many events (default: unlimited)
  --help            Show this help`);
  process.exit(exitCode);
}

function parseOptions(argv) {
  const options = {
    host: "127.0.0.1",
    port: 19999,
    realm: "default",
    topics: [],
    duration: 0,
    maxEvents: 0,
  };

  if (argv.includes("--help")) {
    usage();
  }
  while (argv.length > 0) {
    const option = argv.shift();
    const value = argv.shift();
    if (value === undefined) {
      throw new Error(`${option} requires a value`);
    }
    switch (option) {
      case "--host":
        options.host = value;
        break;
      case "--port":
        options.port = Number(value);
        break;
      case "--realm":
        options.realm = value;
        break;
      case "--topic":
        options.topics.push(value);
        break;
      case "--duration":
        options.duration = Number(value) * 1000;
        break;
      case "--max-events":
        options.maxEvents = Number(value);
        break;
      default:
        throw new Error(`unknown option: ${option}`);
    }
  }

  if (options.topics.length === 0) {
    options.topics = MCU_TOPICS;
  }
  if (!Number.isInteger(options.port) || options.port <= 0) {
    throw new Error("--port must be a positive integer");
  }
  if (!Number.isFinite(options.duration) || options.duration < 0) {
    throw new Error("--duration must be a non-negative number");
  }
  if (!Number.isInteger(options.maxEvents) || options.maxEvents < 0) {
    throw new Error("--max-events must be a non-negative integer");
  }
  return options;
}

async function main() {
  await monitorTopics(parseOptions(process.argv.slice(2)));
}

if (process.argv[1]?.endsWith("wamp-monitor.mjs")) {
  main().catch((error) => {
    console.error(`ERROR: ${error.message}`);
    process.exitCode = 1;
  });
}
