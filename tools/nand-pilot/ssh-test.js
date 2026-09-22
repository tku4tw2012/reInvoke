// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT
'use strict';
const fs = require('fs');
const path = require('path');
const cp = require('child_process');
const net = require('net');
const assert = require('assert');
const { readConfig, installConfig, localAccountNSS } = require('./private-config');
const { hashFile, rooted, BLUETOOTH_NAME } = require('./build-lib');

// Usage: node ssh-test.js ARCHIVE EXTRACTED_TARGET_ROOT NEW_PRIVATE_OUTPUT
// Requires x86-64 Linux, gcc, rootless bwrap and a registered ARM interpreter for
// Dropbear's execve(/bin/sh). Never registers binfmt or substitutes a host shell.
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const exec = (command, args, options = {}) => cp.execFileSync(command, args, {
  encoding: 'utf8', timeout: 15000, stdio: ['ignore', 'pipe', 'pipe'], ...options,
});
const write = (file, data) => fs.writeFileSync(file, data, { mode: 0o600 });
let evidenceOutput;

async function stop(server, closed) {
  if (server.exitCode === null && server.signalCode === null) server.kill('SIGTERM');
  let timer;
  try {
    await Promise.race([closed, new Promise(resolve => {
      timer = setTimeout(() => { server.kill('SIGKILL'); resolve(); }, 3000);
    })]);
  } finally {
    clearTimeout(timer);
  }
  await closed;
}

