// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT

import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import test from "node:test";

import {
  configureWiFi,
  DescriptorHTTPClient,
  main,
  NetworkManagerCommands,
  parseArgs,
  validateDescriptor,
} from "./configure-wifi.mjs";

const descriptor = {
  url: "https://bootstrap.invalid:9443",
  token: "abcdefghijklmnopqrstuvwxyz_ABCDE-1234567890",
  certificate_sha256: "a".repeat(64),
  expires_after_seconds: 300,
};

class MockCommands {
  constructor({ active = true, failUp = false } = {}) {
    this.active = active;
    this.failUp = failUp;
    this.calls = [];
  }

  async run(argumentsList) {
    this.calls.push([...argumentsList]);
    const property = [
      "802-11-wireless.ssid",
      "802-11-wireless-security.psk",
      "802-11-wireless.hidden",
    ].find((candidate) => argumentsList.includes(candidate));
    if (argumentsList.includes("--active")) {
      return this.active
        ? Buffer.from(
          "12345678-1234-1234-1234-123456789abc:802-11-wireless\n",
        )
        : Buffer.alloc(0);
    }
    if (property === "802-11-wireless.ssid") {
      return Buffer.from("network-name\n");
    }
    if (property === "802-11-wireless-security.psk") {
      return Buffer.from("network-passphrase\n");
    }
    if (property === "802-11-wireless.hidden") {
      return Buffer.from("no\n");
    }
    if (argumentsList[0] === "connection" &&
        argumentsList[1] === "up" &&
        argumentsList[2] === "id" &&
        this.failUp) {
      throw new Error("mock AP activation failure");
    }
    return Buffer.alloc(0);
  }
}

class MockHTTP {
  constructor({ value = descriptor, error = null } = {}) {
    this.value = value;
    this.error = error;
    this.urls = [];
  }

  async fetch(url) {
    this.urls.push(url);
    if (this.error) {
      throw this.error;
    }
    return this.value;
  }
}

class MockTLS {
  constructor({ error = null } = {}) {
    this.error = error;
    this.calls = [];
  }

  async post(url, fingerprint, token, payload) {
    this.calls.push({
      url,
      fingerprint,
      token,
      payload: Buffer.from(payload),
    });
    if (this.error) {
      throw this.error;
    }
  }
}

function options() {
  return {
    apProfile: "ap-profile",
    targetProfile: "target-profile",
    descriptorURL: "http://bootstrap.invalid:9080/provisioning.json",
    nmcli: "nmcli",
    timeoutMilliseconds: 30_000,
  };
}

test("parseArgs accepts generic required options", () => {
  const parsed = parseArgs([
    "--ap-profile",
    "ap-profile",
    "--target-profile",
    "target-profile",
    "--descriptor-url",
    "http://bootstrap.invalid/provisioning.json",
    "--timeout-seconds",
    "45",
  ]);
  assert.equal(parsed.apProfile, "ap-profile");
  assert.equal(parsed.targetProfile, "target-profile");
  assert.equal(parsed.timeoutMilliseconds, 45_000);
});

test("parseArgs rejects non-HTTP descriptor transport", () => {
  assert.throws(() => parseArgs([
    "--ap-profile",
    "ap-profile",
    "--target-profile",
    "target-profile",
    "--descriptor-url",
    "https://bootstrap.invalid/provisioning.json",
  ]), /unauthenticated HTTP/);
});

test("validateDescriptor enforces host, fingerprint, and expiry", () => {
  const result = validateDescriptor(
    descriptor,
    "http://bootstrap.invalid/provisioning.json",
  );
  assert.equal(result.url, new URL(descriptor.url).toString());
  assert.equal(result.fingerprint, descriptor.certificate_sha256);

  assert.throws(() => validateDescriptor({
    ...descriptor,
    url: "https://other.invalid",
  }, "http://bootstrap.invalid/provisioning.json"), /endpoint/);
  assert.throws(() => validateDescriptor({
    ...descriptor,
    certificate_sha256: "short",
  }, "http://bootstrap.invalid/provisioning.json"), /authorization/);
});

test("workflow keeps credentials out of argv and restores prior profile", async () => {
  const commands = new MockCommands();
  const http = new MockHTTP();
  const tls = new MockTLS();

  await configureWiFi(options(), { commands, http, tls });

  assert.equal(http.urls.length, 1);
  assert.equal(tls.calls.length, 1);
  assert.deepEqual(JSON.parse(tls.calls[0].payload.toString("utf8")), {
    ssid: "network-name",
    passphrase: "network-passphrase",
    security: "wpa2-psk",
    hidden: false,
  });
  const allArguments = commands.calls.flat();
  assert(!allArguments.includes("network-name"));
  assert(!allArguments.includes("network-passphrase"));
  assert.deepEqual(commands.calls.at(-1), [
    "connection",
    "up",
    "uuid",
    "12345678-1234-1234-1234-123456789abc",
  ]);
});

