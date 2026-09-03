// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

import { execFile } from "node:child_process";

/**
 * @typedef {object} SpeakerBackendSnapshot
 * @property {boolean} pcmAvailable
 * @property {number=} volume
 * @property {boolean=} muted
 * @property {boolean=} sourceConnected
 * @property {string=} transportState
 */

/**
 * @typedef {object} SpeakerControlBackend
 * @property {() => Promise<SpeakerBackendSnapshot>} read
 * @property {(volume: number) => Promise<void>} setVolume
 * @property {(muted: boolean) => Promise<void>} setMuted
 */

/**
 * @typedef {object} BlueAlsaCliBackendOptions
 * @property {string} pcmPath
 * @property {string=} command
 * @property {string=} dbusSuffix
 * @property {(command: string, args: string[]) =>
 *   Promise<{code: number, stdout: string, stderr: string}>=} run
 * @property {() => Promise<Partial<SpeakerBackendSnapshot>>=} observe
 * @property {(volume: number) => number=} toBackendVolume
 * @property {(volume: number) => number=} fromBackendVolume
 */

export class BackendUnavailableError extends Error {
  constructor(message = "speaker control backend is unavailable") {
    super(message);
    this.name = "BackendUnavailableError";
  }
}

function requireIntegerInRange(value, minimum, maximum, name) {
  if (!Number.isInteger(value) || value < minimum || value > maximum) {
    throw new RangeError(`${name} must be an integer from ${minimum} to ${maximum}`);
  }
  return value;
}

/**
 * Conversion uses nearest-integer rounding at both API boundaries. This makes
 * every WAMP percentage stable after a write/read round trip.
 */
export function percentToA2dpVolume(percent) {
  requireIntegerInRange(percent, 0, 100, "volume");
  return Math.round((percent * 127) / 100);
}

export function a2dpVolumeToPercent(volume) {
  requireIntegerInRange(volume, 0, 127, "A2DP volume");
  return Math.round((volume * 100) / 127);
}

function parseChannelValues(line, label, parseValue) {
  const value = line.slice(label.length).trim();
  const stereo = /^L:\s*(\S+)\s+R:\s*(\S+)$/.exec(value);
  if (stereo !== null) {
    return [parseValue(stereo[1]), parseValue(stereo[2])];
  }
  if (!/^\S+$/.test(value)) {
    throw new Error(`invalid BlueALSA ${label.toLowerCase()} field`);
  }
  return [parseValue(value)];
}

export function parseBlueAlsaPcmInfo(output) {
  const fields = new Map();
  for (const line of String(output).split(/\r?\n/)) {
    const separator = line.indexOf(":");
    if (separator !== -1) {
      fields.set(line.slice(0, separator), line.slice(separator + 1).trim());
    }
  }

  const channels = Number(fields.get("Channels"));
  const transport = fields.get("Transport");
  const volumeLine = String(output)
    .split(/\r?\n/)
    .find((line) => line.startsWith("Volume:"));
  const muteLine = String(output)
    .split(/\r?\n/)
    .find((line) => line.startsWith("Muted:"));

  if (
    !Number.isInteger(channels) ||
    channels < 1 ||
    channels > 2 ||
    typeof transport !== "string" ||
    (transport !== "A2DP-source" && transport !== "A2DP-sink") ||
    volumeLine === undefined ||
    muteLine === undefined
  ) {
    throw new Error("incomplete or unsupported BlueALSA PCM information");
  }

  const volumes = parseChannelValues(volumeLine, "Volume:", (value) =>
    requireIntegerInRange(Number(value), 0, 127, "A2DP volume"),
  );
  const muted = parseChannelValues(muteLine, "Muted:", (value) => {
    if (value !== "Y" && value !== "N") {
      throw new Error("invalid BlueALSA mute field");
    }
    return value === "Y";
  });

  if (volumes.length !== channels || muted.length !== channels) {
    throw new Error("BlueALSA channel count does not match volume state");
  }
  if (volumes.some((value) => value !== volumes[0])) {
    throw new Error("BlueALSA channel volumes differ");
  }
  if (muted.some((value) => value !== muted[0])) {
    throw new Error("BlueALSA channel mute states differ");
  }

  return {
    channels,
    transport,
    rawVolume: volumes[0],
    volume: a2dpVolumeToPercent(volumes[0]),
    muted: muted[0],
  };
}

