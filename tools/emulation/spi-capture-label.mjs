#!/usr/bin/env node
// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

// Offline labeller and comparator for byte-exact SPI captures.
//
// Input is a log written by invoke-ioctl-shim.c in record mode. This tool
// reads that log after the fact, classifies every SPI_IOC_MESSAGE, reassembles
// the donor's one-byte-per-ioctl message frames, and diffs the image-download
// stream against the preserved dsp-img.ldr.
//
// It is passive by construction. It opens the log and the image file read
// only, opens no device node, and sends nothing. It does not parse, produce,
// or depend on the shim's I2C, HCI, or ALSA records, so it does not overlap
// the shim itself.
//
// Capture criteria and frame formats: docs/emulation/dsp-boundary.md

import fs from "node:fs";
import process from "node:process";

import {
  DSP_COMMANDS,
  DSP_EVENTS,
  devicePayloadRead,
  frameLength,
} from "../control/dsp-frame-decode.mjs";

// Header line: one per ioctl, request and result phases.
const HEADER = /^SPI_IOC_MESSAGE seq=(\d+) phase=(request|result) fd=(-?\d+) transfers=(\d+) result=(-?\d+) errno=(\d+)$/;

// Per-transfer line. The trailing field is tx= on request, rx= on response.
const TRANSFER = /^SPI_IOC_MESSAGE seq=(\d+) phase=(request|response) transfer=(\d+) len=(\d+) speed_hz=(\d+) delay_usecs=(\d+) bits_per_word=(\d+) cs_change=(\d+) tx_nbits=(\d+) rx_nbits=(\d+) word_delay_usecs=(\d+) (tx|rx)=(.*)$/;

// Values the donor's .data holds for the held build. A capture that disagrees
// is either a different build or a different caller.
export const EXPECTED_SPEED_HZ = 1000000;
export const EXPECTED_BITS_PER_WORD = 8;

// Observed on hardware in the 20260903T191657Z record run: the image download
// runs with no inter-word delay, message bytes carry delay_usecs=1. Static
// reading of the donor predicted 0 for both, so this table follows the capture.
export const EXPECTED_DELAY_USECS = { image: 0, message: 1 };

function expectedDelayUsecs(kind) {
  return kind === "image"
    ? EXPECTED_DELAY_USECS.image
    : EXPECTED_DELAY_USECS.message;
}

export function reverseBits(byte) {
  let out = 0;
  for (let bit = 0; bit < 8; bit++) {
    out = (out << 1) | ((byte >> bit) & 1);
  }
  return out;
}

const REVERSED = Uint8Array.from({ length: 256 }, (_, i) => reverseBits(i));

// The loader bit-reverses every byte of the image before sending it, so this
// is what the preserved file should look like on the wire.
export function expectedImageStream(image) {
  const out = new Uint8Array(image.length);
  for (let i = 0; i < image.length; i++) {
    out[i] = REVERSED[image[i]];
  }
  return out;
}

function parseHexBlob(text) {
  if (text === "-") {
    return null;
  }
  if (text.length % 2 !== 0 || !/^[0-9a-fA-F]*$/.test(text)) {
    throw new Error(`malformed byte blob: ${text}`);
  }
  const out = new Uint8Array(text.length / 2);
  for (let i = 0; i < out.length; i++) {
    out[i] = Number.parseInt(text.slice(i * 2, i * 2 + 2), 16);
  }
  return out;
}

