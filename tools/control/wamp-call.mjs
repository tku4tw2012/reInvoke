#!/usr/bin/env node
// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT

import { once } from "node:events";
import net from "node:net";
import process from "node:process";

const WAMP_CALL = 48;
const WAMP_ERROR = 8;
const WAMP_HELLO = 1;
const WAMP_RESULT = 50;
const WAMP_WELCOME = 2;

function concat(prefix, payload) {
  return Buffer.concat([Buffer.from(prefix), payload]);
}

function encodeInteger(value) {
  if (value >= 0 && value <= 0x7f) {
    return Buffer.from([value]);
  }
  if (value >= -32 && value < 0) {
    return Buffer.from([0x100 + value]);
  }

  if (value >= 0 && value <= 0xff) {
    return Buffer.from([0xcc, value]);
  }
  if (value >= 0 && value <= 0xffff) {
    const output = Buffer.alloc(3);
    output[0] = 0xcd;
    output.writeUInt16BE(value, 1);
    return output;
  }
  if (value >= 0 && value <= 0xffffffff) {
    const output = Buffer.alloc(5);
    output[0] = 0xce;
    output.writeUInt32BE(value, 1);
    return output;
  }
  if (value >= -0x80 && value < 0) {
    const output = Buffer.alloc(2);
    output[0] = 0xd0;
    output.writeInt8(value, 1);
    return output;
  }
  if (value >= -0x8000 && value < 0) {
    const output = Buffer.alloc(3);
    output[0] = 0xd1;
    output.writeInt16BE(value, 1);
    return output;
  }
  if (value >= -0x80000000 && value < 0) {
    const output = Buffer.alloc(5);
    output[0] = 0xd2;
    output.writeInt32BE(value, 1);
    return output;
  }

  const output = Buffer.alloc(9);
  output[0] = 0xcb;
  output.writeDoubleBE(value, 1);
  return output;
}

function encodeFloat(value) {
  const output = Buffer.alloc(9);
  output[0] = 0xcb;
  output.writeDoubleBE(value, 1);
  return output;
}

function encodeString(value) {
  const payload = Buffer.from(value, "utf8");
  if (payload.length <= 31) {
    return concat([0xa0 | payload.length], payload);
  }
  if (payload.length <= 0xff) {
    return concat([0xd9, payload.length], payload);
  }
  if (payload.length <= 0xffff) {
    const header = Buffer.alloc(3);
    header[0] = 0xda;
    header.writeUInt16BE(payload.length, 1);
    return Buffer.concat([header, payload]);
  }

  const header = Buffer.alloc(5);
  header[0] = 0xdb;
  header.writeUInt32BE(payload.length, 1);
  return Buffer.concat([header, payload]);
}

function encodeArray(value) {
  const items = value.map(encode);
  let header;
  if (items.length <= 15) {
    header = Buffer.from([0x90 | items.length]);
  } else if (items.length <= 0xffff) {
    header = Buffer.alloc(3);
    header[0] = 0xdc;
    header.writeUInt16BE(items.length, 1);
  } else {
    header = Buffer.alloc(5);
    header[0] = 0xdd;
    header.writeUInt32BE(items.length, 1);
  }
  return Buffer.concat([header, ...items]);
}

function encodeMap(value) {
  const entries = Object.entries(value);
  let header;
  if (entries.length <= 15) {
    header = Buffer.from([0x80 | entries.length]);
  } else if (entries.length <= 0xffff) {
    header = Buffer.alloc(3);
    header[0] = 0xde;
    header.writeUInt16BE(entries.length, 1);
  } else {
    header = Buffer.alloc(5);
    header[0] = 0xdf;
    header.writeUInt32BE(entries.length, 1);
  }

  const items = entries.flatMap(([key, item]) => [
    encodeString(key),
    encode(item),
  ]);
  return Buffer.concat([header, ...items]);
}

export function encode(value) {
  if (value === null) {
    return Buffer.from([0xc0]);
  }
  if (value === false) {
    return Buffer.from([0xc2]);
  }
  if (value === true) {
    return Buffer.from([0xc3]);
  }
  if (typeof value === "number") {
    return Number.isInteger(value) ? encodeInteger(value) : encodeFloat(value);
  }
  if (typeof value === "string") {
    return encodeString(value);
  }
  if (Buffer.isBuffer(value)) {
    if (value.length > 0xff) {
      throw new Error("binary values longer than 255 bytes are not supported");
    }
    return concat([0xc4, value.length], value);
  }
  if (Array.isArray(value)) {
    return encodeArray(value);
  }
  if (typeof value === "object") {
    return encodeMap(value);
  }
  throw new TypeError(`unsupported MsgPack value: ${typeof value}`);
}

function requireBytes(buffer, offset, count) {
  if (offset + count > buffer.length) {
    throw new Error("truncated MsgPack value");
  }
}

function decodeLength(buffer, offset, width) {
  requireBytes(buffer, offset, width);
  if (width === 1) {
    return [buffer.readUInt8(offset), offset + 1];
  }
  if (width === 2) {
    return [buffer.readUInt16BE(offset), offset + 2];
  }
  return [buffer.readUInt32BE(offset), offset + 4];
}

