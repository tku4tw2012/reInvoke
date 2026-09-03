#!/usr/bin/env node
// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

// Offline decoder for the donor dsp-client SPI frame format.
//
// This tool is passive by construction. It opens no device node, sends no
// WAMP message, and drives no SPI, GPIO, or I2C line. Its command mode only
// prints the bytes a procedure would put on the wire.
//
// The format is documented in docs/emulation/dsp-boundary.md.

import fs from "node:fs";
import process from "node:process";

// Device-to-host events, keyed by "<message id>:<event code>".
export const DSP_EVENTS = new Map([
  ["0:4", "EVENT_NEW_DAC_GAIN"],
  ["0:5", "EVENT_EXPECT_SPEECH"],
  ["0:6", "EVENT_CANCEL_TRIGGER"],
  ["0:7", "EVENT_SW_UPGRADE"],
  ["0:8", "EVENT_DSP_VERSION"],
  ["0:9", "EVENT_MIC_MUTE"],
  ["0:11", "EVENT_CORTANA_SKYPE"],
  ["0:12", "DSP_MEMORY_DUMP"],
  ["0:255", "EVENT_ERR"],
  ["1:0", "EVENT_TRIGGER_FOUND"],
  ["1:1", "EVENT_PAYLOAD_DEGIN"],
  ["1:2", "EVENT_PAYLOAD_END"],
  ["1:3", "EVENT_PAYLOAD_TIMEOUT"],
  ["1:4", "EVENT_DSP_BOOTUP"],
  ["1:255", "EVENT_WRITE_ERR"],
  ["2:0", "EVENT_MIC_TEST_SINGLE"],
  ["2:1", "EVENT_MIC_TEST_PAIR"],
  ["2:2", "EVENT_MIC_NORMAL"],
  ["2:3", "EVENT_HW_PERFORM_TEST"],
  ["2:255", "EVENT_TEST_ERR"],
]);

// Host-to-device commands, one entry per msgwrite call site in the donor.
export const DSP_COMMANDS = new Map([
  ["com.harman.dsp.micTestSingle", { id: 2, opcode: 0x00, args: ["mic"] }],
  ["com.harman.dsp.micTestPair", { id: 2, opcode: 0x01, args: ["pair"] }],
  ["com.harman.dsp.micTestNormal", { id: 2, opcode: 0x02, args: [] }],
  ["com.harman.test.dspBypassMode", { id: 2, opcode: 0x03, args: ["mode"] }],
  ["com.harman.dsp.volumeSet", { id: 0, opcode: 0x04, args: ["volume"] }],
  ["com.harman.dsp.getVer", { id: 0, opcode: 0x08, args: [] }],
  ["com.harman.dsp.micMute", { id: 0, opcode: 0x09, args: ["mute"] }],
  ["com.harman.stateChanged", { id: 0, opcode: 0x0b, args: ["state"] }],
  [
    "com.harman.dsp.dumpDspMemory",
    { id: 0, opcode: 0x0c, args: ["low", "high"] },
  ],
]);

// Never called by this tool; listed so callers can tell the one safe
// procedure from the rest. Every other entry changes DSP state.
export const SIDE_EFFECT_FREE_PROCEDURES = ["com.harman.dsp.getVer"];

// Handled as a subscription in the donor, not as a registration.
export const SUBSCRIBED_TOPICS = ["com.harman.stateChanged"];

// Wire length of a frame carrying `length` payload bytes. Both directions
// round the 5-byte header plus payload up to a multiple of four.
export function frameLength(length) {
  return Math.ceil((length + 5) / 4) * 4;
}

// Bytes the receive path actually reads after the 5-byte header. The donor
// clamps to a minimum of three so that an id-and-code tuple always arrives.
export function devicePayloadRead(length) {
  return length <= 3 ? 3 : frameLength(length) - 5;
}

export function checksum(id, payload) {
  let sum = (id >> 8) + (id & 0xff) + (payload.length >> 8) +
    (payload.length & 0xff);
  for (const byte of payload) {
    sum += byte;
  }
  return sum & 0xff;
}

