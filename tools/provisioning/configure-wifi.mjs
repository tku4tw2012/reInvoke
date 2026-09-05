#!/usr/bin/env node
// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT

import { createHash, timingSafeEqual } from "node:crypto";
import http from "node:http";
import https from "node:https";
import { spawn } from "node:child_process";
import { pathToFileURL } from "node:url";

const MAX_COMMAND_OUTPUT = 64 * 1024;
const MAX_DESCRIPTOR_BYTES = 16 * 1024;
const MAX_RESPONSE_BYTES = 16 * 1024;

const USAGE = `Usage: configure-wifi.mjs [options]

Required:
  --ap-profile NAME       Preconfigured NetworkManager AP profile
  --target-profile NAME   Existing NetworkManager profile to send
  --descriptor-url URL    HTTP URL of the bootstrap descriptor

Options:
  --nmcli COMMAND         NetworkManager CLI executable (default: nmcli)
  --timeout-seconds N     Per-operation timeout (default: 30)
  --help                  Show this help
`;

class UsageError extends Error {}

function staticError(message) {
  return new Error(message);
}

export function parseArgs(argv) {
  const options = {
    nmcli: "nmcli",
    timeoutMilliseconds: 30_000,
  };
  const valueOptions = new Map([
    ["--ap-profile", "apProfile"],
    ["--target-profile", "targetProfile"],
    ["--descriptor-url", "descriptorURL"],
    ["--nmcli", "nmcli"],
    ["--timeout-seconds", "timeoutSeconds"],
  ]);

  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--help") {
      options.help = true;
      continue;
    }
    const property = valueOptions.get(argument);
    if (!property) {
      throw new UsageError("unknown option");
    }
    index += 1;
    if (index >= argv.length || argv[index].length === 0) {
      throw new UsageError("option requires a value");
    }
    options[property] = argv[index];
  }

  if (options.help) {
    return options;
  }
  if (!options.apProfile || !options.targetProfile || !options.descriptorURL) {
    throw new UsageError("required option is missing");
  }
  if (options.apProfile.includes("\0") ||
      options.targetProfile.includes("\0") ||
      options.nmcli.includes("\0")) {
    throw new UsageError("option contains an invalid character");
  }
  if (options.timeoutSeconds !== undefined) {
    if (!/^[1-9][0-9]*$/.test(options.timeoutSeconds)) {
      throw new UsageError("timeout must be a positive integer");
    }
    const seconds = Number(options.timeoutSeconds);
    if (!Number.isSafeInteger(seconds) || seconds > 300) {
      throw new UsageError("timeout must not exceed 300 seconds");
    }
    options.timeoutMilliseconds = seconds * 1000;
  }
  delete options.timeoutSeconds;

  let descriptorURL;
  try {
    descriptorURL = new URL(options.descriptorURL);
  } catch {
    throw new UsageError("descriptor URL is invalid");
  }
  if (descriptorURL.protocol !== "http:" ||
      descriptorURL.username ||
      descriptorURL.password) {
    throw new UsageError("descriptor URL must be unauthenticated HTTP");
  }
  options.descriptorURL = descriptorURL.toString();
  return options;
}

export class NetworkManagerCommands {
  constructor(executable, timeoutMilliseconds, options = {}) {
    this.executable = executable;
    this.timeoutMilliseconds = timeoutMilliseconds;
    this.spawnProcess = options.spawnProcess ?? spawn;
    this.terminationGraceMilliseconds =
      options.terminationGraceMilliseconds ?? 1000;
    this.killWaitMilliseconds = options.killWaitMilliseconds ?? 1000;
  }