// Pairs the request and response lines of each ioctl into one record.
export function parseCapture(text) {
  const bySeq = new Map();
  const order = [];
  const warnings = [];

  for (const [index, line] of text.split(/\r?\n/).entries()) {
    const trimmed = line.trim();
    if (!trimmed.startsWith("SPI_IOC_MESSAGE ")) {
      continue;
    }
    const lineNumber = index + 1;
    const header = HEADER.exec(trimmed);
    if (header) {
      const seq = Number(header[1]);
      let record = bySeq.get(seq);
      if (!record) {
        record = { seq, line: lineNumber, transfers: [] };
        bySeq.set(seq, record);
        order.push(record);
      }
      if (header[2] === "request") {
        record.fd = Number(header[3]);
        record.count = Number(header[4]);
      } else {
        record.result = Number(header[5]);
        record.errno = Number(header[6]);
      }
      continue;
    }
    const transfer = TRANSFER.exec(trimmed);
    if (!transfer) {
      warnings.push({ line: lineNumber, text: trimmed, why: "unparsed" });
      continue;
    }
    const seq = Number(transfer[1]);
    let record = bySeq.get(seq);
    if (!record) {
      record = { seq, line: lineNumber, transfers: [] };
      bySeq.set(seq, record);
      order.push(record);
    }
    const slot = Number(transfer[3]);
    let entry = record.transfers[slot];
    if (!entry) {
      entry = { index: slot, line: lineNumber };
      record.transfers[slot] = entry;
    }
    entry.len = Number(transfer[4]);
    entry.speedHz = Number(transfer[5]);
    entry.delayUsecs = Number(transfer[6]);
    entry.bitsPerWord = Number(transfer[7]);
    entry.csChange = Number(transfer[8]);
    const bytes = parseHexBlob(transfer[13]);
    if (transfer[12] === "tx") {
      entry.tx = bytes;
    } else {
      entry.rx = bytes;
      entry.hasResponse = true;
    }
  }

  return { records: order, warnings };
}

// The donor issues one transfer per ioctl. rx_buf is NULL for everything it
// sends, so the response line is what separates the directions:
//   rx=-  len=4  image download word
//   rx=-  len=1  host-to-DSP frame byte
//   rx=XX len=1  DSP-to-host frame byte
export function classifyTransfer(record, transfer) {
  if (record.result !== undefined && record.result < 0) {
    return "failed";
  }
  if (!transfer.hasResponse) {
    return "no-response";
  }
  const received = transfer.rx !== null && transfer.rx !== undefined;
  if (!received && transfer.len === 4) {
    return "image";
  }
  if (!received && transfer.len === 1) {
    return "tx-byte";
  }
  if (received && transfer.len === 1) {
    return "rx-byte";
  }
  return "unclassified";
}

// Walks the capture in order and cuts it into same-kind runs.
export function segment(records) {
  const runs = [];
  let current = null;
  for (const record of records) {
    for (const transfer of record.transfers) {
      if (!transfer) {
        continue;
      }
      const kind = classifyTransfer(record, transfer);
      if (!current || current.kind !== kind) {
        current = {
          kind,
          startSeq: record.seq,
          startLine: transfer.line,
          transfers: [],
        };
        runs.push(current);
      }
      current.endSeq = record.seq;
      current.transfers.push({ record, transfer });
    }
  }
  return runs;
}

function concatBytes(run, field) {
  let total = 0;
  for (const { transfer } of run.transfers) {
    total += transfer[field] ? transfer[field].length : 0;
  }
  const out = new Uint8Array(total);
  let offset = 0;
  for (const { transfer } of run.transfers) {
    if (transfer[field]) {
      out.set(transfer[field], offset);
      offset += transfer[field].length;
    }
  }
  return out;
}

function nameHostFrame(id, payload) {
  if (payload.length === 0) {
    return null;
  }
  for (const [name, spec] of DSP_COMMANDS) {
    if (spec.id === id && spec.opcode === payload[0]) {
      return name;
    }
  }
  return null;
}