export function runBlueAlsaCommand(command, args) {
  return new Promise((resolve, reject) => {
    execFile(command, args, { encoding: "utf8" }, (error, stdout, stderr) => {
      if (error === null) {
        resolve({ code: 0, stdout, stderr });
        return;
      }
      if (typeof error.code !== "number") {
        reject(error);
        return;
      }
      resolve({ code: error.code, stdout, stderr });
    });
  });
}

function validateObservation(observation) {
  if (observation === undefined) {
    return {};
  }
  if (observation === null || typeof observation !== "object") {
    throw new TypeError("backend observer must return an object");
  }
  if (
    observation.sourceConnected !== undefined &&
    typeof observation.sourceConnected !== "boolean"
  ) {
    throw new TypeError("sourceConnected must be boolean");
  }
  if (
    observation.transportState !== undefined &&
    typeof observation.transportState !== "string"
  ) {
    throw new TypeError("transportState must be string");
  }
  return observation;
}

export class BlueAlsaCliBackend {
  /** @param {BlueAlsaCliBackendOptions} options */
  constructor({
    pcmPath,
    command = "bluealsactl",
    dbusSuffix,
    run = runBlueAlsaCommand,
    observe,
    toBackendVolume = percentToA2dpVolume,
    fromBackendVolume = a2dpVolumeToPercent,
  } = {}) {
    if (typeof pcmPath !== "string" || !pcmPath.startsWith("/")) {
      throw new TypeError("pcmPath must be an explicit D-Bus object path");
    }
    if (typeof command !== "string" || command.length === 0) {
      throw new TypeError("command must be a non-empty string");
    }
    if (dbusSuffix !== undefined && typeof dbusSuffix !== "string") {
      throw new TypeError("dbusSuffix must be a string");
    }
    if (typeof run !== "function") {
      throw new TypeError("run must be a function");
    }
    if (observe !== undefined && typeof observe !== "function") {
      throw new TypeError("observe must be a function");
    }
    if (
      typeof toBackendVolume !== "function" ||
      typeof fromBackendVolume !== "function"
    ) {
      throw new TypeError("volume mappings must be functions");
    }

    this.pcmPath = pcmPath;
    this.command = command;
    this.dbusSuffix = dbusSuffix;
    this.run = run;
    this.observe = observe;
    this.toBackendVolume = toBackendVolume;
    this.fromBackendVolume = fromBackendVolume;
  }

  async read() {
    const observation = validateObservation(await this.observe?.());
    const result = await this.#run(["info", this.pcmPath]);
    if (result.code !== 0) {
      return { ...observation, pcmAvailable: false };
    }
    const info = parseBlueAlsaPcmInfo(result.stdout);
    const rawVolume = info.rawVolume;
    const volume = this.fromBackendVolume(rawVolume);
    requireIntegerInRange(volume, 0, 100, "mapped volume");
    return {
      ...observation,
      pcmAvailable: true,
      volume,
      muted: info.muted,
    };
  }

  async setVolume(volume) {
    requireIntegerInRange(volume, 0, 100, "volume");
    const raw = String(
      requireIntegerInRange(
        this.toBackendVolume(volume),
        0,
        127,
        "mapped A2DP volume",
      ),
    );
    await this.#runMutation(["volume", this.pcmPath, raw, raw]);
  }

  async setMuted(muted) {
    if (typeof muted !== "boolean") {
      throw new TypeError("muted must be boolean");
    }
    const value = muted ? "y" : "n";
    await this.#runMutation(["mute", this.pcmPath, value, value]);
  }

  async #runMutation(args) {
    const result = await this.#run(args);
    if (result.code !== 0) {
      throw new BackendUnavailableError(
        String(result.stderr ?? "").trim() ||
          `bluealsactl exited with status ${result.code}`,
      );
    }
  }

  #run(args) {
    const options =
      this.dbusSuffix === undefined ? args : [`--dbus=${this.dbusSuffix}`, ...args];
    return this.run(this.command, options);
  }
}