  run(argumentsList, signal) {
    if (signal?.aborted) {
      return Promise.reject(staticError("NetworkManager command cancelled"));
    }
    return new Promise((resolve, reject) => {
      let child;
      try {
        child = this.spawnProcess(this.executable, argumentsList, {
          shell: false,
          stdio: ["ignore", "pipe", "ignore"],
          windowsHide: true,
        });
      } catch {
        reject(staticError("NetworkManager command could not start"));
        return;
      }
      const chunks = [];
      let length = 0;
      let settled = false;
      let terminating = false;
      let terminationMessage = "";
      let escalationTimer;
      let killWaitTimer;

      const scrub = () => {
        for (const chunk of chunks) {
          chunk.fill(0);
        }
        chunks.length = 0;
        length = 0;
      };
      const cleanup = () => {
        clearTimeout(commandTimer);
        clearTimeout(escalationTimer);
        clearTimeout(killWaitTimer);
        signal?.removeEventListener("abort", abortHandler);
      };
      const finishReject = (message) => {
        if (settled) {
          return;
        }
        settled = true;
        cleanup();
        scrub();
        reject(staticError(message));
      };
      const beginTermination = (message) => {
        if (settled || terminating) {
          return;
        }
        terminating = true;
        terminationMessage = message;
        clearTimeout(commandTimer);
        scrub();
        escalationTimer = setTimeout(() => {
          if (settled) {
            return;
          }
          killWaitTimer = setTimeout(() => {
            finishReject(terminationMessage);
          }, this.killWaitMilliseconds);
          try {
            child.kill("SIGKILL");
          } catch {
            finishReject(terminationMessage);
          }
        }, this.terminationGraceMilliseconds);
        try {
          child.kill("SIGTERM");
        } catch {
          finishReject(terminationMessage);
        }
      };
      const abortHandler = () => {
        beginTermination("NetworkManager command cancelled");
      };
      const commandTimer = setTimeout(() => {
        beginTermination("NetworkManager command timed out");
      }, this.timeoutMilliseconds);
      signal?.addEventListener("abort", abortHandler, { once: true });

      child.stdout.on("data", (chunk) => {
        if (settled || terminating) {
          chunk.fill(0);
          return;
        }
        length += chunk.length;
        if (length > MAX_COMMAND_OUTPUT) {
          chunk.fill(0);
          beginTermination("NetworkManager output was too large");
          return;
        }
        chunks.push(chunk);
      });
      child.once("error", () => {
        finishReject(
          terminating
            ? terminationMessage
            : "NetworkManager command could not start",
        );
      });
      child.once("close", (code) => {
        if (settled) {
          scrub();
          return;
        }
        if (terminating) {
          finishReject(terminationMessage);
          return;
        }
        settled = true;
        cleanup();
        if (code !== 0) {
          scrub();
          reject(staticError("NetworkManager command failed"));
          return;
        }
        const output = Buffer.concat(chunks, length);
        scrub();
        resolve(output);
      });
    });
  }
}

export class DescriptorHTTPClient {
  constructor(timeoutMilliseconds, options = {}) {
    this.timeoutMilliseconds = timeoutMilliseconds;
    this.get = options.get ?? http.get;
  }

