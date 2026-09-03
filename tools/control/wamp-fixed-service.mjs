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

const WAMP_HELLO = 1;
const WAMP_WELCOME = 2;
const WAMP_ERROR = 8;
const WAMP_REGISTER = 64;
const WAMP_REGISTERED = 65;
const WAMP_INVOCATION = 68;
const WAMP_YIELD = 70;

function usage(exitCode = 0) {
  console.log(`Usage: wamp-fixed-service.mjs PROCEDURE [options]

Options:
  --host HOST       WAMP host (default: 127.0.0.1)
  --port PORT       RawSocket port (default: 19999)
  --realm REALM     WAMP realm (default: default)
  --args JSON       Fixed positional result array (default: [])
  --kwargs JSON     Fixed keyword result object (default: {})
  --once            Exit after serving one invocation
  --help            Show this help`);
  process.exit(exitCode);
}

function parseOptions(argv) {
  const options = {
    host: "127.0.0.1",
    port: 19999,
    realm: "default",
    args: [],
    kwargs: {},
    once: false,
  };

  if (argv.length === 0 || argv.includes("--help")) {
    usage();
  }
  options.procedure = argv.shift();

  while (argv.length > 0) {
    const option = argv.shift();
    if (option === "--once") {
      options.once = true;
      continue;
    }

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
      case "--args":
        options.args = JSON.parse(value);
        break;
      case "--kwargs":
        options.kwargs = JSON.parse(value);
        break;
      default:
        throw new Error(`unknown option: ${option}`);
    }
  }

  if (!Array.isArray(options.args)) {
    throw new Error("--args must decode to a JSON array");
  }
  if (
    options.kwargs === null ||
    Array.isArray(options.kwargs) ||
    typeof options.kwargs !== "object"
  ) {
    throw new Error("--kwargs must decode to a JSON object");
  }
  if (!Number.isInteger(options.port) || options.port <= 0) {
    throw new Error("--port must be a positive integer");
  }
  return options;
}

export async function serveFixedProcedure({
  host,
  port,
  realm,
  procedure,
  args,
  kwargs,
  once: serveOnce,
}) {
  const socket = net.createConnection({ host, port });
  await once(socket, "connect");
  const reader = new SocketReader(socket);

  try {
    await write(socket, Buffer.from([0x7f, 0xf2, 0, 0]));
    const handshake = await reader.read(4);
    if (handshake[0] !== 0x7f || (handshake[1] & 0x0f) !== 2) {
      throw new Error(
        `rawsocket handshake rejected: ${handshake.toString("hex")}`,
      );
    }

    await writeFrame(socket, [
      WAMP_HELLO,
      realm,
      { roles: { callee: {} } },
    ]);
    const welcome = await readFrame(reader);
    if (!Array.isArray(welcome) || welcome[0] !== WAMP_WELCOME) {
      throw new Error(`expected WELCOME, received ${JSON.stringify(welcome)}`);
    }

    const registerRequestId = 1;
    await writeFrame(socket, [
      WAMP_REGISTER,
      registerRequestId,
      {},
      procedure,
    ]);
    const registered = await readFrame(reader);
    if (
      !Array.isArray(registered) ||
      registered[0] !== WAMP_REGISTERED ||
      registered[1] !== registerRequestId
    ) {
      if (
        Array.isArray(registered) &&
        registered[0] === WAMP_ERROR &&
        registered[1] === WAMP_REGISTER
      ) {
        throw new Error(`registration failed: ${registered[4]}`);
      }
      throw new Error(
        `expected REGISTERED, received ${JSON.stringify(registered)}`,
      );
    }

    console.log(
      JSON.stringify({
        state: "registered",
        procedure,
        registrationId: registered[2],
      }),
    );

    while (true) {
      const invocation = await readFrame(reader);
      if (
        !Array.isArray(invocation) ||
        invocation[0] !== WAMP_INVOCATION ||
        invocation[2] !== registered[2]
      ) {
        continue;
      }

      await writeFrame(socket, [
        WAMP_YIELD,
        invocation[1],
        {},
        args,
        kwargs,
      ]);
      console.log(
        JSON.stringify({
          state: "served",
          procedure,
          requestId: invocation[1],
        }),
      );
      if (serveOnce) {
        return;
      }
    }
  } finally {
    socket.destroy();
  }
}

async function main() {
  await serveFixedProcedure(parseOptions(process.argv.slice(2)));
}

if (process.argv[1]?.endsWith("wamp-fixed-service.mjs")) {
  main().catch((error) => {
    console.error(`ERROR: ${error.message}`);
    process.exitCode = 1;
  });
}