// Cuts a run of one-byte transfers into frames. Both directions carry a
// five-byte header, so the length field says where the next frame starts.
export function reassembleFrames(bytes, direction) {
  const frames = [];
  let offset = 0;
  while (offset < bytes.length) {
    const remaining = bytes.length - offset;
    if (remaining < 5) {
      frames.push({
        offset,
        direction,
        id: null,
        length: null,
        payload: bytes.slice(offset),
        bytes: bytes.slice(offset),
        incomplete: true,
        truncatedHeader: true,
      });
      break;
    }
    const id = (bytes[offset] << 8) | bytes[offset + 1];
    const length = (bytes[offset + 2] << 8) | bytes[offset + 3];
    const total = frameLength(length);
    const frame = bytes.slice(offset, offset + total);
    const payloadRead = direction === "dsp-to-host"
      ? devicePayloadRead(length)
      : total - 5;
    let sum = frame[0] + frame[1] + frame[2] + frame[3];
    for (let i = 0; i < payloadRead && 5 + i < frame.length; i++) {
      sum += frame[5 + i];
    }
    const payload = frame.slice(5, 5 + length);
    const entry = {
      offset,
      direction,
      id,
      length,
      checksum: frame[4],
      checksumExpected: sum & 0xff,
      payload,
      bytes: frame,
      incomplete: frame.length !== total,
    };
    entry.checksumValid = !entry.incomplete &&
      entry.checksum === entry.checksumExpected;
    if (direction === "dsp-to-host") {
      entry.code = payload.length ? payload[0] : null;
      entry.event = entry.code === null
        ? null
        : DSP_EVENTS.get(`${id}:${entry.code}`) ?? null;
    } else {
      entry.procedure = nameHostFrame(id, payload);
    }
    frames.push(entry);
    if (entry.incomplete || total === 0) {
      break;
    }
    offset += total;
  }
  return frames;
}

// Compares one download run against the bit-reversed preserved image.
export function diffImageRun(run, expected) {
  const captured = concatBytes(run, "tx");
  const compared = Math.min(captured.length, expected.length);
  let firstMismatch = -1;
  let mismatches = 0;
  for (let i = 0; i < compared; i++) {
    if (captured[i] !== expected[i]) {
      mismatches++;
      if (firstMismatch < 0) {
        firstMismatch = i;
      }
    }
  }
  const speeds = new Map();
  for (const { transfer } of run.transfers) {
    speeds.set(transfer.speedHz, (speeds.get(transfer.speedHz) ?? 0) + 1);
  }
  // The loader restores the saved speed after the transfer at offset 1536.
  const boundaryTransfer = run.transfers[1536 / 4] ?? null;
  return {
    transfers: run.transfers.length,
    bytes: captured.length,
    expectedBytes: expected.length,
    expectedTransfers: Math.ceil(expected.length / 4),
    compared,
    mismatches,
    firstMismatch,
    identical: mismatches === 0 && captured.length === expected.length,
    prefix: mismatches === 0 && captured.length < expected.length,
    truncatedAt: captured.length < expected.length ? captured.length : null,
    trailing: captured.length > expected.length
      ? captured.length - expected.length
      : 0,
    speeds: [...speeds.entries()].map(([hz, count]) => ({ hz, count })),
    boundarySpeedHz: boundaryTransfer ? boundaryTransfer.transfer.speedHz : null,
    startLine: run.startLine,
  };
}

