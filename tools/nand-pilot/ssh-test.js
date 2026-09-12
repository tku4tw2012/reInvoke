// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
'use strict';
const fs = require('fs');
const path = require('path');
const cp = require('child_process');
const net = require('net');
const os = require('os');
const assert = require('assert');
const { readConfig, installConfig } = require('./private-config');
const archive = path.resolve(process.argv[2]);
const config = readConfig(process.argv[3]);
const operator = path.resolve(process.argv[4]);
const output = path.resolve(process.argv[5]);
assert(!fs.existsSync(output), 'new evidence directory required');
fs.mkdirSync(output, { mode: 0o700 });
const inputConfig = JSON.parse(fs.readFileSync(process.argv[3], 'utf8'));
const invalid = path.join(output, 'invalid-config.json');
for (const override of [{ sshCIDRs: ['0.0.0.0/0'] }, { sshBinarySHA256: '0'.repeat(64) },
  { authorizedKey: operator }]) {
  fs.writeFileSync(invalid, JSON.stringify({ ...inputConfig, ...override }), { mode: 0o600 });
  assert.throws(() => readConfig(invalid), /SSH|operator/);
}
fs.rmSync(invalid);
const installed = path.join(output, 'installed-fixture');
fs.mkdirSync(path.join(installed, 'opt/reinvoke/etc'), { recursive: true });
fs.writeFileSync(path.join(installed, 'opt/reinvoke/etc/bluez-main.conf'), '[General]\nName = old-test-name\n');
installConfig(config, installed);
assert.equal(fs.readFileSync(path.join(installed, 'root/.ssh/authorized_keys'), 'utf8'), config.publicKey);
assert.equal(fs.statSync(path.join(installed, 'etc/native-admin/host-key')).mode & 0o777, 0o600);
assert.equal(fs.statSync(path.join(installed, 'root/.ssh')).mode & 0o777, 0o700);
assert.match(fs.readFileSync(path.join(installed, 'etc/passwd'), 'utf8'), /^root:x:0:0:/);
assert.match(fs.readFileSync(path.join(installed, 'opt/reinvoke/etc/bluez-main.conf'), 'utf8'), /Name = reInvoke-NAND/);
fs.rmSync(installed, { recursive: true });
fs.writeFileSync(path.join(output, 'authorized_keys'), config.publicKey, { mode: 0o600 });
const qemu = path.join(archive, 'emulation/qemu-arm-static');
const run = (args) => cp.spawnSync('ssh', args, { encoding: 'utf8', timeout: 12000 });
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
(async () => {
  const probe = net.createServer();
  await new Promise(resolve => probe.listen(0, '127.0.0.1', resolve));
  const port = probe.address().port;
  await new Promise(resolve => probe.close(resolve));
  const version = cp.execFileSync(qemu, [config.sshBinary, 'dropbear', '-V'],
    { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] });
  const serverLog = fs.openSync(path.join(output, 'server-private.log'), 'wx', 0o600);
  const server = cp.spawn(qemu, [config.sshBinary, 'dropbear', '-F', '-E', '-j', '-k',
    '-p', `127.0.0.1:${port}`, '-r', config.sshHostKey,
    '-D', '.', '-P', path.join(output, 'server.pid')],
  { cwd: output, stdio: ['ignore', serverLog, serverLog] });
  fs.closeSync(serverLog);
  try {
    let ready = false;
    for (let n = 0; n < 30; n++) {
      ready = await new Promise(resolve => {
        const socket = net.connect({ host: '127.0.0.1', port });
        socket.once('connect', () => { socket.destroy(); resolve(true); });
        socket.once('error', () => resolve(false));
      });
      if (ready) break;
      await sleep(100);
    }
    assert(ready, 'loopback-only SSH listener failed');
    const hostPublic = cp.execFileSync(qemu, [config.sshBinary, 'dropbearkey', '-y',
      '-f', config.sshHostKey], { encoding: 'utf8' }).split('\n').find(s => s.startsWith('ssh-ed25519 '));
    assert(hostPublic);
    const known = path.join(output, 'known_hosts');
    fs.writeFileSync(known, `native03-loopback ${hostPublic}\n`, { mode: 0o600 });
    const args = ['-F', '/dev/null', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=5',
      '-o', 'IdentitiesOnly=yes', '-o', 'IdentityAgent=none', '-o', 'StrictHostKeyChecking=yes',
      '-o', `UserKnownHostsFile=${known}`, '-o', 'GlobalKnownHostsFile=/dev/null',
      '-o', 'HostKeyAlias=native03-loopback', '-p', String(port)];
    const destination = `${os.userInfo().username}@127.0.0.1`;
    const good = run([...args, '-i', operator, destination, '/bin/true']);
    if (good.status !== 0)
      fs.writeFileSync(path.join(output, 'client-private.log'), good.stderr || '', { mode: 0o600 });
    assert.equal(good.status, 0, 'authorized operator and pinned host must authenticate');
    const password = run([...args, '-o', 'PreferredAuthentications=password,keyboard-interactive',
      '-o', 'PubkeyAuthentication=no', destination, '/bin/true']);
    assert.equal(password.status, 255, 'password-only request must fail');
    assert.match(password.stderr, /Permission denied \(publickey\)/);
    const wrong = path.join(output, 'wrong-key');
    cp.execFileSync('ssh-keygen', ['-q', '-t', 'ed25519', '-N', '', '-C', 'negative-test', '-f', wrong]);
    const unauthorized = run([...args, '-i', wrong, destination, '/bin/true']);
    assert.equal(unauthorized.status, 255, 'unlisted operator key must fail');
    fs.writeFileSync(known, 'native03-loopback ' + fs.readFileSync(wrong + '.pub', 'utf8'), { mode: 0o600 });
    const changedHost = run([...args, '-i', operator, destination, '/bin/true']);
    assert.equal(changedHost.status, 255, 'wrong host pin must fail');
    assert.match(changedHost.stderr, /HOST IDENTIFICATION HAS CHANGED|Host key verification failed/);
    fs.writeFileSync(known, `native03-loopback ${hostPublic}\n`, { mode: 0o600 });
    const report = { transport: 'host loopback only; static ARM server under QEMU',
      version: version.trim() || 'Dropbear 2026.94', authorizedKey: 'PASS',
      passwordRejected: 'PASS', unlistedKeyRejected: 'PASS', mismatchedHostPinRejected: 'PASS',
      privateConfigAndInstalledModes: 'PASS',
      nativeKernelAndPTY: 'NOT TESTED' };
    fs.writeFileSync(path.join(output, 'RESULT.json'), JSON.stringify(report, null, 2) + '\n');
    console.log(JSON.stringify(report));
  } finally {
    server.kill('SIGTERM');
    await new Promise(resolve => server.once('close', resolve));
    for (const name of ['wrong-key', 'wrong-key.pub', 'server.pid'])
      fs.rmSync(path.join(output, name), { force: true });
  }
})().catch(error => { console.error(error.message); process.exitCode = 1; });