test("workflow restores prior profile after TLS failure", async () => {
  const commands = new MockCommands();
  const tls = new MockTLS({
    error: new Error("mock TLS failure"),
  });

  await assert.rejects(
    configureWiFi(options(), {
      commands,
      http: new MockHTTP(),
      tls,
    }),
    /mock TLS failure/,
  );
  assert.deepEqual(commands.calls.at(-1), [
    "connection",
    "up",
    "uuid",
    "12345678-1234-1234-1234-123456789abc",
  ]);
});

test("workflow deactivates AP when no Wi-Fi profile was active", async () => {
  const commands = new MockCommands({ active: false });
  await configureWiFi(options(), {
    commands,
    http: new MockHTTP(),
    tls: new MockTLS(),
  });
  assert.deepEqual(commands.calls.at(-1), [
    "connection",
    "down",
    "id",
    "ap-profile",
  ]);
});

test("workflow makes no network request when AP activation fails", async () => {
  const commands = new MockCommands({ failUp: true });
  const http = new MockHTTP();
  const tls = new MockTLS();
  await assert.rejects(
    configureWiFi(options(), { commands, http, tls }),
    /activation failure/,
  );
  assert.equal(http.urls.length, 0);
  assert.equal(tls.calls.length, 0);
  assert.deepEqual(commands.calls.at(-1), [
    "connection",
    "up",
    "uuid",
    "12345678-1234-1234-1234-123456789abc",
  ]);
});

function fakeChild(closeSignal) {
  const child = new EventEmitter();
  child.stdout = new EventEmitter();
  child.signals = [];
  child.kill = (signal) => {
    child.signals.push(signal);
    if (signal === closeSignal) {
      setImmediate(() => child.emit("close", null, signal));
    }
    return true;
  };
  return child;
}

test("nmcli timeout waits for SIGTERM to SIGKILL escalation", async () => {
  const child = fakeChild("SIGKILL");
  let closed = false;
  child.once("close", () => {
    closed = true;
  });
  const commands = new NetworkManagerCommands("nmcli", 5, {
    spawnProcess: () => child,
    terminationGraceMilliseconds: 5,
    killWaitMilliseconds: 50,
  });

  await assert.rejects(commands.run(["connection", "show"]), /timed out/);
  assert(closed, "command rejected before the child exited");
  assert.deepEqual(child.signals, ["SIGTERM", "SIGKILL"]);
});

test("oversized nmcli output waits for child exit", async () => {
  const child = fakeChild("SIGTERM");
  let closed = false;
  child.once("close", () => {
    closed = true;
  });
  const commands = new NetworkManagerCommands("nmcli", 1000, {
    spawnProcess: () => child,
    terminationGraceMilliseconds: 100,
    killWaitMilliseconds: 50,
  });

  const result = commands.run(["connection", "show"]);
  child.stdout.emit("data", Buffer.alloc(70 * 1024));
  await assert.rejects(result, /too large/);
  assert(closed, "oversize rejection occurred before child exit");
  assert.deepEqual(child.signals, ["SIGTERM"]);
});

test("descriptor fetch has an independent wall-clock abort", async () => {
  const request = new EventEmitter();
  request.destroyed = false;
  request.destroy = () => {
    request.destroyed = true;
    setImmediate(() => request.emit("error", new Error("destroyed")));
  };
  const client = new DescriptorHTTPClient(5, {
    get: () => request,
  });

  await assert.rejects(
    client.fetch("http://bootstrap.invalid/provisioning.json"),
    /timed out/,
  );
  assert(request.destroyed);
});

test("SIGTERM abort restores the prior profile before main returns", async () => {
  const runtime = new EventEmitter();
  runtime.stdout = { content: "", write(value) { this.content += value; } };
  runtime.stderr = { content: "", write(value) { this.content += value; } };
  let descriptorStarted;
  const started = new Promise((resolve) => {
    descriptorStarted = resolve;
  });
  const blockingHTTP = {
    fetch(_url, signal) {
      descriptorStarted();
      return new Promise((resolve, reject) => {
        if (signal.aborted) {
          reject(new Error("aborted"));
          return;
        }
        signal.addEventListener(
          "abort",
          () => reject(new Error("aborted")),
          { once: true },
        );
      });
    },
  };
  const commands = new MockCommands();
  const result = main([
    "--ap-profile",
    "ap-profile",
    "--target-profile",
    "target-profile",
    "--descriptor-url",
    "http://bootstrap.invalid/provisioning.json",
  ], {
    runtime,
    commands,
    http: blockingHTTP,
    tls: new MockTLS(),
  });

  await started;
  assert.equal(runtime.listenerCount("SIGINT"), 1);
  assert.equal(runtime.listenerCount("SIGTERM"), 1);
  runtime.emit("SIGTERM");
  assert.equal(await result, 143);
  assert.deepEqual(commands.calls.at(-1), [
    "connection",
    "up",
    "uuid",
    "12345678-1234-1234-1234-123456789abc",
  ]);
  assert.equal(runtime.listenerCount("SIGINT"), 0);
  assert.equal(runtime.listenerCount("SIGTERM"), 0);
  assert.equal(runtime.stdout.content, "");
});
