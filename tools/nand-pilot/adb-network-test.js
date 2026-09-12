// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
'use strict';
const fs = require('fs');
const path = require('path');
const cp = require('child_process');
const assert = require('assert');
const crypto = require('crypto');
const net = require('net');
const {
  propertyWorkspace, ADBD_SHA256, validateAdbNetwork, installAdbNetwork, ADB_NETWORK_STATES
} = require('./adb-network-config');
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const hash = file => crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');

function packet(command, arg0, arg1, data = Buffer.alloc(0)) {
  const header = Buffer.alloc(24);
  header.write(command, 'ascii');
  header.writeUInt32LE(arg0, 4);
  header.writeUInt32LE(arg1, 8);
  header.writeUInt32LE(data.length, 12);
  header.writeUInt32LE(data.reduce((sum, byte) => (sum + byte) >>> 0, 0), 16);
  header.writeUInt32LE((header.readUInt32LE(0) ^ 0xffffffff) >>> 0, 20);
  return Buffer.concat([header, data]);
}

function command(port, localAddress = '127.0.0.1', shellCommand, timeout = 3000) {
  return new Promise((resolve, reject) => {
    const socket = net.connect({ host: '127.0.0.1', port, localAddress });
    let incoming = Buffer.alloc(0), output = '', connected = false;
    socket.setTimeout(timeout, () => socket.destroy(new Error('ADB timeout')));
    socket.on('error', reject);
    socket.on('connect', () => socket.write(packet('CNXN', 0x01000000, 4096,
      Buffer.from('host::offline-qualification\0'))));
    socket.on('data', bytes => {
      incoming = Buffer.concat([incoming, bytes]);
      try {
        while (incoming.length >= 24) {
          const length = incoming.readUInt32LE(12);
          assert(length <= 4096);
          if (incoming.length < 24 + length) break;
          const header = incoming.subarray(0, 24), payload = incoming.subarray(24, 24 + length);
          incoming = incoming.subarray(24 + length);
          const tag = header.toString('ascii', 0, 4);
          assert.equal(header.readUInt32LE(20), (header.readUInt32LE(0) ^ 0xffffffff) >>> 0);
          assert.equal(header.readUInt32LE(16), payload.reduce((sum, byte) => sum + byte, 0));
          if (tag === 'CNXN') {
            connected = true;
            socket.write(packet('OPEN', 1, 0, Buffer.from('shell:' + (shellCommand ||
              'read marker < /guest-only; printf "GUEST:%s\\n" "$marker"; printf "ROOT:"; pwd') + '\0')));
          } else if (tag === 'WRTE') {
            output += payload.toString();
            socket.write(packet('OKAY', 1, header.readUInt32LE(4)));
          } else if (tag === 'CLSE') {
            socket.end();
            assert(connected);
            resolve(output);
          } else assert.equal(tag, 'OKAY', 'unexpected ADB command (including authentication challenge)');
        }
      } catch (error) { socket.destroy(); reject(error); }
    });
    socket.on('end', () => reject(new Error('ADB closed without CLSE')));
  });
}

async function until(check, label, count = 60) {
  for (let n = 0; n < count; n++) {
    if (check()) return;
    await sleep(100);
  }
  throw new Error(label);
}

