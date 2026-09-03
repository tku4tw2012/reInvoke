// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import test from "node:test";

import {
  buildFrame,
  checksum,
  decodeDeviceFrame,
  decodeHostFrame,
  decodeReadmsgTuple,
  devicePayloadRead,
  frameLength,
  decodeLog,
  encodeCommand,
  parseHexBytes,
} from "./dsp-frame-decode.mjs";

test("decodes the tuple captured on hardware", () => {
  // services.log from the SPI plus base-gpio run: "readmsg: 0x00 0x01 0x04",
  // immediately followed by EVENT_DSP_BOOTUP. What dsp-client prints is the
  // header id followed by the payload, not the wire frame.
  const tuple = decodeReadmsgTuple([0x00, 0x01, 0x04]);

  assert.equal(tuple.id, 1);
  assert.equal(tuple.code, 4);
  assert.equal(tuple.event, "EVENT_DSP_BOOTUP");
});

test("decodes the wire frame that produces that tuple", () => {
  // id 1, one payload byte, checksum 0+1+0+1+4+0+0 over the padded read.
  const frame = decodeDeviceFrame([0x00, 0x01, 0x00, 0x01, 0x06, 0x04, 0, 0]);

  assert.equal(frame.id, 1);
  assert.equal(frame.length, 1);
  assert.equal(frame.code, 4);
  assert.equal(frame.event, "EVENT_DSP_BOOTUP");
  assert.equal(frame.checksumValid, true);
  assert.equal(frame.rejected, false);
});

test("rounds both directions to the same wire length", () => {
  assert.equal(frameLength(1), 8);
  assert.equal(frameLength(3), 8);
  assert.equal(frameLength(4), 12);
  assert.equal(frameLength(7), 12);
  assert.equal(frameLength(8), 16);
  // The receive path clamps short payloads up to three bytes.
  assert.equal(devicePayloadRead(1), 3);
  assert.equal(devicePayloadRead(4), 7);
  assert.equal(devicePayloadRead(8), 11);
});

test("packs the version bytes the way the donor publishes them", () => {
  // The hardware run printed EVENT_DSP_VERSION=0.0.64.58 and published 25688.
  const tuple = decodeReadmsgTuple([0x00, 0x00, 0x08, 0x00, 0x00, 0x64, 0x58]);

  assert.equal(tuple.event, "EVENT_DSP_VERSION");
  assert.equal(tuple.version, "0.0.64.58");
  assert.equal(tuple.versionInteger, 25688);
});

test("computes the donor checksum over header and payload", () => {
  assert.equal(checksum(0, [0x08]), 0x01 + 0x08);
  assert.equal(checksum(2, [0x00, 0x03]), 0x02 + 0x02 + 0x03);
});

test("pads host frames to a multiple of four bytes", () => {
  assert.deepEqual(
    Array.from(buildFrame(0, [0x08])),
    [0x00, 0x00, 0x00, 0x01, 0x09, 0x08, 0x00, 0x00],
  );
  assert.equal(buildFrame(0, [0x0c, 0x00, 0x10]).length, 8);
  assert.equal(buildFrame(0, []).length, 8);
});

test("encodes every recovered procedure", () => {
  assert.deepEqual(
    Array.from(encodeCommand("com.harman.dsp.getVer").payload),
    [0x08],
  );
  assert.deepEqual(
    Array.from(encodeCommand("com.harman.dsp.volumeSet", [30]).payload),
    [0x04, 30],
  );
  assert.deepEqual(
    Array.from(encodeCommand("com.harman.dsp.micTestPair", [1]).payload),
    [0x01, 1],
  );
  assert.equal(encodeCommand("com.harman.dsp.micTestPair", [1]).id, 2);
  assert.deepEqual(
    Array.from(
      encodeCommand("com.harman.dsp.dumpDspMemory", [0x00, 0x10]).payload,
    ),
    [0x0c, 0x00, 0x10],
  );
});

test("rejects unknown procedures and bad arity", () => {
  assert.throws(() => encodeCommand("com.harman.dsp.reboot"), /unknown/);
  assert.throws(
    () => encodeCommand("com.harman.dsp.volumeSet"),
    /takes 1 argument/,
  );
  assert.throws(
    () => encodeCommand("com.harman.dsp.volumeSet", [300]),
    /bytes 0\.\.255/,
  );
});

test("round-trips a host frame through the decoder", () => {
  const encoded = encodeCommand("com.harman.dsp.micMute", [1]);
  const decoded = decodeHostFrame(encoded.frame);

  assert.equal(decoded.id, 0);
  assert.equal(decoded.length, 2);
  assert.equal(decoded.checksumValid, true);
  assert.equal(decoded.procedure, "com.harman.dsp.micMute");
  assert.deepEqual(Array.from(decoded.payload), [0x09, 0x01]);
});

test("reports a corrupted checksum instead of guessing", () => {
  const frame = Array.from(encodeCommand("com.harman.dsp.getVer").frame);
  frame[4] ^= 0xff;

  const decoded = decodeHostFrame(frame);

  assert.equal(decoded.checksumValid, false);
  assert.equal(decoded.checksumExpected, 0x09);
});

test("decodes a captured log excerpt", () => {
  const log = [
    "enter dsp client thread...",
    "readmsg: 0x00 0x01 0x04 ",
    "EVENT_DSP_BOOTUP",
    "dsp call mcu unmute!!!",
    "rec: EVENT_DSP_VERSION= 0.0.64.58",
    "msgwrite_callback ret= 0",
  ].join("\n");

  const entries = decodeLog(log);

  assert.equal(entries.length, 2);
  assert.equal(entries[0].event, "EVENT_DSP_BOOTUP");
  assert.equal(entries[0].line, 2);
  assert.equal(entries[1].kind, "version");
  assert.equal(entries[1].version, "0.0.64.58");
});

test("leaves unmapped codes unnamed", () => {
  const tuple = decodeReadmsgTuple([0x00, 0x00, 0x02]);

  assert.equal(tuple.event, null);
  assert.equal(tuple.code, 2);
});

test("parses hex byte lists in either notation", () => {
  assert.deepEqual(parseHexBytes("0x00 0x01 0x04"), [0, 1, 4]);
  assert.deepEqual(parseHexBytes("00,01,ff"), [0, 1, 255]);
  assert.throws(() => parseHexBytes("0x100"), /not a byte/);
});