export function analyze(text, image) {
  const { records, warnings } = parseCapture(text);
  const runs = segment(records);
  const expected = image ? expectedImageStream(image) : null;

  const counts = new Map();
  let anomalies = [];
  for (const record of records) {
    for (const transfer of record.transfers) {
      if (!transfer) {
        continue;
      }
      const kind = classifyTransfer(record, transfer);
      counts.set(kind, (counts.get(kind) ?? 0) + 1);
      const delayExpected = expectedDelayUsecs(kind);
      if (transfer.speedHz !== EXPECTED_SPEED_HZ ||
        transfer.bitsPerWord !== EXPECTED_BITS_PER_WORD ||
        transfer.csChange !== 0 || transfer.delayUsecs !== delayExpected) {
        anomalies.push({
          seq: record.seq,
          line: transfer.line,
          kind,
          speedHz: transfer.speedHz,
          bitsPerWord: transfer.bitsPerWord,
          csChange: transfer.csChange,
          delayUsecs: transfer.delayUsecs,
          delayExpected,
        });
      }
    }
  }

  const downloads = [];
  const frames = [];
  for (const run of runs) {
    if (run.kind === "image") {
      downloads.push(expected ? diffImageRun(run, expected) : {
        transfers: run.transfers.length,
        bytes: concatBytes(run, "tx").length,
        startLine: run.startLine,
        compared: 0,
        note: "no image supplied, not compared",
      });
      continue;
    }
    if (run.kind === "tx-byte" || run.kind === "rx-byte") {
      const direction = run.kind === "tx-byte" ? "host-to-dsp" : "dsp-to-host";
      const bytes = concatBytes(run, run.kind === "tx-byte" ? "tx" : "rx");
      for (const frame of reassembleFrames(bytes, direction)) {
        frames.push({ ...frame, startLine: run.startLine, seq: run.startSeq });
      }
    }
  }

  const totalTransfers = [...counts.values()].reduce((a, b) => a + b, 0);
  return {
    ioctls: records.length,
    totalTransfers,
    counts: Object.fromEntries(counts),
    runs: runs.length,
    downloads,
    frames,
    anomalies,
    warnings,
  };
}

function hex(bytes) {
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join(" ");
}

function reportDownload(download, index) {
  const lines = [
    `download ${index + 1} (from line ${download.startLine})`,
    `  transfers ${download.transfers}` +
    (download.expectedTransfers !== undefined
      ? ` of ${download.expectedTransfers} expected`
      : ""),
    `  bytes     ${download.bytes}` +
    (download.expectedBytes !== undefined
      ? ` of ${download.expectedBytes} expected`
      : ""),
  ];
  if (download.note) {
    lines.push(`  ${download.note}`);
    return lines;
  }
  if (download.identical) {
    lines.push("  verdict   byte-identical to the bit-reversed image");
  } else if (download.prefix) {
    lines.push(
      `  verdict   clean prefix, truncated at byte ${download.truncatedAt}`,
    );
  } else {
    lines.push(
      `  verdict   ${download.mismatches} mismatched byte(s), first at ` +
        `offset ${download.firstMismatch}`,
    );
  }
  if (download.trailing) {
    lines.push(`  trailing  ${download.trailing} byte(s) beyond the image`);
  }
  const speeds = download.speeds
    .map((s) => `${s.hz} Hz x${s.count}`)
    .join(", ");
  lines.push(`  speeds    ${speeds}`);
  if (download.boundarySpeedHz !== null) {
    lines.push(`  offset 1536 transfer speed ${download.boundarySpeedHz} Hz`);
  }
  return lines;
}

function reportFrame(frame) {
  const parts = [
    frame.direction === "host-to-dsp" ? "TX" : "RX",
    frame.id === null ? "id=?" : `id=${frame.id}`,
    frame.length === null ? "len=?" : `len=${frame.length}`,
    frame.incomplete
      ? "INCOMPLETE"
      : frame.checksumValid
      ? "checksum OK"
      : `checksum BAD, expected 0x${
        frame.checksumExpected.toString(16).padStart(2, "0")
      }`,
  ];
  if (frame.procedure) {
    parts.push(frame.procedure);
  }
  if (frame.event) {
    parts.push(frame.event);
  } else if (frame.direction === "dsp-to-host" && frame.code !== null) {
    parts.push(`unmapped code 0x${frame.code.toString(16).padStart(2, "0")}`);
  }
  parts.push(`[${hex(frame.bytes)}]`);
  return `  line ${frame.startLine}: ${parts.join("  ")}`;
}

