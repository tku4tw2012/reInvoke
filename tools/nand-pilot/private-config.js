// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
'use strict';
const fs = require('fs');
const path = require('path');
const net = require('net');
const { run, hashFile } = require('./build-lib');

function readConfig(file) {
  if (!file) throw new Error('PILOT_PRIVATE_CONFIG is required for this unit-only build');
  const repo = path.resolve(__dirname, '../..');
  const outside = name => {
    const resolved = fs.realpathSync(name);
    if (resolved === repo || resolved.startsWith(repo + path.sep))
      throw new Error('private build inputs must be outside Git');
    return resolved;
  };
  outside(file);
  const config = JSON.parse(fs.readFileSync(file, 'utf8'));
  for (const key of ['authorizedKey', 'sshHostKey', 'sshBinary', 'sshLicense']) {
    if (typeof config[key] !== 'string') throw new Error(`missing private input ${key}`);
    config[key] = outside(config[key]);
  }
  if (fs.statSync(config.sshHostKey).mode & 0o077) throw new Error('host key permissions must be private');
  if (!/^[a-f0-9]{64}$/.test(config.sshBinarySHA256 || '') ||
      hashFile(config.sshBinary) !== config.sshBinarySHA256)
    throw new Error('private SSH binary hash pin missing or mismatched');
  const publicKey = fs.readFileSync(config.authorizedKey, 'utf8').trim();
  if (!/^ssh-ed25519 [A-Za-z0-9+/]+={0,2}(?: [^\r\n]*)?$/.test(publicKey))
    throw new Error('require one plain ED25519 operator public key');
  run('ssh-keygen', ['-lf', config.authorizedKey]);
  config.publicKey = publicKey.split(' ').slice(0, 2).join(' ') + '\n';
  if (!Array.isArray(config.sshCIDRs) || !config.sshCIDRs.length || config.sshCIDRs.length > 8)
    throw new Error('require a small explicit IPv4 SSH peer allowlist');
  for (const cidr of config.sshCIDRs) {
    const [ip, bits, extra] = String(cidr).split('/');
    if (extra !== undefined || net.isIP(ip) !== 4 || !/^\d+$/.test(bits) ||
      +bits < 24 || +bits > 32 || ip === '0.0.0.0' || ip.startsWith('127.') || +ip.split('.')[0] >= 224)
      throw new Error('SSH policy requires restricted IPv4 /24 through /32 peers');
  }
  if (config.wifiMAC !== undefined && !/^[0-9a-f]{2}(:[0-9a-f]{2}){5}$/i.test(config.wifiMAC))
    throw new Error('invalid private Wi-Fi MAC');
  for (const key of ['runtimeConfig', 'apSSID', 'apPSK']) {
    if (config[key] !== undefined) config[key] = outside(config[key]);
  }
  return config;
}

function installConfig(config, root) {
  const write = (name, data, mode = 0o600) => {
    const to = path.join(root, name);
    fs.mkdirSync(path.dirname(to), { recursive: true });
    fs.writeFileSync(to, data, { mode });
    fs.chmodSync(to, mode);
  };
  write('usr/sbin/dropbear', fs.readFileSync(config.sshBinary), 0o755);
  write('usr/share/licenses/dropbear/LICENSE', fs.readFileSync(config.sshLicense), 0o644);
  for (const name of ['libtomcrypt-LICENSE', 'libtommath-LICENSE', 'libc-LICENSE',
    'libc-copyright', 'libgcc-GPL', 'libgcc-copyright']) {
    write('usr/share/licenses/dropbear/' + name, fs.readFileSync(path.join(path.dirname(config.sshLicense), name)), 0o644);
  }
  write('etc/native-admin/host-key', fs.readFileSync(config.sshHostKey));
  write('etc/native-admin/allow-cidrs', config.sshCIDRs.join('\n') + '\n');
  write('root/.ssh/authorized_keys', config.publicKey);
  fs.chmodSync(path.join(root, 'root'), 0o700);
  fs.chmodSync(path.join(root, 'root/.ssh'), 0o700);
  fs.chmodSync(path.join(root, 'etc/native-admin'), 0o700);
  // A nonlocked account entry is necessary for public keys; password auth is
  // unavailable in this server. Nothing adds a password or an anonymous login.
  const passwd = path.join(root, 'etc/passwd');
  const existing = fs.existsSync(passwd) ? fs.readFileSync(passwd, 'utf8') : '';
  write('etc/passwd', 'root:x:0:0:root:/root:/bin/sh\n' +
    existing.split('\n').filter(line => line && !line.startsWith('root:')).join('\n') + '\n', 0o644);
  if (!fs.existsSync(path.join(root, 'etc/group'))) write('etc/group', 'root:x:0:\n', 0o644);
  write('etc/shells', '/bin/sh\n/bin/ash\n', 0o644);
  if (config.wifiMAC) write('etc/native-admin/wifi-mac', config.wifiMAC + '\n');
  for (const [key, name] of [['runtimeConfig', 'runtime.conf'], ['apSSID', 'provision-ap-ssid'],
    ['apPSK', 'provision-ap-psk']]) {
    if (config[key]) write(`opt/reinvoke/etc/${name}`, fs.readFileSync(config[key]));
  }
  const bluez = path.join(root, 'opt/reinvoke/etc/bluez-main.conf');
  let text = fs.readFileSync(bluez, 'utf8');
  if (!/^Name\s*=/m.test(text)) throw new Error('Bluetooth name configuration missing');
  text = text.replace(/^Name\s*=.*$/m, 'Name = reInvoke-NAND');
  fs.writeFileSync(bluez, text);
}
module.exports = { readConfig, installConfig };