// Host-to-device frame: 5-byte header, payload, zero padded to a multiple of
// four bytes.
export function buildFrame(id, payload) {
  const body = Uint8Array.from(payload);
  const frame = new Uint8Array(frameLength(body.length));
  frame[0] = (id >> 8) & 0xff;
  frame[1] = id & 0xff;
  frame[2] = (body.length >> 8) & 0xff;
  frame[3] = body.length & 0xff;
  frame[4] = checksum(id, body);
  frame.set(body, 5);
  return frame;
}

export function encodeCommand(procedure, args = []) {
  const spec = DSP_COMMANDS.get(procedure);
  if (!spec) {
    throw new Error(`unknown procedure ${procedure}`);
  }
  if (args.length !== spec.args.length) {
    throw new Error(
      `${procedure} takes ${spec.args.length} argument(s): ${
        spec.args.join(", ") || "none"
      }`,
    );
  }
  for (const value of args) {
    if (!Number.isInteger(value) || value < 0 || value > 0xff) {
      throw new Error(`${procedure} arguments must be bytes 0..255`);
    }
  }
  const payload = [spec.opcode, ...args];
  return {
    procedure,
    id: spec.id,
    payload: Uint8Array.from(payload),
    frame: buildFrame(spec.id, payload),
  };
}

export function decodeHostFrame(bytes) {
  const data = Uint8Array.from(bytes);
  if (data.length < 5) {
    throw new Error("host frame needs at least 5 bytes");
  }
  const id = (data[0] << 8) | data[1];
  const length = (data[2] << 8) | data[3];
  const payload = data.slice(5, 5 + length);
  const expected = checksum(id, payload);
  let procedure = null;
  if (payload.length > 0) {
    for (const [name, spec] of DSP_COMMANDS) {
      if (spec.id === id && spec.opcode === payload[0]) {
        procedure = name;
        break;
      }
    }
  }
  return {
    direction: "host-to-dsp",
    id,
    length,
    checksum: data[4],
    checksumExpected: expected,
    checksumValid: data[4] === expected && payload.length === length,
    truncated: payload.length !== length,
    procedure,
    payload,
  };
}

// Device-to-host wire frame. Same 5-byte header as the host direction:
// id, length, checksum. The donor verifies the checksum over the four header
// bytes plus every payload byte it read, padding included.
export function decodeDeviceFrame(bytes) {
  const data = Uint8Array.from(bytes);
  if (data.length < 5) {
    throw new Error("device frame needs at least 5 bytes");
  }
  const id = (data[0] << 8) | data[1];
  const length = (data[2] << 8) | data[3];
  const read = devicePayloadRead(length);
  const readBytes = data.slice(5, 5 + read);
  let sum = data[0] + data[1] + data[2] + data[3];
  for (const byte of readBytes) {
    sum += byte;
  }
  const expected = sum & 0xff;
  const payload = data.slice(5, 5 + length);
  const decoded = {
    direction: "dsp-to-dost",
    id,
    length,
    checksum: data[4],
    checksumExpected: expected,
    checksumValid: data[4] === expected && readBytes.length === read,
    truncated: readBytes.length !== read,
    payload,
    ...describeEvent(id, payload),
  };
  decoded.direction = "dsp-to-host";
  // The donor rejects a header whose first two bytes are 0xFF.
  decoded.rejected = data[0] === 0xff || data[1] === 0xff || length === 0;
  return decoded;
}

// What dsp-client hands to Dsp_msg_handle, and what it prints as
// "readmsg:": the two header id bytes followed by the payload. The event
// code is payload[0], so it lands at index 2 of the printed tuple.
export function decodeReadmsgTuple(bytes) {
  const data = Uint8Array.from(bytes);
  if (data.length < 3) {
    throw new Error("readmsg tuple needs at least 3 bytes");
  }
  const id = (data[0] << 8) | data[1];
  const payload = data.slice(2);
  return {
    direction: "dsp-to-host",
    source: "readmsg",
    id,
    payload,
    ...describeEvent(id, payload),
  };
}