  fetch(descriptorURL, signal) {
    if (signal?.aborted) {
      return Promise.reject(staticError("descriptor request cancelled"));
    }
    return new Promise((resolve, reject) => {
      let request;
      let settled = false;
      let chunks = [];
      let length = 0;
      const cleanup = () => {
        clearTimeout(wallClockTimer);
        signal?.removeEventListener("abort", abortHandler);
        for (const chunk of chunks) {
          chunk.fill(0);
        }
        chunks = [];
        length = 0;
      };
      const finishReject = (message) => {
        if (settled) {
          return;
        }
        settled = true;
        cleanup();
        reject(staticError(message));
      };
      const abortHandler = () => {
        finishReject("descriptor request cancelled");
        request?.destroy();
      };
      const wallClockTimer = setTimeout(() => {
        finishReject("descriptor request timed out");
        request?.destroy();
      }, this.timeoutMilliseconds);
      signal?.addEventListener("abort", abortHandler, { once: true });
      try {
        request = this.get(descriptorURL, {
          headers: { Accept: "application/json" },
        });
      } catch {
        finishReject("descriptor request failed");
        return;
      }
      request.once("error", () => {
        finishReject("descriptor request failed");
      });
      request.once("response", (response) => {
        if (response.statusCode !== 200) {
          response.resume();
          finishReject("descriptor request was rejected");
          return;
        }
        response.on("data", (chunk) => {
          if (settled) {
            chunk.fill(0);
            return;
          }
          length += chunk.length;
          if (length > MAX_DESCRIPTOR_BYTES) {
            chunk.fill(0);
            finishReject("descriptor was too large");
            response.destroy();
            return;
          }
          chunks.push(chunk);
        });
        response.once("error", () => {
          finishReject("descriptor response failed");
        });
        response.once("end", () => {
          if (settled) {
            return;
          }
          const content = Buffer.concat(chunks, length);
          try {
            const value = JSON.parse(content.toString("utf8"));
            settled = true;
            cleanup();
            resolve(value);
          } catch {
            finishReject("descriptor was not valid JSON");
          } finally {
            content.fill(0);
          }
        });
      });
    });
  }
}

export class PinnedTLSClient {
  constructor(timeoutMilliseconds) {
    this.timeoutMilliseconds = timeoutMilliseconds;
  }

  post(urlText, fingerprint, token, payload, signal) {
    if (signal?.aborted) {
      return Promise.reject(staticError("provisioning request cancelled"));
    }
    return new Promise((resolve, reject) => {
      const target = new URL("/v1/wifi", urlText);
      const expectedFingerprint = Buffer.from(fingerprint, "hex");
      let verified = false;
      let settled = false;
      const cleanup = () => {
        clearTimeout(timer);
        signal?.removeEventListener("abort", abortHandler);
        expectedFingerprint.fill(0);
      };
      const finishReject = (message) => {
        if (!settled) {
          settled = true;
          cleanup();
          reject(staticError(message));
        }
      };
      const request = https.request(target, {
        method: "POST",
        agent: false,
        rejectUnauthorized: false,
        minVersion: "TLSv1.3",
        headers: {
          Accept: "application/json",
          Authorization: `Bearer ${token}`,
          "Content-Type": "application/json",
          "Content-Length": payload.length,
        },
      });
      const timer = setTimeout(() => {
        finishReject("provisioning request timed out");
        request.destroy();
      }, this.timeoutMilliseconds);
      const abortHandler = () => {
        finishReject("provisioning request cancelled");
        request.destroy();
      };
      signal?.addEventListener("abort", abortHandler, { once: true });
      request.once("socket", (socket) => {
        socket.once("secureConnect", () => {
          const certificate = socket.getPeerCertificate(true);
          if (!certificate?.raw) {
            request.destroy();
            finishReject("provisioning certificate was unavailable");
            return;
          }
          const actualFingerprint = createHash("sha256")
            .update(certificate.raw)
            .digest();
          if (actualFingerprint.length !== expectedFingerprint.length ||
              !timingSafeEqual(actualFingerprint, expectedFingerprint)) {
            actualFingerprint.fill(0);
            request.destroy();
            finishReject("provisioning certificate did not match");
            return;
          }
          actualFingerprint.fill(0);
          verified = true;
          request.end(payload);
        });
      });
      request.once("response", (response) => {
        let received = 0;
        response.on("data", (chunk) => {
          received += chunk.length;
          if (received > MAX_RESPONSE_BYTES) {
            response.destroy();
            finishReject("provisioning response was too large");
          }
        });
        response.once("error", () => {
          finishReject("provisioning response failed");
        });
        response.once("end", () => {
          clearTimeout(timer);
          if (!verified) {
            finishReject("provisioning certificate was not verified");
          } else if (response.statusCode !== 202) {
            finishReject("provisioning request was rejected");
          } else if (!settled) {
            settled = true;
            cleanup();
            resolve();
          }
        });
      });
      request.once("error", () => {
        finishReject("provisioning TLS request failed");
      });
    });
  }
}

