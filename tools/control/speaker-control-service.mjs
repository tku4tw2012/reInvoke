#!/usr/bin/env node
// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

import { once } from "node:events";
import net from "node:net";
import process from "node:process";

import {
  BackendUnavailableError,
  BlueAlsaCliBackend,
} from "./speaker-control-backend.mjs";
import {
  normalizeMusicVolume,
  SpeakerControlState,
} from "./speaker-control-state.mjs";
import {
  SocketReader,
  readFrame,
  write,
  writeFrame,
} from "./wamp-call.mjs";

const WAMP_HELLO = 1;
const WAMP_WELCOME = 2;
const WAMP_ERROR = 8;
const WAMP_PUBLISH = 16;
const WAMP_SUBSCRIBE = 32;
const WAMP_SUBSCRIBED = 33;
const WAMP_EVENT = 36;
const WAMP_REGISTER = 64;
const WAMP_REGISTERED = 65;
const WAMP_INVOCATION = 68;
const WAMP_YIELD = 70;

export const PROCEDURES = [
  "com.harman.extStateUpdate",
  "com.harman.musicMuteSet",
  "com.harman.musicMuteToggle",
  "com.harman.source.get-active",
  "com.harman.source.get-registered",
  "com.harman.source.register",
  "com.harman.source.start",
  "com.harman.stateGet",
  "com.harman.volumeAdjust",
  "com.harman.volumeGet",
  "com.harman.volumeSet",
];

function requireArgument(args, index, type) {
  const value = args[index];
  if (
    value === undefined ||
    (type === "number" && typeof value !== "number") ||
    (type === "string" && typeof value !== "string") ||
    (type === "boolean" && typeof value !== "boolean")
  ) {
    throw new TypeError("invalid argument format");
  }
  return value;
}

export function invokeProcedure(state, procedure, args = [], kwargs = {}) {
  switch (procedure) {
    case "com.harman.volumeGet":
      return state.volumeGet();
    case "com.harman.volumeSet":
      return state.volumeSet(requireArgument(args, 0, "number"));
    case "com.harman.volumeAdjust":
      return state.volumeAdjust(requireArgument(args, 0, "number"));
    case "com.harman.musicMuteSet":
      return state.musicMuteSet(requireArgument(args, 0, "boolean"));
    case "com.harman.musicMuteToggle":
      return state.musicMuteToggle();
    case "com.harman.stateGet":
      return state.stateGet();
    case "com.harman.extStateUpdate":
      return state.extStateUpdate(
        requireArgument(args, 0, "string"),
        requireArgument([kwargs.state], 0, "string"),
      );
    case "com.harman.source.register":
      return state.sourceRegister(requireArgument(args, 0, "string"));
    case "com.harman.source.start":
      return state.sourceStart(requireArgument(args, 0, "string"));
    case "com.harman.source.get-active":
      return state.sourceGetActive();
    case "com.harman.source.get-registered":
      return state.sourceGetRegistered();
    default:
      throw new Error(`unsupported procedure: ${procedure}`);
  }
}

function applyInputEvent(state, args) {
  if (args[0] === "volumeup") {
    return state.volumeAdjust(1);
  }
  if (args[0] === "volumedown") {
    return state.volumeAdjust(-1);
  }
  return null;
}

export class SpeakerControlController {
  constructor(state, backend = null) {
    if (
      backend !== null &&
      (typeof backend.read !== "function" ||
        typeof backend.setVolume !== "function" ||
        typeof backend.setMuted !== "function")
    ) {
      throw new TypeError("backend does not implement speaker control operations");
    }
    this.state = state;
    this.backend = backend;
    this.pending = Promise.resolve();
  }