function decodeCollection(buffer, offset, count, map) {
  const output = map ? {} : [];
  let cursor = offset;

  for (let index = 0; index < count; index += 1) {
    const [keyOrValue, next] = decode(buffer, cursor);
    cursor = next;
    if (map) {
      const [value, afterValue] = decode(buffer, cursor);
      output[String(keyOrValue)] = value;
      cursor = afterValue;
    } else {
      output.push(keyOrValue);
    }
  }
  return [output, cursor];
}

function decodeString(buffer, offset, length) {
  requireBytes(buffer, offset, length);
  return [buffer.toString("utf8", offset, offset + length), offset + length];
}

export function decode(buffer, offset = 0) {
  requireBytes(buffer, offset, 1);
  const marker = buffer[offset];
  let cursor = offset + 1;

  if (marker <= 0x7f) {
    return [marker, cursor];
  }
  if (marker >= 0xe0) {
    return [marker - 0x100, cursor];
  }
  if ((marker & 0xf0) === 0x80) {
    return decodeCollection(buffer, cursor, marker & 0x0f, true);
  }
  if ((marker & 0xf0) === 0x90) {
    return decodeCollection(buffer, cursor, marker & 0x0f, false);
  }
  if ((marker & 0xe0) === 0xa0) {
    return decodeString(buffer, cursor, marker & 0x1f);
  }

  let length;
  switch (marker) {
    case 0xc0:
      return [null, cursor];
    case 0xc2:
      return [false, cursor];
    case 0xc3:
      return [true, cursor];
    case 0xc4:
      [length, cursor] = decodeLength(buffer, cursor, 1);
      requireBytes(buffer, cursor, length);
      return [buffer.subarray(cursor, cursor + length), cursor + length];
    case 0xca:
      requireBytes(buffer, cursor, 4);
      return [buffer.readFloatBE(cursor), cursor + 4];
    case 0xcb:
      requireBytes(buffer, cursor, 8);
      return [buffer.readDoubleBE(cursor), cursor + 8];
    case 0xcc:
      requireBytes(buffer, cursor, 1);
      return [buffer.readUInt8(cursor), cursor + 1];
    case 0xcd:
      requireBytes(buffer, cursor, 2);
      return [buffer.readUInt16BE(cursor), cursor + 2];
    case 0xce:
      requireBytes(buffer, cursor, 4);
      return [buffer.readUInt32BE(cursor), cursor + 4];
    case 0xcf: {
      requireBytes(buffer, cursor, 8);
      const value = buffer.readBigUInt64BE(cursor);
      const safe = value <= BigInt(Number.MAX_SAFE_INTEGER) ? Number(value) : value;
      return [safe, cursor + 8];
    }
    case 0xd0:
      requireBytes(buffer, cursor, 1);
      return [buffer.readInt8(cursor), cursor + 1];
    case 0xd1:
      requireBytes(buffer, cursor, 2);
      return [buffer.readInt16BE(cursor), cursor + 2];
    case 0xd2:
      requireBytes(buffer, cursor, 4);
      return [buffer.readInt32BE(cursor), cursor + 4];
    case 0xd3: {
      requireBytes(buffer, cursor, 8);
      const value = buffer.readBigInt64BE(cursor);
      const minimum = BigInt(Number.MIN_SAFE_INTEGER);
      const maximum = BigInt(Number.MAX_SAFE_INTEGER);
      const safe = value >= minimum && value <= maximum ? Number(value) : value;
      return [safe, cursor + 8];
    }
    case 0xd9:
      [length, cursor] = decodeLength(buffer, cursor, 1);
      return decodeString(buffer, cursor, length);
    case 0xda:
      [length, cursor] = decodeLength(buffer, cursor, 2);
      return decodeString(buffer, cursor, length);
    case 0xdb:
      [length, cursor] = decodeLength(buffer, cursor, 4);
      return decodeString(buffer, cursor, length);
    case 0xdc:
      [length, cursor] = decodeLength(buffer, cursor, 2);
      return decodeCollection(buffer, cursor, length, false);
    case 0xdd:
      [length, cursor] = decodeLength(buffer, cursor, 4);
      return decodeCollection(buffer, cursor, length, false);
    case 0xde:
      [length, cursor] = decodeLength(buffer, cursor, 2);
      return decodeCollection(buffer, cursor, length, true);
    case 0xdf:
      [length, cursor] = decodeLength(buffer, cursor, 4);
      return decodeCollection(buffer, cursor, length, true);
    default:
      throw new Error(`unsupported MsgPack marker: 0x${marker.toString(16)}`);
  }
}

export class SocketReader {
  constructor(socket) {
    this.iterator = socket[Symbol.asyncIterator]();
    this.buffer = Buffer.alloc(0);
  }