function normalizeOutput(output) {
  if (Buffer.isBuffer(output)) {
    return output;
  }
  return Buffer.from(String(output), "utf8");
}

async function captureProfileProperty(commands, profile, property, signal) {
  const output = normalizeOutput(await commands.run([
    "--terse",
    "--escape",
    "no",
    "--show-secrets",
    "--get-values",
    property,
    "connection",
    "show",
    "id",
    profile,
  ], signal));
  try {
    return output.toString("utf8").replace(/\r?\n$/, "");
  } finally {
    output.fill(0);
  }
}

async function activeWiFiUUID(commands, signal) {
  const output = normalizeOutput(await commands.run([
    "--terse",
    "--escape",
    "no",
    "--fields",
    "UUID,TYPE",
    "connection",
    "show",
    "--active",
  ], signal));
  try {
    for (const line of output.toString("utf8").split(/\r?\n/)) {
      const separator = line.indexOf(":");
      if (separator < 1) {
        continue;
      }
      const uuid = line.slice(0, separator);
      const type = line.slice(separator + 1);
      if ((type === "802-11-wireless" || type === "wifi") &&
          /^[0-9a-fA-F-]{32,36}$/.test(uuid)) {
        return uuid;
      }
    }
    return null;
  } finally {
    output.fill(0);
  }
}

function validateTargetCredentials(ssid, passphrase) {
  const ssidLength = Buffer.byteLength(ssid, "utf8");
  if (ssidLength < 1 || ssidLength > 32 || /[\0\r\n]/.test(ssid)) {
    throw staticError("target profile has an invalid SSID");
  }
  const passphraseLength = Buffer.byteLength(passphrase, "utf8");
  if (passphraseLength < 8 ||
      passphraseLength > 63 ||
      passphrase === "<hidden>" ||
      /[\0\r\n]/.test(passphrase)) {
    throw staticError("target profile has an invalid WPA2 passphrase");
  }
}

export function validateDescriptor(value, bootstrapURL) {
  if (!value || typeof value !== "object" || Array.isArray(value) ||
      typeof value.url !== "string" ||
      typeof value.token !== "string" ||
      typeof value.certificate_sha256 !== "string") {
    throw staticError("descriptor fields were invalid");
  }
  const allowedFields = new Set([
    "url",
    "token",
    "certificate_sha256",
    "expires_after_seconds",
    "expires_utc",
  ]);
  for (const field of Object.keys(value)) {
    if (!allowedFields.has(field)) {
      throw staticError("descriptor contained an unknown field");
    }
  }
  let secureURL;
  try {
    secureURL = new URL(value.url);
  } catch {
    throw staticError("descriptor TLS URL was invalid");
  }
  const bootstrap = new URL(bootstrapURL);
  if (secureURL.protocol !== "https:" ||
      secureURL.username ||
      secureURL.password ||
      secureURL.hostname !== bootstrap.hostname) {
    throw staticError("descriptor TLS endpoint was invalid");
  }
  if (!/^[A-Za-z0-9_-]{32,128}$/.test(value.token) ||
      !/^[0-9a-fA-F]{64}$/.test(value.certificate_sha256)) {
    throw staticError("descriptor authorization data was invalid");
  }
  if (!Number.isSafeInteger(value.expires_after_seconds) ||
      value.expires_after_seconds < 1 ||
      value.expires_after_seconds > 900) {
    throw staticError("descriptor expiry was invalid");
  }
  return {
    url: secureURL.toString(),
    token: value.token,
    fingerprint: value.certificate_sha256.toLowerCase(),
  };
}