  invoke(procedure, args = [], kwargs = {}) {
    return this.#enqueue(async () => {
      const observedEvents = await this.#sync();
      if (this.backend === null) {
        const result = invokeProcedure(this.state, procedure, args, kwargs);
        result.events.unshift(...observedEvents);
        return result;
      }

      let result;
      switch (procedure) {
        case "com.harman.volumeGet":
          this.#requirePcm();
          result = this.state.volumeGet();
          break;
        case "com.harman.volumeSet":
          result = await this.#setVolume(
            normalizeMusicVolume(requireArgument(args, 0, "number")),
          );
          break;
        case "com.harman.volumeAdjust":
          result = await this.#setVolume(
            normalizeMusicVolume(
              this.state.musicVolume +
                Math.trunc(requireArgument(args, 0, "number")),
            ),
          );
          break;
        case "com.harman.musicMuteSet":
          result = await this.#setMuted(
            requireArgument(args, 0, "boolean"),
          );
          break;
        case "com.harman.musicMuteToggle":
          result = await this.#setMuted(!this.state.musicMuted);
          break;
        default:
          result = invokeProcedure(this.state, procedure, args, kwargs);
      }
      result.events.unshift(...observedEvents);
      return result;
    });
  }

  applyInput(args) {
    if (args[0] !== "volumeup" && args[0] !== "volumedown") {
      return Promise.resolve(null);
    }
    return this.invoke("com.harman.volumeAdjust", [
      args[0] === "volumeup" ? 1 : -1,
    ]);
  }

  sync() {
    return this.#enqueue(() => this.#sync());
  }

  async #setVolume(volume) {
    await this.backend.setVolume(volume);
    await this.#sync();
    this.#requirePcm();
    return this.state.volumeSet(this.state.musicVolume);
  }

  async #setMuted(muted) {
    await this.backend.setMuted(muted);
    await this.#sync();
    this.#requirePcm();
    return this.state.musicMuteSet(this.state.musicMuted);
  }

  async #sync() {
    if (this.backend === null) {
      return [];
    }
    this.lastSnapshot = await this.backend.read();
    return this.state.reconcileBackend(this.lastSnapshot);
  }

  #requirePcm() {
    if (this.lastSnapshot?.pcmAvailable !== true) {
      throw new BackendUnavailableError("BlueALSA PCM is not available");
    }
  }

  #enqueue(operation) {
    const result = this.pending.then(operation, operation);
    this.pending = result.catch(() => {});
    return result;
  }
}

async function negotiate(socket, reader, realm) {
  await write(socket, Buffer.from([0x7f, 0xf2, 0, 0]));
  const handshake = await reader.read(4);
  if (handshake[0] !== 0x7f || (handshake[1] & 0x0f) !== 2) {
    throw new Error(`rawsocket handshake rejected: ${handshake.toString("hex")}`);
  }

  await writeFrame(socket, [
    WAMP_HELLO,
    realm,
    {
      roles: {
        callee: {},
        publisher: {},
        subscriber: {},
      },
    },
  ]);
  const welcome = await readFrame(reader);
  if (!Array.isArray(welcome) || welcome[0] !== WAMP_WELCOME) {
    throw new Error(`expected WELCOME, received ${JSON.stringify(welcome)}`);
  }
}