function describeEvent(id, payload) {
  if (payload.length === 0) {
    return { code: null, event: null };
  }
  const code = payload[0];
  const described = {
    code,
    event: DSP_EVENTS.get(`${id}:${code}`) ?? null,
  };
  const version = payload.slice(1, 5);
  if (described.event === "EVENT_DSP_VERSION" && version.length === 4) {
    described.version = Array.from(version, (b) => b.toString(16).toUpperCase())
      .join(".");
    described.versionInteger = ((version[0] << 24) >>> 0) +
      (version[1] << 16) + (version[2] << 8) + version[3];
  }
  return described;
}

export function parseHexBytes(text) {
  const tokens = text.trim().split(/[\s,]+/).filter(Boolean);
  return tokens.map((token) => {
    const value = Number.parseInt(token.replace(/^0x/i, ""), 16);
    if (!Number.isInteger(value) || value < 0 || value > 0xff) {
      throw new Error(`not a byte: ${token}`);
    }
    return value;
  });
}

// dsp-client prints every received frame as "readmsg: 0x00 0x01 0x04 ".
const READMSG = /^\s*readmsg:\s*((?:0x[0-9a-fA-F]{2}\s*)+)$/;
const VERSION_LINE = /^\s*rec:\s*EVENT_DSP_VERSION=\s*([0-9A-Fa-f.]+)\s*$/;

export function parseLogLine(line) {
  const frame = READMSG.exec(line);
  if (frame) {
    return { kind: "frame", bytes: parseHexBytes(frame[1]) };
  }
  const version = VERSION_LINE.exec(line);
  if (version) {
    return { kind: "version", version: version[1] };
  }
  return null;
}

export function decodeLog(text) {
  const results = [];
  for (const [index, line] of text.split(/\r?\n/).entries()) {
    const parsed = parseLogLine(line);
    if (!parsed) {
      continue;
    }
    if (parsed.kind === "frame") {
      results.push({
        line: index + 1,
        raw: line.trim(),
        ...decodeReadmsgTuple(parsed.bytes),
      });
    } else {
      results.push({ line: index + 1, raw: line.trim(), ...parsed });
    }
  }
  return results;
}

function hex(bytes) {
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join(" ");
}

function formatDeviceFrame(entry) {
  const code = entry.code ?? 0;
  const name = entry.event ?? `unknown (id ${entry.id}, code 0x${
    code.toString(16).padStart(2, "0")
  })`;
  const parts = [`id=${entry.id}`];
  if (entry.length !== undefined) {
    parts.push(`len=${entry.length}`);
    parts.push(`checksum=0x${entry.checksum.toString(16).padStart(2, "0")}`);
    parts.push(
      entry.checksumValid ? "checksum OK" : `checksum BAD, expected 0x${
        entry.checksumExpected.toString(16).padStart(2, "0")
      }`,
    );
  }
  parts.push(`code=0x${code.toString(16).padStart(2, "0")}`, name);
  if (entry.rejected) {
    parts.push("REJECTED by donor header check");
  }
  if (entry.payload.length > 0) {
    parts.push(`payload=[${hex(entry.payload)}]`);
  }
  if (entry.version) {
    parts.push(`version=${entry.version} (${entry.versionInteger})`);
  }
  return parts.join("  ");
}

function formatHostFrame(entry) {
  const parts = [
    `id=${entry.id}`,
    `len=${entry.length}`,
    `checksum=0x${entry.checksum.toString(16).padStart(2, "0")}`,
    entry.checksumValid ? "checksum OK" : `checksum BAD, expected 0x${
      entry.checksumExpected.toString(16).padStart(2, "0")
    }`,
  ];
  if (entry.procedure) {
    parts.push(entry.procedure);
  }
  if (entry.payload.length > 0) {
    parts.push(`payload=[${hex(entry.payload)}]`);
  }
  return parts.join("  ");
}