export async function configureWiFi(options, adapters, signal) {
  const commands = adapters.commands;
  const descriptorHTTP = adapters.http;
  const pinnedTLS = adapters.tls;
  let priorUUID = null;
  let apActivationAttempted = false;
  let ssid = "";
  let passphrase = "";
  let payload;
  let operationError;

  try {
    priorUUID = await activeWiFiUUID(commands, signal);
    ssid = await captureProfileProperty(
      commands,
      options.targetProfile,
      "802-11-wireless.ssid",
      signal,
    );
    passphrase = await captureProfileProperty(
      commands,
      options.targetProfile,
      "802-11-wireless-security.psk",
      signal,
    );
    const hiddenText = await captureProfileProperty(
      commands,
      options.targetProfile,
      "802-11-wireless.hidden",
      signal,
    );
    validateTargetCredentials(ssid, passphrase);
    const hidden = hiddenText.trim().toLowerCase() === "yes";

    apActivationAttempted = true;
    await commands.run([
      "connection",
      "up",
      "id",
      options.apProfile,
    ], signal);

    const rawDescriptor = await descriptorHTTP.fetch(
      options.descriptorURL,
      signal,
    );
    const descriptor = validateDescriptor(
      rawDescriptor,
      options.descriptorURL,
    );
    payload = Buffer.from(JSON.stringify({
      ssid,
      passphrase,
      security: "wpa2-psk",
      hidden,
    }), "utf8");
    await pinnedTLS.post(
      descriptor.url,
      descriptor.fingerprint,
      descriptor.token,
      payload,
      signal,
    );
  } catch (error) {
    operationError = error;
  } finally {
    if (payload) {
      payload.fill(0);
    }
    ssid = "";
    passphrase = "";
    if (apActivationAttempted) {
      try {
        if (priorUUID) {
          await commands.run([
            "connection",
            "up",
            "uuid",
            priorUUID,
          ]);
        } else {
          await commands.run([
            "connection",
            "down",
            "id",
            options.apProfile,
          ]);
        }
      } catch {
        throw staticError("prior NetworkManager state could not be restored");
      }
    }
  }
  if (operationError) {
    throw operationError;
  }
}

export async function main(argv, dependencies = {}) {
  const runtime = dependencies.runtime ?? process;
  let options;
  try {
    options = parseArgs(argv);
  } catch (error) {
    if (error instanceof UsageError) {
      runtime.stderr.write(`ERROR: ${error.message}\n${USAGE}`);
      return 2;
    }
    throw error;
  }
  if (options.help) {
    runtime.stdout.write(USAGE);
    return 0;
  }

  const adapters = {
    commands: dependencies.commands ??
      new NetworkManagerCommands(
        options.nmcli,
        options.timeoutMilliseconds,
      ),
    http: dependencies.http ??
      new DescriptorHTTPClient(options.timeoutMilliseconds),
    tls: dependencies.tls ??
      new PinnedTLSClient(options.timeoutMilliseconds),
  };
  const abortController = new AbortController();
  let signalExitCode = 0;
  const handleSIGINT = () => {
    signalExitCode = 130;
    abortController.abort();
  };
  const handleSIGTERM = () => {
    signalExitCode = 143;
    abortController.abort();
  };
  runtime.on("SIGINT", handleSIGINT);
  runtime.on("SIGTERM", handleSIGTERM);
  try {
    await configureWiFi(options, adapters, abortController.signal);
    if (signalExitCode !== 0) {
      return signalExitCode;
    }
    runtime.stdout.write(
      "Provisioning request accepted; network restored.\n",
    );
    return 0;
  } catch {
    if (signalExitCode !== 0) {
      return signalExitCode;
    }
    runtime.stderr.write("ERROR: Wi-Fi provisioning failed.\n");
    return 1;
  } finally {
    runtime.removeListener("SIGINT", handleSIGINT);
    runtime.removeListener("SIGTERM", handleSIGTERM);
  }
}

if (process.argv[1] &&
    pathToFileURL(process.argv[1]).href === import.meta.url) {
  process.exitCode = await main(process.argv.slice(2));
}