async function establishBindings(socket, reader) {
  const registrations = new Map();
  const pending = new Map();
  const deferredMessages = [];
  let requestId = 1;

  for (const procedure of PROCEDURES) {
    const currentRequestId = requestId++;
    pending.set(currentRequestId, {
      requestType: WAMP_REGISTER,
      responseType: WAMP_REGISTERED,
      procedure,
    });
    await writeFrame(socket, [
      WAMP_REGISTER,
      currentRequestId,
      {},
      procedure,
    ]);
  }

  const subscribeRequestId = requestId++;
  pending.set(subscribeRequestId, {
    requestType: WAMP_SUBSCRIBE,
    responseType: WAMP_SUBSCRIBED,
  });
  await writeFrame(socket, [
    WAMP_SUBSCRIBE,
    subscribeRequestId,
    {},
    "com.harman.test.inputEvent",
  ]);

  let inputSubscriptionId;
  while (pending.size > 0) {
    const message = await readFrame(reader);
    if (!Array.isArray(message)) {
      deferredMessages.push(message);
      continue;
    }

    if (message[0] === WAMP_ERROR) {
      const request = pending.get(message[2]);
      if (request === undefined) {
        deferredMessages.push(message);
        continue;
      }
      if (request.requestType !== message[1]) {
        throw new Error(`unexpected startup error: ${JSON.stringify(message)}`);
      }
      throw new Error(
        `startup request ${message[2]} failed: ${JSON.stringify(message)}`,
      );
    }

    if (message[0] !== WAMP_REGISTERED && message[0] !== WAMP_SUBSCRIBED) {
      deferredMessages.push(message);
      continue;
    }

    const request = pending.get(message[1]);
    if (request === undefined || request.responseType !== message[0]) {
      throw new Error(`unexpected startup response: ${JSON.stringify(message)}`);
    }
    pending.delete(message[1]);

    if (message[0] === WAMP_REGISTERED) {
      registrations.set(message[2], request.procedure);
    } else {
      inputSubscriptionId = message[2];
    }
  }

  return {
    registrations,
    inputSubscriptionId,
    deferredMessages,
    nextRequestId: requestId,
  };
}

async function publishEvents(socket, events, nextRequestId) {
  for (const event of events) {
    await writeFrame(socket, [
      WAMP_PUBLISH,
      nextRequestId.value++,
      {},
      event.topic,
      event.args,
      event.kwargs,
    ]);
  }
}

export async function serveSpeakerControl({
  host = "127.0.0.1",
  port = 19999,
  realm = "default",
  musicVolume = 20,
  bluetoothActive = false,
  backend = null,
  backendPollMs = 1000,
  signal,
  onReady,
} = {}) {
  const state = new SpeakerControlState({ musicVolume });
  if (bluetoothActive) {
    state.registerBluetoothSource();
  }
  const controller = new SpeakerControlController(state, backend);

  const socket = net.createConnection({ host, port });
  const abort = () => socket.destroy(new Error("service stopped"));
  signal?.addEventListener("abort", abort, { once: true });
  let pollTimer;
  let pollInFlight = false;

  try {
    await once(socket, "connect");
    const reader = new SocketReader(socket);
    await negotiate(socket, reader, realm);
    const {
      registrations,
      inputSubscriptionId,
      deferredMessages,
      nextRequestId: firstRequestId,
    } = await establishBindings(socket, reader);
    const nextRequestId = { value: firstRequestId };
    const initialEvents = await controller.sync();
    await publishEvents(socket, initialEvents, nextRequestId);

    if (backend !== null && backendPollMs > 0) {
      pollTimer = setInterval(() => {
        if (pollInFlight) {
          return;
        }
        pollInFlight = true;
        controller
          .sync()
          .then((events) => publishEvents(socket, events, nextRequestId))
          .catch((error) =>
            console.error(`speaker backend poll failed: ${error.message}`),
          )
          .finally(() => {
            pollInFlight = false;
          });
      }, backendPollMs);
      pollTimer.unref();
    }

    onReady?.({ procedures: [...PROCEDURES] });
    console.log(
      JSON.stringify({
        state: "registered",
        procedures: PROCEDURES.length,
        inputTopic: "com.harman.test.inputEvent",
      }),
    );

    while (!signal?.aborted) {
      const message =
        deferredMessages.length > 0
          ? deferredMessages.shift()
          : await readFrame(reader);
      if (!Array.isArray(message)) {
        continue;
      }

      if (message[0] === WAMP_EVENT && message[1] === inputSubscriptionId) {
        try {
          const result =
            backend === null
              ? applyInputEvent(state, message[4] ?? [])
              : await controller.applyInput(message[4] ?? []);
          if (result !== null) {
            await publishEvents(socket, result.events, nextRequestId);
          }
        } catch (error) {
          console.error(`speaker input event failed: ${error.message}`);
        }
        continue;
      }

      if (message[0] !== WAMP_INVOCATION) {
        continue;
      }
      const procedure = registrations.get(message[2]);
      if (procedure === undefined) {
        continue;
      }

      try {
        const result = await controller.invoke(
          procedure,
          message[4] ?? [],
          message[5] ?? {},
        );
        await publishEvents(socket, result.events, nextRequestId);
        await writeFrame(socket, [
          WAMP_YIELD,
          message[1],
          {},
          result.args,
          result.kwargs,
        ]);
      } catch (error) {
        await writeFrame(socket, [
          WAMP_ERROR,
          WAMP_INVOCATION,
          message[1],
          {},
          error instanceof TypeError
            ? "invalid.argument.format"
            : "com.harman.invalid-state",
        ]);
      }
    }
  } catch (error) {
    if (!signal?.aborted) {
      throw error;
    }
  } finally {
    clearInterval(pollTimer);
    signal?.removeEventListener("abort", abort);
    socket.destroy();
  }
}