async function main() {
  assert.equal(process.argv.length, 5,
    'usage: ssh-test.js ARCHIVE EXTRACTED_TARGET_ROOT NEW_PRIVATE_OUTPUT');
  assert(process.platform === 'linux' && process.arch === 'x64',
    'the namespace qualification fixture requires x86-64 Linux');
  const archive = fs.realpathSync(process.argv[2]);
  const source = fs.realpathSync(process.argv[3]);
  const output = path.resolve(process.argv[4]);
  const repo = fs.realpathSync(path.resolve(__dirname, '../..'));
  for (const dir of [source, fs.realpathSync(path.dirname(output))]) {
    assert(dir !== repo && !dir.startsWith(repo + path.sep),
      'target root and evidence must be outside source');
  }
  assert(!fs.existsSync(output), 'new evidence directory required');
  fs.mkdirSync(output, { mode: 0o700 });
  evidenceOutput = output;
  const root = path.join(output, 'target-root');
  const qemu = path.join(archive, 'emulation/qemu-arm-static');
  const inputNSS = fs.readFileSync(path.join(source, 'etc/nsswitch.conf'), 'utf8');
  const sourcePins = Object.fromEntries(['usr/sbin/dropbear', 'bin/busybox',
    'etc/passwd', 'etc/group', 'etc/nsswitch.conf', 'root/.ssh/authorized_keys',
    'etc/native-admin/host-key'].map(name => [name, hashFile(rooted(source, name))]));
  const serverELF = fs.readFileSync(rooted(source, 'usr/sbin/dropbear'));
  const shellELF = fs.readFileSync(rooted(source, 'bin/sh'));
  for (const elf of [serverELF, shellELF]) {
    assert.equal(elf.subarray(0, 4).toString('hex'), '7f454c46');
    assert.equal(elf.readUInt16LE(18), 40, 'packaged ARM executables required');
  }
  assert.equal(hashFile(rooted(source, 'bin/sh')), sourcePins['bin/busybox'],
    'target login shell must be the packaged ARM BusyBox');
  assert.match(fs.readFileSync(path.join(source, 'etc/passwd'), 'utf8'),
    /^root:x:0:0:root:\/root:\/bin\/sh$/m, 'packaged root account required');
  exec('cp', ['-a', source, root]);
  fs.writeFileSync(path.join(root, 'qualification-qemu'), '');
  fs.writeFileSync(path.join(root, 'qualification-fixture'), '');
  const fixture = path.join(output, 'namespace-fixture');
  exec('nice', ['-n', '10', 'gcc', '-static', '-Os', '-Wall', '-Wextra', '-Werror',
    path.join(__dirname, 'ssh-namespace-fixture.c'), '-o', fixture],
  { env: { ...process.env, TMPDIR: output } });
  const operator = path.join(output, 'operator');
  const wrong = path.join(output, 'wrong-key');
  const hostKey = path.join(output, 'host-key');
  const configFile = path.join(output, 'synthetic-config.json');
  let report = { transport: 'loopback; rootless namespace; extracted target /etc and ARM shell',
    sshBinarySHA256: sourcePins['usr/sbin/dropbear'],
    busyboxSHA256: sourcePins['bin/busybox'],
    originalNSSSHA256: sourcePins['etc/nsswitch.conf'],
    nativeKernelAndHardware: 'NOT TESTED',
    childExecution: 'existing host ARM binfmt; no host account, libc, or shell bind',
    namespaceLimitation: 'fixture permits only denied setgroups(1,[0]); native privileges not tested' };
  try {
    for (const key of [operator, wrong]) {
      exec('ssh-keygen', ['-q', '-t', 'ed25519', '-N', '', '-C',
        'synthetic-loopback', '-f', key]);
    }
    exec(qemu, ['-0', 'dropbearkey', path.join(source, 'usr/sbin/dropbear'),
      '-t', 'ed25519', '-f', hostKey]);
    const publicText = exec(qemu, ['-0', 'dropbearkey',
      path.join(source, 'usr/sbin/dropbear'), '-y', '-f', hostKey]);
    const hostPublic = publicText.split('\n').find(s => s.startsWith('ssh-ed25519 '));
    assert(hostPublic, 'synthetic host key generation failed');
    const inputConfig = { authorizedKey: operator + '.pub', sshHostKey: hostKey,
      sshBinary: path.join(source, 'usr/sbin/dropbear'),
      sshBinarySHA256: sourcePins['usr/sbin/dropbear'],
      sshLicense: path.join(source, 'usr/share/licenses/dropbear/LICENSE'),
      sshCIDRs: ['192.0.2.1/32'] };
    write(configFile, JSON.stringify(inputConfig));
    const config = readConfig(configFile);
    const invalid = path.join(output, 'invalid-config.json');
    try {
      for (const override of [{ sshCIDRs: ['0.0.0.0/0'] },
        { sshBinarySHA256: '0'.repeat(64) }, { authorizedKey: operator }]) {
        write(invalid, JSON.stringify({ ...inputConfig, ...override }));
        assert.throws(() => readConfig(invalid), /SSH|operator/);
      }
    } finally {
      fs.rmSync(invalid, { force: true });
    }
    installConfig(config, root);
    assert.equal(hashFile(path.join(root, 'usr/sbin/dropbear')), sourcePins['usr/sbin/dropbear']);
    assert.equal(hashFile(rooted(root, 'bin/sh')), sourcePins['bin/busybox']);
    assert.equal(fs.readFileSync(path.join(root, 'opt/reinvoke/etc/bluez-main.conf'), 'utf8')
      .split('\n').find(line => /^Name\s*=/.test(line)), `Name = ${BLUETOOTH_NAME}`);
    report.candidateBluetoothName = BLUETOOTH_NAME;
    for (const [name, mode] of [['etc/native-admin/host-key', 0o600],
      ['root/.ssh', 0o700], ['root/.ssh/authorized_keys', 0o600]]) {
      assert.equal(fs.statSync(path.join(root, name)).mode & 0o777, mode);
    }
    const known = path.join(output, 'known_hosts');
    const setPin = publicKey => write(known, `target-loopback ${publicKey.trim()}\n`);
    setPin(hostPublic);
    const namespace = ['--unshare-user', '--uid', '0', '--gid', '0',
      '--unshare-pid', '--unshare-ipc', '--unshare-uts', '--die-with-parent',
      '--ro-bind', root, '/', '--ro-bind', qemu, '/qualification-qemu',
      '--ro-bind', fixture, '/qualification-fixture',
      '--dev', '/dev', '--proc', '/proc', '--chdir', '/'];
    // This fails closed if ARM child exec is unavailable; /bin/sh is untouched.
    assert.equal(exec('bwrap', [...namespace, '--', '/bin/sh', '-c',
      'printf "packaged-arm-shell\\n"']), 'packaged-arm-shell\n');

    async function withServer(label, test) {
      const probe = net.createServer();
      probe.on('error', () => {});
      await new Promise((resolve, reject) => {
        probe.once('error', reject);
        probe.listen(0, '127.0.0.1', resolve);
      });
      const port = probe.address().port;
      await new Promise(resolve => probe.close(resolve));
      const log = fs.openSync(path.join(output, `${label}-server-private.log`), 'wx', 0o600);
      const server = cp.spawn('bwrap', [...namespace, '--', '/qualification-fixture',
        '/qualification-qemu',
        '-strace', '/usr/sbin/dropbear', '-F', '-E', '-j', '-k',
        '-p', `127.0.0.1:${port}`, '-r', '/etc/native-admin/host-key',
        '-P', '/dev/null', '-I', '15', '-M', '20'],
      { stdio: ['ignore', log, log] });
      fs.closeSync(log);
      let spawnError;
      server.once('error', error => { spawnError = error; });
      const closed = new Promise(resolve => server.once('close', resolve));
      try {
        let banner = '';
        for (let n = 0; n < 40; n++) {
          if (spawnError || server.exitCode !== null) break;
          banner = await new Promise(resolve => {
            const socket = net.connect({ host: '127.0.0.1', port });
            const finish = text => { socket.destroy(); resolve(text); };
            socket.setTimeout(500, () => finish(''));
            socket.once('data', data => finish(data.toString()));
            socket.once('error', () => finish(''));
          });
          if (banner.startsWith('SSH-2.0-dropbear')) break;
          await sleep(100);
        }
        assert.match(banner, /^SSH-2\.0-dropbear/, `${label}: no SSH banner`);
        const base = ['-F', '/dev/null', '-T', '-o', 'BatchMode=yes',
          '-o', 'ConnectTimeout=4', '-o', 'ConnectionAttempts=1',
          '-o', 'ServerAliveInterval=2', '-o', 'ServerAliveCountMax=2',
          '-o', 'IdentitiesOnly=yes', '-o', 'IdentityAgent=none',
          '-o', 'StrictHostKeyChecking=yes', '-o', `UserKnownHostsFile=${known}`,
          '-o', 'GlobalKnownHostsFile=/dev/null', '-o', 'HostKeyAlias=target-loopback',
          '-o', 'HostKeyAlgorithms=ssh-ed25519', '-p', String(port)];
        const client = (name, options, command = 'printf "ssh-target-shell\\n"; /bin/busybox id -u') => {
          const result = cp.spawnSync('ssh', [...base, ...options, 'root@127.0.0.1', command],
            { encoding: 'utf8', timeout: 12000, killSignal: 'SIGKILL' });
          write(path.join(output, `${label}-${name}-client-private.log`),
            result.stderr || '');
          assert(!result.error, `${label}/${name}: client timeout or execution error`);
          return result;
        };
        await test(client);
      } finally {
        await stop(server, closed);
        let stillListening = true;
        for (let attempt = 0; attempt < 20 && stillListening; attempt++) {
          stillListening = await new Promise(resolve => {
            const socket = net.connect({ host: '127.0.0.1', port });
            const finish = alive => { socket.destroy(); resolve(alive); };
            socket.setTimeout(500, () => finish(false));
            socket.once('connect', () => finish(true));
            socket.once('error', () => finish(false));
          });
          if (stillListening) await sleep(100);
        }
        assert(!stillListening, `${label}: listener must stop with its namespace PID`);
      }
    }

    // Native03 actually shipped compat. On later fixed roots, recreate only
    // that account lookup policy as the regression's negative baseline.
    const wasCompat = ['passwd', 'group', 'shadow'].every(database =>
      new RegExp(`^${database}:\\s+compat\\s*$`, 'm').test(inputNSS));
    const compatNSS = wasCompat ? inputNSS :
      inputNSS.replace(/^(passwd|group|shadow):[^\r\n]*/gm,
        (_, database) => `${database}: compat`);
    assert.match(compatNSS, /^passwd:\s+compat$/m);
    fs.writeFileSync(path.join(root, 'etc/nsswitch.conf'), compatNSS);
    await withServer('compat-baseline', client => {
      const result = client('authorized', ['-v', '-i', operator]);
      assert.equal(result.status, 255, 'compat account lookup must fail before correction');
      assert(!/Authenticated to /.test(result.stderr),
        'negative baseline must fail authentication, not merely command execution');
      assert.match(result.stderr, /SSH2_MSG_SERVICE_ACCEPT received/,
        'baseline must reach the user-authentication service');
      assert.equal(result.stdout, '', 'negative baseline must not execute a command');
      report.compatAccountBaseline = 'FAILS BEFORE AUTHENTICATION (expected)';
      report.baselinePolicy = compatNSS === inputNSS ? 'original target policy' :
        'native03 compat account policy restored on corrected target';
      write(path.join(output, 'PROGRESS.json'), JSON.stringify(report, null, 2) + '\n');
    });
    const baselineLog = fs.readFileSync(path.join(output, 'compat-baseline-server-private.log'), 'utf8');
    assert(baselineLog.includes('"/lib/libnss_compat.so.2"') &&
      baselineLog.includes('"/lib/libc.so.6"'), 'baseline must exercise packaged NSS ABI');
    report.accountLookupFailure = /_dl_call_libc_early_init: Assertion/.test(baselineLog) ?
      'vendor compat NSS loads old libc; _dl_call_libc_early_init assertion aborts' :
      'packaged compat NSS cannot authenticate target root';

    installConfig(config, root);
    const correctedNSS = fs.readFileSync(path.join(root, 'etc/nsswitch.conf'), 'utf8');
    assert.equal(correctedNSS, localAccountNSS(inputNSS));
    write(path.join(output, 'nsswitch-before.conf'), inputNSS);
    write(path.join(output, 'nsswitch-after.conf'), correctedNSS);
    report.correctedNSSSHA256 = hashFile(path.join(root, 'etc/nsswitch.conf'));
    await withServer('corrected', client => {
      const result = client('authorized', ['-v', '-i', operator]);
      assert.equal(result.status, 0, 'corrected target must authenticate and execute ARM shell');
      assert.match(result.stderr, /Authenticated to .* using "publickey"/);
      assert.equal(result.stdout, 'ssh-target-shell\n0\n',
        'actual packaged ARM shell and BusyBox command must execute as target root');
      report.authorizedKeyAndTargetCommand = 'PASS';
      const unauthorized = client('wrong-key', ['-i', wrong]);
      assert.equal(unauthorized.status, 255);
      assert.match(unauthorized.stderr, /Permission denied \(publickey\)/);
      report.unlistedKeyRejected = 'PASS';
      const password = client('password', ['-o',
        'PreferredAuthentications=password,keyboard-interactive',
      '-o', 'PubkeyAuthentication=no']);
      assert.equal(password.status, 255);
      assert.match(password.stderr, /Permission denied \(publickey\)/);
      report.passwordRejected = 'PASS (server offers publickey only)';
      setPin(fs.readFileSync(wrong + '.pub', 'utf8'));
      try {
        const changed = client('wrong-pin', ['-i', operator]);
        assert.equal(changed.status, 255);
        assert.match(changed.stderr, /HOST IDENTIFICATION HAS CHANGED|Host key verification failed/);
        report.mismatchedHostPinRejected = 'PASS';
      } finally {
        setPin(hostPublic);
      }
    });
    const correctedLog = fs.readFileSync(path.join(output, 'corrected-server-private.log'), 'utf8');
    assert(!correctedLog.includes('"/lib/libnss_compat.so.2"'),
      'corrected account lookup must not load vendor compat NSS');
    assert(!correctedLog.includes('"/lib/libc.so.6"'),
      'static corrected server must not load vendor libc for account lookup');
    assert.match(correctedLog, /fixture: execve\(\/bin\/sh\) observed/,
      'server must exec the packaged login shell, not a host test command');
    report.correctedAccountLookup = 'files; no vendor compat NSS or libc loaded';
    report.targetShellExecve = 'PASS';
    const passwd = path.join(root, 'etc/passwd');
    fs.writeFileSync(passwd, fs.readFileSync(passwd, 'utf8')
      .split('\n').filter(line => !line.startsWith('root:')).join('\n'));
    await withServer('missing-root', client => {
      const result = client('authorized', ['-v', '-i', operator]);
      assert.equal(result.status, 255, 'target root account must exist');
      assert(!/Authenticated to /.test(result.stderr));
      assert.equal(result.stdout, '');
      report.missingTargetAccountRejected = 'PASS';
    });
    for (const [name, hash] of Object.entries(sourcePins)) {
      assert.equal(hashFile(rooted(source, name)), hash, 'input target must remain unchanged');
    }
    report.privateConfigAndInstalledModes = 'PASS';
    report.sourceFilesUnchanged = 'PASS';
    report.loopbackListenersStopped = 'PASS';
    write(path.join(output, 'RESULT.json'), JSON.stringify(report, null, 2) + '\n');
    console.log(JSON.stringify(report));
  } finally {
    // Evidence logs are private; ephemeral test credentials and copied private
    // runtime data need not survive qualification.
    fs.rmSync(root, { recursive: true, force: true });
    for (const file of [operator, operator + '.pub', wrong, wrong + '.pub',
      hostKey, hostKey + '.pub', configFile, path.join(output, 'known_hosts')]) {
      fs.rmSync(file, { force: true });
    }
  }
}
main().catch(error => {
  if (evidenceOutput) {
    write(path.join(evidenceOutput, 'failure-private.log'), error.stack || String(error));
    fs.rmSync(path.join(evidenceOutput, 'target-root'), { recursive: true, force: true });
  }
  console.error('target-root SSH qualification failed; inspect the private evidence directory');
  process.exitCode = 1;
});