  async read(length) {
    while (this.buffer.length < length) {
      const { value, done } = await this.iterator.next();
      if (done) {
        throw new Error("socket closed before the response completed");
      }
      this.buffer = Buffer.concat([this.buffer, value]);
    }
    const output = this.buffer.subarray(0, length);
    this.buffer = this.buffer.subarray(length);
    return output;
  }
}

export async function write(socket, payload) {
  if (!socket.write(payload)) {
    await once(socket, "drain");
  }
}

export async function writeFrame(socket, message) {
  const payload = encode(message);
  if (payload.length > 0xffffff) {
    throw new Error("WAMP frame exceeds the 24-bit rawsocket limit");
  }
  const header = Buffer.from([
    0,
    (payload.length >> 16) & 0xff,
    (payload.length >> 8) & 0xff,
    payload.length & 0xff,
  ]);
  await write(socket, Buffer.concat([header, payload]));
}

export async function readFrame(reader) {
  const header = await reader.read(4);
  if (header[0] !== 0) {
    throw new Error(`unsupported rawsocket frame type: ${header[0]}`);
  }
  const length = (header[1] << 16) | (header[2] << 8) | header[3];
  const payload = await reader.read(length);
  const [message, consumed] = decode(payload);
  if (consumed !== payload.length) {
    throw new Error("WAMP frame contains trailing MsgPack data");
  }
  return message;
}

export async function callProcedure({
  host,
  port,
  realm,
  procedure,
  args,
  kwargs,
  timeout,
}) {
  const socket = net.createConnection({ host, port });
  socket.setTimeout(timeout, () => {
    socket.destroy(new Error(`WAMP call timed out after ${timeout} ms`));
  });
  await once(socket, "connect");
  const reader = new SocketReader(socket);

  try {
    await write(socket, Buffer.from([0x7f, 0xf2, 0, 0]));
    const handshake = await reader.read(4);
    if (handshake[0] !== 0x7f || (handshake[1] & 0x0f) !== 2) {
      throw new Error(`rawsocket handshake rejected: ${handshake.toString("hex")}`);
    }

    await writeFrame(socket, [
      WAMP_HELLO,
      realm,
      { roles: { caller: {} } },
    ]);
    const welcome = await readFrame(reader);
    if (!Array.isArray(welcome) || welcome[0] !== WAMP_WELCOME) {
      throw new Error(`expected WELCOME, received ${JSON.stringify(welcome)}`);
    }

    const requestId = 1;
    await writeFrame(socket, [
      WAMP_CALL,
      requestId,
      {},
      procedure,
      args,
      kwargs,
    ]);

    while (true) {
      const response = await readFrame(reader);
      if (!Array.isArray(response)) {
        continue;
      }
      if (response[0] === WAMP_RESULT && response[1] === requestId) {
        return {
          type: "result",
          details: response[2] ?? {},
          args: response[3] ?? [],
          kwargs: response[4] ?? {},
        };
      }
      if (
        response[0] === WAMP_ERROR &&
        response[1] === WAMP_CALL &&
        response[2] === requestId
      ) {
        return {
          type: "error",
          details: response[3] ?? {},
          error: response[4],
          args: response[5] ?? [],
          kwargs: response[6] ?? {},
        };
      }
    }
  } finally {
    socket.destroy();
  }
}

function usage(exitCode = 0) {
  console.log(`Usage: wamp-call.mjs PROCEDURE [options]

Options:
  --host HOST       WAMP host (default: 127.0.0.1)
  --port PORT       RawSocket port (default: 19999)
  --realm REALM     WAMP realm (default: default)
  --args JSON       Positional argument array (default: [])
  --kwargs JSON     Keyword argument object (default: {})
  --timeout MS      Socket timeout (default: 8000)
  --help             Show this help`);
  process.exit(exitCode);
}

function parseOptions(argv) {
  const options = {
    host: "127.0.0.1",
    port: 19999,
    realm: "default",
    args: [],
    kwargs: {},
    timeout: 8000,
  };

  if (argv.length === 0 || argv.includes("--help")) {
    usage();
  }
  options.procedure = argv.shift();

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
      case "--args":
        options.args = JSON.parse(value);
        break;
      case "--kwargs":
        options.kwargs = JSON.parse(value);
        break;
      case "--timeout":
        options.timeout = Number(value);
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
  for (const [name, value] of [
    ["--port", options.port],
    ["--timeout", options.timeout],
  ]) {
    if (!Number.isInteger(value) || value <= 0) {
      throw new Error(`${name} must be a positive integer`);
    }
  }
  return options;
}

function jsonReplacer(_key, value) {
  if (typeof value === "bigint") {
    return value.toString();
  }
  if (Buffer.isBuffer(value)) {
    return { base64: value.toString("base64") };
  }
  return value;
}

async function main() {
  const options = parseOptions(process.argv.slice(2));
  const result = await callProcedure(options);
  console.log(JSON.stringify(result, jsonReplacer, 2));
  if (result.type === "error") {
    process.exitCode = 2;
  }
}

if (process.argv[1]?.endsWith("wamp-call.mjs")) {
  main().catch((error) => {
    console.error(`ERROR: ${error.message}`);
    process.exitCode = 1;
  });
}