function usage(exitCode = 0) {
  console.log(`Usage: speaker-control-service.mjs [options]

Options:
  --host HOST           WAMP host (default: 127.0.0.1)
  --port PORT           RawSocket port (default: 19999)
  --realm REALM         WAMP realm (default: default)
  --music-volume VALUE  Initial music volume, 0-100 (default: 20)
  --bluetooth-active    Register and activate com.harman.bluetooth
  --bluealsa-pcm PATH   Use this explicit BlueALSA PCM object path
  --bluealsactl PATH    bluealsactl executable (default: bluealsactl)
  --bluealsa-dbus NAME  BlueALSA D-Bus service suffix
  --backend-poll-ms MS  Backend observation interval (default: 1000)
  --help                Show this help`);
  process.exit(exitCode);
}

function parseOptions(argv) {
  const options = {};

  while (argv.length > 0) {
    const option = argv.shift();
    if (option === "--help") {
      usage();
    }
    if (option === "--bluetooth-active") {
      options.bluetoothActive = true;
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
      case "--music-volume":
        options.musicVolume = Number(value);
        break;
      case "--bluealsa-pcm":
        options.bluealsaPcm = value;
        break;
      case "--bluealsactl":
        options.bluealsactl = value;
        break;
      case "--bluealsa-dbus":
        options.bluealsaDbus = value;
        break;
      case "--backend-poll-ms":
        options.backendPollMs = Number(value);
        break;
      default:
        throw new Error(`unknown option: ${option}`);
    }
  }

  for (const [name, value] of [
    ["--port", options.port],
    ["--music-volume", options.musicVolume],
    ["--backend-poll-ms", options.backendPollMs],
  ]) {
    if (value !== undefined && (!Number.isInteger(value) || value < 0)) {
      throw new Error(`${name} must be a non-negative integer`);
    }
  }
  if (options.port === 0) {
    throw new Error("--port must be positive");
  }
  if (options.musicVolume !== undefined && options.musicVolume > 100) {
    throw new Error("--music-volume must not exceed 100");
  }
  return options;
}

async function main() {
  const controller = new AbortController();
  process.once("SIGINT", () => controller.abort());
  process.once("SIGTERM", () => controller.abort());
  const options = parseOptions(process.argv.slice(2));
  const backend =
    options.bluealsaPcm === undefined
      ? null
      : new BlueAlsaCliBackend({
          pcmPath: options.bluealsaPcm,
          command: options.bluealsactl,
          dbusSuffix: options.bluealsaDbus,
        });
  await serveSpeakerControl({
    ...options,
    backend,
    signal: controller.signal,
  });
}

if (process.argv[1]?.endsWith("speaker-control-service.mjs")) {
  main().catch((error) => {
    console.error(`ERROR: ${error.message}`);
    process.exitCode = 1;
  });
}