async function namespaceMain() {
  // The ordinary Android-style property name is not an environment override.
  // A synthetic regular file makes the binary's one-shot USB access() branch
  // visible without exposing any host USB node or changing a gadget.
  fs.writeFileSync('/fixture/root/dev/android_adb', '');
  const envOnly = cp.spawn('/usr/sbin/chroot', ['/fixture/root', '/qemu', '/sbin/adbd-root'],
    { env: { 'service.adb.tcp.port': '5555' }, stdio: 'ignore' });
  await sleep(400);
  await assert.rejects(command(5555), /ECONNREFUSED/);
  envOnly.kill('SIGKILL');
  await new Promise(resolve => envOnly.once('close', resolve));
  const log = fs.openSync('/fixture/daemon.log', 'w', 0o600);
  const server = cp.spawn('/usr/sbin/chroot', ['/fixture/root', '/qemu', '/bin/busybox', 'sh', '-c',
    'exec 9</etc/native-adb/properties; export ANDROID_PROPERTY_WORKSPACE=9,32768; exec /qemu /sbin/adbd-root'],
  { stdio: ['ignore', log, log] });
  fs.closeSync(log);
  try {
    let result;
    for (let attempt = 0; attempt < 25; attempt++) {
      try { result = await command(5555); break; } catch (error) {
        if (attempt === 24) throw error;
        await sleep(100);
      }
    }
    assert.match(result, /GUEST:native04-isolated-ARM-shell/);
    assert.match(result, /ROOT:\//);
    fs.writeFileSync('/fixture/guest-command.txt', result);
    const listeners = fs.readFileSync('/proc/net/tcp', 'utf8').trim().split('\n').slice(1)
      .map(line => line.trim().split(/\s+/)).filter(row => row[3] === '0A').map(row => row[1]);
    assert(listeners.includes('00000000:15B3'));
    assert(listeners.every(endpoint => endpoint === '00000000:15B3' || endpoint.startsWith('0100007F:')),
      'retained daemon must not expose another non-loopback port');
    assert(!fs.readFileSync('/proc/net/tcp6', 'utf8').split('\n').some(line => /\s0A\s/.test(line)));
    fs.writeFileSync('/fixture/listeners.txt', listeners.join('\n') + '\n');
    console.log('retained adbd TCP CNXN/OPEN/WRTE/CLSE and ARM guest shell: PASS');
  } finally {
    server.kill('SIGTERM');
    await new Promise(resolve => { if (server.exitCode !== null) resolve(); else server.once('close', resolve); });
  }
  await firewallTests();
}

async function firewallTests() {
  const iptables = (...args) => cp.execFileSync('/usr/sbin/iptables', args, { encoding: 'utf8' });
  cp.execFileSync('/usr/bin/ip', ['address', 'add', '10.231.254.1/32', 'dev', 'lo']);
  const state = '/fixture/state', config = '/fixture/root/etc/native-adb';
  const base = 'BB="/fixture/root/qemu /fixture/root/bin/busybox"\n' +
    'PILOT_STATE=/fixture/state\nPILOT_ADB_NETWORK_CONFIG=/fixture/root/etc/native-adb\n' +
    'PILOT_RUNTIME_SHUTDOWN=/fixture/runtime-shutdown\n' +
    '. /code/common.sh\n. /code/adb-network-start.sh\n' +
    'pilot_adb_network_iptables() { /usr/sbin/iptables "$@" >>/fixture/firewall.log 2>&1; }\n' +
    'pilot_adb_network_run() { exec 9<"${adb_config}/properties"; export ANDROID_PROPERTY_WORKSPACE=9,32768; ' +
    'exec /usr/sbin/chroot /fixture/root /qemu /sbin/adbd-root; }\n';
  const result = {};
  const shell = text => cp.spawn('/fixture/root/qemu', ['/fixture/root/bin/busybox', 'sh', '-c', text],
    { stdio: 'ignore' });
  const clean = () => {
    iptables('-F', 'INPUT');
    if (cp.spawnSync('/usr/sbin/iptables', ['-S', 'NATIVE_ADB'], { stdio: 'ignore' }).status === 0) {
      iptables('-F', 'NATIVE_ADB'); iptables('-X', 'NATIVE_ADB');
    }
    fs.rmSync(state, { force: true, recursive: true });
    fs.mkdirSync(state, { mode: 0o700 });
    fs.rmSync('/fixture/runtime-shutdown', { force: true });
    fs.writeFileSync(path.join(config, 'allow-peers'), '10.231.254.1/32\n', { mode: 0o600 });
    fs.writeFileSync(path.join(config, 'window-seconds'), '8\n', { mode: 0o600 });
  };
  const exited = child => new Promise(resolve => {
    if (child.exitCode !== null) resolve(child.exitCode); else child.once('close', resolve);
  });
  const launch = suffix => shell(base + (suffix || '') +
    '\npilot_adb_network_start || exit 1\n' +
    '[ ! -f "$PILOT_STATE/adbd-supervisor.pid" ] || wait "$(cat "$PILOT_STATE/adbd-supervisor.pid")"\n');
  const read = name => {
    try { return fs.readFileSync(path.join(state, name), 'utf8').trim(); } catch { return ''; }
  };
  clean();
  fs.writeFileSync(path.join(config, 'enabled'), '0\n');
  assert.equal(await exited(launch()), 0, 'disabled diagnostics must be a no-op');
  assert.equal(read('adb-network-state'), 'disabled');
  assert(!fs.existsSync(path.join(state, 'adb-network-once')), 'disabled diagnostics must not consume this boot window');
  assert.equal(iptables('-S', 'INPUT'), '-P INPUT ACCEPT\n');
  await assert.rejects(command(5555), /ECONNREFUSED/);
  fs.writeFileSync(path.join(config, 'enabled'), '1\n');
  result.disabledByDefault = 'PASS';
  fs.mkdirSync('/fixture/usb-case');
  const usbResult = cp.spawnSync('/fixture/root/qemu', ['/fixture/root/bin/busybox', 'sh', '-c',
    base + 'PILOT_STATE=/fixture/usb-case\nPILOT_ADBD_GADGET=/fixture/usb-case\n' +
    'PILOT_ADBD_DEV_FILE=data\nPILOT_ADBD_NODE=data-node\n' +
    'PILOT_ADBD_ENABLE_DEV_FILE=/fixture/usb-case/control\nPILOT_ADBD_ENABLE_NODE=control-node\n' +
    'configure_adb() { return 0; }\n' +
    'pilot_node_from_sysfs() { echo "$2" >>/fixture/usb-case/calls; [ "${bad:-0}" = 0 ]; }\n' +
    'pilot_usb_prerequisites || exit 1\n' +
    'echo "10:71" >/fixture/usb-case/control\npilot_usb_prerequisites || exit 2\n' +
    'pilot_node_from_sysfs() { [ "$2" != control-node ]; }\n' +
    'pilot_usb_prerequisites && exit 3\n' +
    '[ "$(cat /fixture/usb-case/adb-transport)" = enable-node-invalid ]\n'],
  { encoding: 'utf8' });
  assert.equal(usbResult.status, 0, usbResult.stderr);
  assert.equal(fs.readFileSync('/fixture/usb-case/calls', 'utf8'), 'data-node\ndata-node\ncontrol-node\n');
  result.optionalUSBLateControlNode = 'PASS (mock sysfs/node operations, no device access)';
  clean();
  const preserved = launch('pilot_adb_network_usb_ready() { return 0; }\n');
  assert.equal(await exited(preserved), 0);
  assert.equal(read('adb-network-state'), 'usb-preserved');
  assert(!fs.existsSync(path.join(state, 'stop-adb')), 'configured USB must not be stopped');
  assert(!fs.existsSync(path.join(state, 'adb-network-once')), 'USB retention must not select TCP ownership');
  assert.equal(iptables('-S', 'INPUT'), '-P INPUT ACCEPT\n');
  result.configuredUSBPreserved = 'PASS (policy fixture; no native USB claim)';
  const usbHealth = cp.spawnSync('/fixture/root/qemu', ['/fixture/root/bin/busybox', 'sh', '-c',
    base + 'PILOT_ADBD_GADGET=/fixture/usb-case\n' +
    'echo 42 >"$PILOT_STATE/adbd.pid"\n' +
    'pilot_adbd_usb_open() { [ "$1" = 42 ] && [ "${fd_open:-0}" = 1 ]; }\n' +
    'echo CONNECTED >/fixture/usb-case/state\nfd_open=1\n' +
    'pilot_adb_network_usb_ready && exit 1\n' +
    'echo CONFIGURED >/fixture/usb-case/state\nfd_open=0\n' +
    'pilot_adb_network_usb_ready && exit 2\n' +
    'fd_open=1\npilot_adb_network_usb_ready\n'], { encoding: 'utf8' });
  assert.equal(usbHealth.status, 0, usbHealth.stderr);
  result.usbRetentionRequiresConfiguredAndOpen = 'PASS';
  for (const test of ['no-allowlist', 'multiple-peers', 'wrong-prefix', 'public-peer', 'bad-window', 'bad-property',
    'firewall-error', 'partial-firewall-error', 'clock-error',
    'target-firewall-error', 'expiry', 'shutdown', 'runtime-shutdown', 'signal']) {
    clean();
    let suffix = '';
    if (test === 'no-allowlist') fs.writeFileSync(path.join(config, 'allow-peers'), '');
    if (test === 'multiple-peers')
      fs.writeFileSync(path.join(config, 'allow-peers'), '10.1.2.3/32\n10.1.2.4/32\n');
    if (test === 'wrong-prefix') fs.writeFileSync(path.join(config, 'allow-peers'), '10.231.254.0/24\n');
    if (test === 'public-peer') fs.writeFileSync(path.join(config, 'allow-peers'), '192.0.2.1/32\n');
    if (test === 'bad-window') fs.writeFileSync(path.join(config, 'window-seconds'), '301\n');
    if (test === 'bad-property') fs.writeFileSync(path.join(config, 'properties'), Buffer.alloc(32768));
    if (test === 'firewall-error') suffix = 'pilot_adb_network_iptables() { return 1; }\n';
    if (test === 'clock-error') suffix = 'pilot_adb_network_uptime() { return 1; }\n';
    if (test === 'partial-firewall-error') suffix = 'pilot_adb_network_iptables() { ' +
      '[ "$1:$2:$4" != "-I:NATIVE_ADB:-s" ] || return 1; /usr/sbin/iptables "$@"; }\n';
    if (test === 'target-firewall-error') suffix = 'pilot_adb_network_iptables() { ' +
      'XTABLES_LIBDIR=/opt/reinvoke/lib/xtables /fixture/root/qemu ' +
      '/opt/reinvoke/lib/ld-linux-armhf.so.3 --library-path /opt/reinvoke/lib ' +
      '/opt/reinvoke/bin/iptables "$@" >>/fixture/target-firewall.log 2>&1; }\n';
    if (['shutdown', 'runtime-shutdown', 'signal'].includes(test))
      fs.writeFileSync(path.join(config, 'window-seconds'), '30\n');
    const child = launch(suffix);
    try {
      if (!['expiry', 'shutdown', 'runtime-shutdown', 'signal'].includes(test)) {
        assert.notEqual(await exited(child), 0, test);
        assert(read('failure-adb-network'), 'failure must be observable');
        assert.equal(read('adb-network-state'), 'failed', 'status must distinguish failure from clean closure');
        await assert.rejects(command(5555), /ECONNREFUSED|timeout/);
      } else {
        await until(() => read('adb-network-state') === 'listening', 'guarded listener did not start');
        const rules = iptables('-S', 'INPUT') + iptables('-S', 'NATIVE_ADB');
        assert.match(rules, /-A INPUT -p (?:tcp|6) -m tcp --dport 5555 -j NATIVE_ADB/);
        assert.match(rules, /-s 10\.231\.254\.1\/32 -j ACCEPT/);
        assert.match(await command(5555, '10.231.254.1'), /GUEST:native04-isolated-ARM-shell/);
        await assert.rejects(command(5555, '127.0.0.2'), /timeout/);
        const duplicate = shell(base + '\npilot_adb_network_start\n');
        assert.notEqual(await exited(duplicate), 0, 'second supervisor must be rejected');
        const interrupted = command(5555, '10.231.254.1',
          'printf "STARTED\\n"; /qemu /bin/busybox sleep 30', 20000);
        const interruptedCheck = assert.rejects(interrupted, /closed without CLSE|ECONNRESET/);
        await sleep(200);
        if (test === 'shutdown') fs.writeFileSync(path.join(state, 'stop-adb-network'), '');
        if (test === 'runtime-shutdown') fs.writeFileSync('/fixture/runtime-shutdown', '');
        if (test === 'signal') process.kill(Number(read('adbd-supervisor.pid')), 'SIGTERM');
        await until(() => read('adb-network-state') === 'closed', 'bounded close did not finish', 120);
        await interruptedCheck;
        assert.equal(await exited(child), 0);
        if (test !== 'signal')
          assert.equal(read('adb-network-result'), test === 'expiry' ? 'expired' : 'shutdown');
        const closed = iptables('-S', 'NATIVE_ADB');
        assert(!closed.includes('ACCEPT'), 'closing must remove peer accepts');
        assert.match(closed, /-A NATIVE_ADB -j DROP/);
        assert(!fs.existsSync(path.join(state, 'adbd.pid')));
        assert(!fs.existsSync(path.join(state, 'adb-owner')));
        await assert.rejects(command(5555, '10.231.254.1'), /timeout/);
      }
      result[test] = 'PASS';
      assert(ADB_NETWORK_STATES.includes(read('adb-network-state')));
    } finally {
      child.kill('SIGKILL');
      await exited(child);
      if (test === 'bad-property') fs.writeFileSync(path.join(config, 'properties'), propertyWorkspace());
    }
  }
  result.isolation = 'rootless user/PID/mount/network namespaces; loopback interface only; no host binfmt';
  result.transport = 'retained ARM adbd and ARM BusyBox shell, real CNXN/OPEN/WRTE/CLSE';
  result.authentication = 'no AUTH challenge; unencrypted root ADB, weaker than pinned-key SSH';
  result.statusFile = '/run/nand-pilot/adb-network-state';
  result.statusTokens = ADB_NETWORK_STATES;
  result.perBootWindow = 'RAM-only one-window guard; no persistence helper or mount in successful transport fixtures';
  result.daemonSHA256 = ADBD_SHA256;
  result.propertyWorkspace = {
    environment: 'ANDROID_PROPERTY_WORKSPACE=9,32768 (read-only FD)',
    property: 'service.adb.tcp.port=5555',
    sha256: crypto.createHash('sha256').update(propertyWorkspace()).digest('hex'),
    readerAddress: '0x1582c (Thumb), legacy header and TOC verified from exact pinned binary',
    selection: 'TCP override wins even when synthetic USB path exists; raw property-name environment variable does not'
  };
  result.firewall = 'real private-namespace host nft iptables rules using production helper argv';
  result.nativeFirewall = 'held ARM legacy iptables rejects rootless user namespace; fail-closed verified; native not tested';
  result.nativeUSB = 'NOT TESTED';
  fs.writeFileSync('/fixture/RESULT.json', JSON.stringify(result, null, 2) + '\n');
  console.log(JSON.stringify(result));
}

function outerMain() {
  assert(process.argv.length === 4 || process.argv.length === 5,
    'usage: node adb-network-test.js ARCHIVE OUTPUT [EXTRACTED_RUNTIME]');
  const archive = path.resolve(process.argv[2]), output = path.resolve(process.argv[3]);
  assert(!fs.existsSync(output), 'new private qualification output required');
  fs.mkdirSync(output, { recursive: true, mode: 0o700 });
  assert.deepEqual(validateAdbNetwork(), { enabled: false });
  assert.deepEqual(validateAdbNetwork({ enabled: true, peers: ['10.231.254.1/32'] }),
    { enabled: true, peers: ['10.231.254.1/32'], windowSeconds: 300 });
  for (const value of [null, {}, { enabled: 'true' }, { enabled: true }, { enabled: true, peers: [] },
    { enabled: true, peers: ['0.0.0.0/0'] }, { enabled: true, peers: ['192.0.2.0/24'] },
    { enabled: true, peers: ['127.0.0.1/32'] }, { enabled: true, peers: ['224.0.0.1/32'] },
    { enabled: true, peers: ['192.0.2.1/32'] }, { enabled: true, peers: ['10.1.02.3/32'] },
    { enabled: true, peers: ['10.1.2.3/32', '10.1.2.3/32'] },
    { enabled: true, peers: ['10.1.2.3/32', '10.1.2.4/32'] },
    { enabled: true, peers: ['10.1.2.3/32'], port: 5555 },
    { enabled: true, peers: ['10.1.2.3/32'], windowSeconds: 0 },
    { enabled: true, peers: ['192.0.2.1/32'], windowSeconds: 301 },
    { enabled: false, peers: ['192.0.2.1/32'] }]) assert.throws(() => validateAdbNetwork(value));
  const root = path.join(output, 'root');
  for (const name of ['bin', 'sbin', 'lib', 'dev', 'proc', 'run', 'etc'])
    fs.mkdirSync(path.join(root, name), { recursive: true });
  const source = fs.realpathSync(process.argv[4] ||
    path.join(archive, 'build/artifacts/reinvoke-native-03-20260912/main/build-a/runtime'));
  const copy = (from, to) => fs.copyFileSync(from, path.join(root, to));
  assert.equal(hash(path.join(source, 'sbin/adbd-root')), ADBD_SHA256);
  assert.equal(hash(path.join(source, 'bin/busybox')), '5fc83ab6cd37841b8d73e07bf3cd8af47ae5af56c93fe085b2db91e0d1f4207b');
  for (const file of ['sbin/adbd-root', 'bin/busybox', 'lib/ld-linux.so.3', 'lib/libc.so.6',
    'lib/libdl.so.2', 'lib/librt.so.1', 'lib/libpthread.so.0', 'lib/libm.so.6']) copy(path.join(source, file), file);
  copy(path.join(archive, 'emulation/qemu-arm-static'), 'qemu');
  installAdbNetwork({ adbNetwork: { enabled: true, peers: ['10.231.254.1/32'] } }, root);
  assert(fs.readFileSync(path.join(root, 'etc/native-adb/properties')).equals(propertyWorkspace()));
  fs.writeFileSync(path.join(root, 'guest-only'), 'native04-isolated-ARM-shell\n');
  // QEMU user mode cannot exec another ARM ELF without binfmt. This native
  // exec-only shim crosses that boundary explicitly; all shell code is ARM.
  const shim = path.join(output, 'exec-arm-shell.c');
  fs.writeFileSync(shim, '#include <unistd.h>\n#include <stdlib.h>\nint main(int n,char **v){' +
    'char **a=calloc(n+4,sizeof(char*));a[0]="/qemu";a[1]="/bin/busybox";a[2]="sh";' +
    'for(int i=1;i<n;i++)a[i+2]=v[i];execv(a[0],a);return 111;}\n');
  cp.execFileSync('cc', ['-Os', '-static', '-o', path.join(root, 'bin/sh'), shim],
    { env: { ...process.env, TMPDIR: output } });
  const args = ['--unshare-all', '--die-with-parent', '--uid', '0', '--gid', '0',
    '--cap-add', 'CAP_SYS_CHROOT', '--cap-add', 'CAP_NET_ADMIN',
    '--ro-bind', '/usr', '/usr', '--ro-bind', '/lib', '/lib', '--ro-bind', '/lib64', '/lib64',
    '--ro-bind', '/etc/alternatives', '/etc/alternatives',
    '--ro-bind', path.join(root, 'sbin/adbd-root'), '/sbin/adbd-root',
    '--ro-bind', path.join(source, 'opt/reinvoke'), '/opt/reinvoke',
    '--proc', '/proc', '--dev', '/dev', '--bind', output, '/fixture',
    '--dev', '/fixture/root/dev', '--proc', '/fixture/root/proc',
    '--ro-bind', __dirname, '/code', process.execPath, '/code/adb-network-test.js', '--namespace'];
  const cpu = fs.readFileSync('/proc/self/status', 'utf8').match(/^Cpus_allowed_list:\s*(\d+)/m)[1];
  const result = cp.spawnSync('nice', ['-n', '10', 'taskset', '-c', cpu, 'bwrap', ...args],
    { encoding: 'utf8', timeout: 120000 });
  fs.writeFileSync(path.join(output, 'namespace.log'), result.stdout + result.stderr, { mode: 0o600 });
  assert.equal(result.status, 0, result.stdout + result.stderr);
  console.log(result.stdout.trim());
}

if (process.argv[2] === '--namespace') namespaceMain().catch(error => {
  console.error(error.stack); process.exitCode = 1;
}); else outerMain();
