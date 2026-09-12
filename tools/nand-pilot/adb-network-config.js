// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
'use strict';
const fs = require('fs');
const path = require('path');
const net = require('net');

const ADBD_SHA256 = '62593dfe9580443dca064e28c38cb647f1b719fe275666d5fb4b615781864ed6';
const PROPERTY_BYTES = 32768;
const ADB_NETWORK_STATES = Object.freeze(['disabled', 'usb-preserved', 'starting', 'listening', 'closed', 'failed']);

function validateAdbNetwork(value) {
  if (value === undefined) return { enabled: false };
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('adbNetwork must be an object');
  if (Object.keys(value).some(key => !['enabled', 'windowSeconds', 'peers'].includes(key)))
    throw new Error('unknown adbNetwork setting');
  if (value.enabled !== true && value.enabled !== false)
    throw new Error('adbNetwork.enabled must be boolean');
  if (!value.enabled) {
    if (Object.keys(value).length !== 1)
      throw new Error('disabled adbNetwork must not contain latent network settings');
    return { enabled: false };
  }
  const windowSeconds = value.windowSeconds === undefined ? 300 : value.windowSeconds;
  if (!Number.isInteger(windowSeconds) || windowSeconds < 1 || windowSeconds > 300)
    throw new Error('adbNetwork.windowSeconds must be 1 through 300');
  if (!Array.isArray(value.peers) || value.peers.length !== 1)
    throw new Error('adbNetwork requires exactly one explicit operator IPv4 /32 peer');
  for (const peer of value.peers) {
    if (typeof peer !== 'string' || !peer.endsWith('/32'))
      throw new Error('adbNetwork accepts IPv4 /32 peers only');
    const ip = peer.slice(0, -3);
    const octets = ip.split('.').map(Number);
    const privateIP = octets[0] === 10 || (octets[0] === 172 && octets[1] >= 16 && octets[1] <= 31) ||
      (octets[0] === 192 && octets[1] === 168);
    if (net.isIP(ip) !== 4 || ip.split('.').some(octet => String(+octet) !== octet) || !privateIP)
      throw new Error('adbNetwork peer must be a canonical private RFC1918 IPv4 /32, not Internet or loopback');
  }
  if (new Set(value.peers).size !== value.peers.length)
    throw new Error('duplicate adbNetwork peer');
  return { enabled: true, windowSeconds, peers: [...value.peers] };
}

function propertyWorkspace() {
  // Retained binary's legacy reader: header (count, serial, magic, version),
  // TOC at 32, name[32], serial at +32, value[92] at +36. One immutable
  // service property selects TCP explicitly; no property server or root flag.
  const blob = Buffer.alloc(PROPERTY_BYTES);
  const name = 'service.adb.tcp.port';
  const value = '5555';
  blob.writeUInt32LE(1, 0);
  blob.writeUInt32LE(0x504f5250, 8);
  blob.writeUInt32LE(0x45434f76, 12);
  blob.writeUInt32LE((name.length << 24) | 128, 32);
  blob.write(name, 128, 'ascii');
  blob.writeUInt32LE(value.length << 24, 160);
  blob.write(value, 164, 'ascii');
  return blob;
}

function installAdbNetwork(privateConfig, root) {
  const config = validateAdbNetwork(privateConfig.adbNetwork);
  const directory = path.join(root, 'etc/native-adb');
  if (fs.existsSync(directory)) throw new Error('native-adb configuration already exists');
  fs.mkdirSync(directory, { recursive: true, mode: 0o700 });
  fs.chmodSync(directory, 0o700);
  const write = (file, data) => fs.writeFileSync(path.join(directory, file), data, { mode: 0o600 });
  write('enabled', config.enabled ? '1\n' : '0\n');
  if (config.enabled) {
    write('window-seconds', `${config.windowSeconds}\n`);
    write('allow-peers', config.peers.join('\n') + '\n');
    write('properties', propertyWorkspace());
  }
  return config;
}

module.exports = {
  validateAdbNetwork, installAdbNetwork, propertyWorkspace, ADBD_SHA256, PROPERTY_BYTES, ADB_NETWORK_STATES
};