function report(result, showFrames) {
  const lines = [];
  lines.push(`ioctls            ${result.ioctls}`);
  lines.push(`transfers         ${result.totalTransfers}`);
  for (const [kind, count] of Object.entries(result.counts)) {
    lines.push(`  ${kind.padEnd(16)}${count}`);
  }
  lines.push(`contiguous runs   ${result.runs}`);
  lines.push("");
  if (result.downloads.length === 0) {
    lines.push("no image download seen");
  }
  for (const [index, download] of result.downloads.entries()) {
    lines.push(...reportDownload(download, index));
  }
  const bad = result.frames.filter((f) => !f.checksumValid);
  lines.push("");
  lines.push(
    `frames            ${result.frames.length} ` +
      `(${result.frames.length - bad.length} with a valid checksum)`,
  );
  if (showFrames) {
    for (const frame of result.frames) {
      lines.push(reportFrame(frame));
    }
  } else if (bad.length > 0) {
    lines.push("bad or incomplete frames:");
    for (const frame of bad.slice(0, 20)) {
      lines.push(reportFrame(frame));
    }
  }
  if (result.anomalies.length > 0) {
    lines.push("");
    lines.push(
      `transfers with unexpected settings: ${result.anomalies.length}`,
    );
    for (const anomaly of result.anomalies.slice(0, 10)) {
      lines.push(
        `  line ${anomaly.line}: ${anomaly.kind} speed=${anomaly.speedHz} ` +
          `bits=${anomaly.bitsPerWord} cs_change=${anomaly.csChange} ` +
          `delay=${anomaly.delayUsecs} (expected ${anomaly.delayExpected})`,
      );
    }
  }
  if (result.warnings.length > 0) {
    lines.push("");
    lines.push(`unparsed SPI lines: ${result.warnings.length}`);
  }
  return lines.join("\n");
}

function usage(exitCode = 0) {
  const stream = exitCode === 0 ? process.stdout : process.stderr;
  stream.write(`Usage:
  spi-capture-label.mjs <capture.log> [--image <dsp-img.ldr>] [options]

Options:
  --image <file>   diff the download stream against this image file
  --frames         list every reassembled frame, not just the bad ones
  --json           emit the analysis as JSON

Reads a log written by invoke-ioctl-shim.c in record mode. Opens no device
node and sends nothing. Capture criteria: docs/emulation/dsp-boundary.md
`);
  process.exit(exitCode);
}

export function main(argv) {
  if (argv.length === 0 || argv.includes("--help") || argv.includes("-h")) {
    usage(argv.length === 0 ? 1 : 0);
  }
  let capturePath = null;
  let imagePath = null;
  let showFrames = false;
  let asJson = false;
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "--image") {
      imagePath = argv[++i];
      if (!imagePath) {
        usage(1);
      }
    } else if (arg === "--frames") {
      showFrames = true;
    } else if (arg === "--json") {
      asJson = true;
    } else if (arg.startsWith("--")) {
      usage(1);
    } else if (capturePath === null) {
      capturePath = arg;
    } else {
      usage(1);
    }
  }
  if (capturePath === null) {
    usage(1);
  }
  const text = capturePath === "-"
    ? fs.readFileSync(0, "utf8")
    : fs.readFileSync(capturePath, "utf8");
  const image = imagePath ? new Uint8Array(fs.readFileSync(imagePath)) : null;
  const result = analyze(text, image);
  if (asJson) {
    process.stdout.write(
      `${
        JSON.stringify(result, (key, value) =>
          value instanceof Uint8Array ? hex(value) : value, 2)
      }\n`,
    );
    return;
  }
  process.stdout.write(`${report(result, showFrames)}\n`);
}

if (import.meta.url === `file://${process.argv[1]}`) {
  try {
    main(process.argv.slice(2));
  } catch (error) {
    if (error && error.code === "EPIPE") {
      process.exit(0);
    }
    process.stderr.write(`${error.message}\n`);
    process.exit(1);
  }
}