function usage(exitCode = 0) {
  const stream = exitCode === 0 ? process.stdout : process.stderr;
  stream.write(`Usage:
  dsp-frame-decode.mjs --log <file>            decode a dsp-client log
  dsp-frame-decode.mjs --log -                 decode a log on stdin
  dsp-frame-decode.mjs --device <hex bytes>    decode one DSP-to-host frame
  dsp-frame-decode.mjs --readmsg <hex bytes>   decode one "readmsg:" tuple
  dsp-frame-decode.mjs --host <hex bytes>      decode one host-to-DSP frame
  dsp-frame-decode.mjs --command <uri> [args]  print the frame a call emits
  dsp-frame-decode.mjs --list                  list commands and events

This tool is passive. It never opens a device node and never sends anything.
Frame format: docs/emulation/dsp-boundary.md
`);
  process.exit(exitCode);
}

function listSurface() {
  process.stdout.write("Host to DSP:\n");
  for (const [name, spec] of DSP_COMMANDS) {
    const args = spec.args.length ? ` <${spec.args.join("> <")}>` : "";
    const notes = [];
    if (SIDE_EFFECT_FREE_PROCEDURES.includes(name)) {
      notes.push("side-effect free");
    }
    if (SUBSCRIBED_TOPICS.includes(name)) {
      notes.push("subscribed topic, not a registration");
    }
    const safe = notes.length ? `  [${notes.join(", ")}]` : "";
    process.stdout.write(
      `  ${name}${args}\n    id ${spec.id}, opcode 0x${
        spec.opcode.toString(16).padStart(2, "0")
      }${safe}\n`,
    );
  }
  process.stdout.write("\nDSP to host:\n");
  for (const [key, name] of DSP_EVENTS) {
    const [id, code] = key.split(":");
    process.stdout.write(
      `  id ${id}, code 0x${
        Number(code).toString(16).padStart(2, "0")
      }  ${name}\n`,
    );
  }
}

function readInput(path) {
  return path === "-"
    ? fs.readFileSync(0, "utf8")
    : fs.readFileSync(path, "utf8");
}

export function main(argv) {
  if (argv.length === 0 || argv.includes("--help") || argv.includes("-h")) {
    usage(0);
  }
  const [mode, ...rest] = argv;
  switch (mode) {
    case "--list": {
      listSurface();
      return;
    }
    case "--log": {
      if (rest.length !== 1) {
        usage(1);
      }
      const entries = decodeLog(readInput(rest[0]));
      if (entries.length === 0) {
        process.stdout.write("no dsp-client frames found\n");
        return;
      }
      for (const entry of entries) {
        if (entry.kind === "version") {
          process.stdout.write(
            `line ${entry.line}: EVENT_DSP_VERSION text ${entry.version}\n`,
          );
        } else {
          process.stdout.write(
            `line ${entry.line}: ${formatDeviceFrame(entry)}\n`,
          );
        }
      }
      return;
    }
    case "--device": {
      const entry = decodeDeviceFrame(parseHexBytes(rest.join(" ")));
      process.stdout.write(`${formatDeviceFrame(entry)}\n`);
      return;
    }
    case "--readmsg": {
      const entry = decodeReadmsgTuple(parseHexBytes(rest.join(" ")));
      process.stdout.write(`${formatDeviceFrame(entry)}\n`);
      return;
    }
    case "--host": {
      const entry = decodeHostFrame(parseHexBytes(rest.join(" ")));
      process.stdout.write(`${formatHostFrame(entry)}\n`);
      return;
    }
    case "--command": {
      if (rest.length === 0) {
        usage(1);
      }
      const [procedure, ...args] = rest;
      const encoded = encodeCommand(procedure, args.map((a) => Number(a)));
      process.stdout.write(
        `${procedure}\n  id ${encoded.id}\n  payload ${
          hex(encoded.payload)
        }\n  frame   ${hex(encoded.frame)}\n  not sent, this tool is passive\n`,
      );
      return;
    }
    default:
      usage(1);
  }
}

if (process.argv[1]?.endsWith("dsp-frame-decode.mjs")) {
  // Piping into head or less closes stdout early; that is not an error here.
  process.stdout.on("error", (error) => {
    if (error.code !== "EPIPE") {
      throw error;
    }
  });
  try {
    main(process.argv.slice(2));
  } catch (error) {
    console.error(`ERROR: ${error.message}`);
    process.exitCode = 1;
  }
}
