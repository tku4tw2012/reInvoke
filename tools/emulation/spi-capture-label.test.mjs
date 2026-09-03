// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import test from "node:test";

import {
  analyze,
  classifyTransfer,
  expectedImageStream,
  parseCapture,
  reassembleFrames,
  reverseBits,
} from "./spi-capture-label.mjs";

let sequence = 0;

function hex(bytes) {
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

// Emits exactly what record_spi_transfers in invoke-ioctl-shim.c writes for a
// single-transfer SPI_IOC_MESSAGE(1).
function ioctl(
  { tx = null, rx = null, len, speedHz = 1000000, result = 1, delayUsecs },
) {
  const seq = ++sequence;
  // Hardware uses no inter-word delay for the 4-byte image words and 1 us for
  // single-byte message traffic.
  const delay = delayUsecs ?? (len === 4 ? 0 : 1);
  const settings = `speed_hz=${speedHz} delay_usecs=${delay} bits_per_word=8 ` +
    `cs_change=0 tx_nbits=0 rx_nbits=0 word_delay_usecs=0`;
  const txBlob = tx ? hex(tx) : "00".repeat(len);
  const rxBlob = rx ? hex(rx) : "-";
  return [
    `SPI_IOC_MESSAGE seq=${seq} phase=request fd=7 transfers=1 ` +
    `result=0 errno=0`,
    `SPI_IOC_MESSAGE seq=${seq} phase=request transfer=0 len=${len} ` +
    `${settings} tx=${txBlob}`,
    `SPI_IOC_MESSAGE seq=${seq} phase=result fd=7 transfers=1 ` +
    `result=${result} errno=0`,
    `SPI_IOC_MESSAGE seq=${seq} phase=response transfer=0 len=${len} ` +
    `${settings} rx=${rxBlob}`,
  ].join("\n");
}

function downloadLines(image) {
  const stream = expectedImageStream(image);
  const lines = [];
  for (let offset = 0; offset < stream.length; offset += 4) {
    lines.push(ioctl({ tx: stream.slice(offset, offset + 4), len: 4 }));
  }
  return lines;
}

function sendLines(frame) {
  return Array.from(frame, (byte) => ioctl({ tx: [byte], len: 1 }));
}

function receiveLines(frame) {
  return Array.from(frame, (byte) => ioctl({ rx: [byte], len: 1 }));
}

const GET_VER = Uint8Array.from([0, 0, 0, 1, 9, 8, 0, 0]);
const BOOTUP = Uint8Array.from([0, 1, 0, 1, 6, 4, 0, 0]);

test("reverses bits the way the loader does", () => {
  assert.equal(reverseBits(0x01), 0x80);
  assert.equal(reverseBits(0x80), 0x01);
  assert.equal(reverseBits(0xa7), 0xe5);
  assert.deepEqual(
    Array.from(expectedImageStream(Uint8Array.from([0x01, 0x02]))),
    [0x80, 0x40],
  );
});

test("pairs request and result phases into one record", () => {
  const { records, warnings } = parseCapture(ioctl({ tx: [1], len: 1 }));

  assert.equal(records.length, 1);
  assert.equal(records[0].transfers.length, 1);
  assert.equal(records[0].result, 1);
  assert.equal(records[0].transfers[0].len, 1);
  assert.equal(warnings.length, 0);
});

test("separates the three directions by the response line", () => {
  const record = { result: 1 };

  assert.equal(
    classifyTransfer(record, { hasResponse: true, len: 4, rx: null }),
    "image",
  );
  assert.equal(
    classifyTransfer(record, { hasResponse: true, len: 1, rx: null }),
    "tx-byte",
  );
  assert.equal(
    classifyTransfer(record, {
      hasResponse: true,
      len: 1,
      rx: Uint8Array.from([4]),
    }),
    "rx-byte",
  );
  assert.equal(
    classifyTransfer({ result: -1 }, { hasResponse: true, len: 1 }),
    "failed",
  );
});

test("confirms a complete download byte for byte", () => {
  const image = Uint8Array.from({ length: 64 }, (_, i) => (i * 7) & 0xff);
  const result = analyze(downloadLines(image).join("\n"), image);

  assert.equal(result.downloads.length, 1);
  assert.equal(result.downloads[0].identical, true);
  assert.equal(result.downloads[0].mismatches, 0);
  assert.equal(result.downloads[0].transfers, 16);
  assert.equal(result.downloads[0].expectedTransfers, 16);
});

test("reports a truncated download as a clean prefix", () => {
  const image = Uint8Array.from({ length: 64 }, (_, i) => (i * 7) & 0xff);
  const lines = downloadLines(image).slice(0, 10);
  const result = analyze(lines.join("\n"), image);

  assert.equal(result.downloads[0].identical, false);
  assert.equal(result.downloads[0].prefix, true);
  assert.equal(result.downloads[0].truncatedAt, 40);
});

test("locates the first byte that differs from the image", () => {
  const image = Uint8Array.from({ length: 64 }, (_, i) => (i * 7) & 0xff);
  const lines = downloadLines(image);
  lines[3] = lines[3].replace(/tx=[0-9a-f]{8}/, "tx=deadbeef");
  const result = analyze(lines.join("\n"), image);

  assert.equal(result.downloads[0].mismatches > 0, true);
  assert.equal(result.downloads[0].firstMismatch, 12);
});

test("reassembles a host frame from one-byte transfers", () => {
  const result = analyze(sendLines(GET_VER).join("\n"), null);

  assert.equal(result.counts["tx-byte"], 8);
  assert.equal(result.frames.length, 1);
  assert.equal(result.frames[0].direction, "host-to-dsp");
  assert.equal(result.frames[0].procedure, "com.harman.dsp.getVer");
  assert.equal(result.frames[0].checksumValid, true);
});

test("reassembles a device frame and names the event", () => {
  const result = analyze(receiveLines(BOOTUP).join("\n"), null);

  assert.equal(result.counts["rx-byte"], 8);
  assert.equal(result.frames.length, 1);
  assert.equal(result.frames[0].direction, "dsp-to-host");
  assert.equal(result.frames[0].event, "EVENT_DSP_BOOTUP");
  assert.equal(result.frames[0].checksumValid, true);
});

test("flags a frame whose checksum does not add up", () => {
  const bad = Uint8Array.from(BOOTUP);
  bad[4] = 0x00;
  const frames = reassembleFrames(bad, "dsp-to-host");

  assert.equal(frames.length, 1);
  assert.equal(frames[0].checksumValid, false);
  assert.equal(frames[0].checksumExpected, 6);
});

test("accounts for every transfer in a mixed capture", () => {
  const image = Uint8Array.from({ length: 32 }, (_, i) => i);
  const lines = [
    ...downloadLines(image),
    ...sendLines(GET_VER),
    ...receiveLines(BOOTUP),
  ];
  const result = analyze(lines.join("\n"), image);
  const total = Object.values(result.counts).reduce((a, b) => a + b, 0);

  assert.equal(result.ioctls, 8 + 8 + 8);
  assert.equal(total, result.totalTransfers);
  assert.equal(result.counts.image, 8);
  assert.equal(result.counts["tx-byte"], 8);
  assert.equal(result.counts["rx-byte"], 8);
  assert.equal(result.frames.length, 2);
  assert.equal(result.runs, 3);
});

test("flags transfers that disagree with the donor settings", () => {
  const lines = [
    ioctl({ tx: [1], len: 1 }),
    ioctl({ tx: [2], len: 1, speedHz: 500000 }),
  ];
  const result = analyze(lines.join("\n"), null);

  assert.equal(result.anomalies.length, 1);
  assert.equal(result.anomalies[0].speedHz, 500000);
});

test("accepts the inter-word delays seen on hardware", () => {
  const lines = [
    ioctl({ tx: [0, 0, 0, 0], len: 4 }),
    ioctl({ tx: [1], len: 1 }),
    ioctl({ tx: [1], len: 1, delayUsecs: 0 }),
  ];
  const result = analyze(lines.join("\n"), null);

  assert.equal(result.anomalies.length, 1);
  assert.equal(result.anomalies[0].delayUsecs, 0);
  assert.equal(result.anomalies[0].delayExpected, 1);
});

test("keeps the direction of a run too short to hold a header", () => {
  const result = analyze(ioctl({ tx: [0x00], len: 1 }), null);

  assert.equal(result.frames.length, 1);
  assert.equal(result.frames[0].direction, "host-to-dsp");
  assert.equal(result.frames[0].incomplete, true);
  assert.equal(result.frames[0].truncatedHeader, true);
  assert.equal(result.frames[0].id, null);
});
