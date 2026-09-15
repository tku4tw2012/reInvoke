// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const { run, foreground, verifyFile, selectTransport, wrapRemote, parseRemote, verifyJournal, pins, ack, banner } = require('./trial-runner');
const report = console.log;
console.log = () => {};

// All ADB operations are in-process mocks. Fixtures stay under this project.
const root = path.join(__dirname, `.trial-runner-test-${process.pid}-${crypto.randomBytes(6).toString('hex')}`);
fs.mkdirSync(root, { mode: 0o700 });
const plan = { writeOrder: [331, 1], targetMainSHA256: 'a'.repeat(64), targetOobSHA256: 'b'.repeat(64) };
const device = 'List of devices attached\n0123456789ABCDEF device usb:3-1.2 product:reInvoke transport_id:7\n';
let cases = 0;

function fixture(mode = 'success') {
  const directory = path.join(root, `case-${++cases}`);
  fs.mkdirSync(directory);
  const evidence = path.join(directory, 'evidence');
  const calls = [];
  let discoveryCount = 0;
  const deps = {
    inspect: () => ({ plan, bytes: 4096 }),
    execute(file, args) {
      assert.strictEqual(file, 'adb');
      assert.deepStrictEqual(args.slice(0, 4), ['-H', '127.0.0.1', '-P', '5037']);
      calls.push(args.join(' '));
      if (args.includes('devices')) {
        discoveryCount++;
        return { status: 0, stdout: mode === 'wrong-target' ? device.replace('3-1.2', '3-1.3') :
          mode === 'changed-target' && discoveryCount === 2 ? device.replace('transport_id:7', 'transport_id:8') : device };
      }
      assert.deepStrictEqual(args.slice(4, 6), ['-t', '7'], 'never select by generic serial');
      if (args.includes('push')) return { status: mode === 'push-failed' ? 1 : 0, stdout: '' };
      assert(args.includes('shell') && !args.includes('-T'), 'legacy adbd requires a PTY');
      const command = args[args.length - 1];
      const token = command.match(/REINVOKE_EXIT_([a-f0-9]{32})=/)[1];
      let output = '';
      if (command.includes('/proc/version')) {
        output = ['0', 'armv7l', mode === 'wrong-kernel' ? 'old kernel' : banner,
          mode === 'wrong-node' ? '5a:2:600' : '5a:3:400',
          mode === 'not-tmpfs' ? 'ef53' : '1021994',
          mode === 'low-space' ? '1' : '200000', mode === 'low-ram' ? '1' : '200000'].join('\n');
      }
      if (command.includes('busybox rm')) {
        assert(!/rm -|\/dev\/mtd|\/dev\/reinvoke-nand-ro|[*]/.test(command), 'no broad cleanup or device-node deletion');
        assert.strictEqual((command.match(/\/dev\/reinvoke-nand-trial-[a-f0-9]{32}\//g) || []).length, 3);
        for (const name of Object.keys(pins).slice(0, 3)) assert(command.includes(name));
        assert(!command.includes('/evidence'), 'preserve engine evidence');
      }
      const status = mode === 'remote-hash-failed' && command.includes('sha256sum') ? 1 : 0;
      return { status: 0, stdout: `/\r\nREINVOKE_BEGIN_${token}\r\n${output}\r\nREINVOKE_EXIT_${token}=${status}\r\n` };
    },
    async foreground(file, args, stdout, stderr) {
      assert.strictEqual(file, 'adb');
      assert(!args.includes('-T'), 'legacy adbd rejects non-PTY shell');
      calls.push(args.join(' '));
      const command = args[args.length - 1];
      assert(command.includes(' apply ') && command.includes(ack));
      assert(!command.includes('preflight') && !command.includes('verify-target'));
      const token = command.match(/REINVOKE_EXIT_([a-f0-9]{32})=/)[1];
      const intent = path.join(directory, `.trial-3-1.2-${pins['UPDATE-PLAN.json']}.intent.json`);
      assert(fs.existsSync(intent), 'durable intent must precede apply');
      const events = [
        { stage: 'phase-start', detail: 'apply' },
        ...plan.writeOrder.map(block => ({ stage: 'after-block', block })),
        { stage: 'phase-complete', detail: `apply main=${plan.targetMainSHA256} OOB=${plan.targetOobSHA256}; unchanged blocks verified; cleanup pending; limits` },
        { stage: 'before-cleanup', detail: '<nil>' },
        { stage: 'cleanup-complete-no-reboot', detail: '<nil>' },
      ];
      if (mode === 'short-blocks') events.splice(1, 1);
      if (mode === 'wrong-result') events[3].detail = 'apply main=wrong';
      if (mode === 'cleanup-failed') events[5].stage = 'cleanup-failed';
      if (mode === 'early-complete') events.unshift(events.splice(3, 1)[0]);
      if (mode === 'partial-json') events.pop();
      let text = '/\r\n';
      if (mode !== 'missing-begin') text += `REINVOKE_BEGIN_${token}\r\n`;
      if (mode === 'duplicate-begin') text += `REINVOKE_BEGIN_${token}\r\n`;
      text += events.map(event => JSON.stringify(event)).join('\r\n') + '\r\n';
      if (mode === 'partial-json') text += '{"stage":';
      if (mode !== 'missing-exit') text += `\nREINVOKE_EXIT_${token}=${mode === 'failed' ? 13 : 0}\n`;
      if (mode === 'duplicate-exit') text += `REINVOKE_EXIT_${token}=0\n`;
      fs.writeFileSync(stdout, text);
      fs.writeFileSync(stderr, '');
      return { status: mode === 'disconnected' ? 1 : 0, signal: mode === 'signal' ? 'SIGTERM' : null, stdout: text };
    },
  };
  return { directory, evidence, calls, deps,
    args: [directory, 'apply', '--evidence', evidence, '--confirm', ack, '--approval-ref', 'mock-only-owner-reference'] };
}

async function main() {
  const transport = await foreground(process.execPath,
    ['-e', 'process.stdout.write("mock ADB stdout\\n"); process.stderr.write("mock ADB stderr\\n"); process.exitCode = 7;'],
    path.join(root, 'mock-adb.stdout'), path.join(root, 'mock-adb.stderr'));
  assert.strictEqual(transport.status, 7, 'record actual child exit, not log-parser status');
  assert.strictEqual(transport.stdout, 'mock ADB stdout\n');
  assert.strictEqual(transport.stderr, 'mock ADB stderr\n');
  const sample = path.join(root, 'sample');
  fs.writeFileSync(sample, 'synthetic input');
  const hash = crypto.createHash('sha256').update('synthetic input').digest('hex');
  assert.strictEqual(verifyFile(sample, hash), 15);
  assert.throws(() => verifyFile(sample, '0'.repeat(64)), /SHA-256 mismatch/);
  fs.symlinkSync(sample, path.join(root, 'link'));
  assert.throws(() => verifyFile(path.join(root, 'link'), hash), /non-symlink/);
  assert.strictEqual(selectTransport(device), '7');
  assert.throws(() => selectTransport(device + device), /exactly one/);
  assert.throws(() => selectTransport(device.replace(' device ', ' offline ')), /ready RAM/);
  assert.throws(() => selectTransport(device.replace('0123456789ABCDEF', 'other')), /ready RAM/);
  assert.throws(() => verifyJournal('{}\n', plan));
  const token = 'c'.repeat(32);
  const framed = `/\r\nREINVOKE_BEGIN_${token}\r\n0\r\nREINVOKE_EXIT_${token}=0\r\n`;
  assert.strictEqual(parseRemote(framed, token), '0', 'exclude legacy shell startup output');
  assert.throws(() => parseRemote(framed.replace(`REINVOKE_BEGIN_${token}\r\n`, ''), token), /remote begin/);
  assert.throws(() => parseRemote(framed.replace(`REINVOKE_EXIT_${token}=0`, `REINVOKE_EXIT_${token}=1`), token), /remote exit/);
  assert.throws(() => parseRemote(`${framed}unexpected trailing output\n`, token), /remote exit/);
  assert(wrapRemote('/bin/busybox id -u', token).includes('/bin/busybox printf'),
    'the outer RAM shell has no bare printf command');

  let test = fixture();
  await run([test.directory], test.deps);
  assert.deepStrictEqual(test.calls, [], 'default must not contact ADB');
  assert.deepStrictEqual(fs.readdirSync(test.directory), [], 'default must not stage or create intent');

  test = fixture();
  test.args[test.args.indexOf('--confirm') + 1] = 'WRONG';
  await assert.rejects(run(test.args, test.deps), /acknowledgment/);
  assert.deepStrictEqual(test.calls, []);
  test = fixture();
  await assert.rejects(run(test.args.slice(0, -2), test.deps), /owner approval/);
  assert.deepStrictEqual(test.calls, []);
  test = fixture();
  test.deps.inspect = () => verifyFile(sample, '0'.repeat(64));
  await assert.rejects(run(test.args, test.deps), /SHA-256 mismatch/);
  assert.deepStrictEqual(test.calls, []);

  for (const mode of ['wrong-target', 'changed-target', 'wrong-kernel', 'wrong-node', 'not-tmpfs',
    'low-space', 'low-ram', 'push-failed', 'remote-hash-failed', 'short-blocks', 'wrong-result',
    'cleanup-failed', 'early-complete', 'partial-json', 'missing-begin', 'duplicate-begin',
    'missing-exit', 'duplicate-exit', 'failed', 'disconnected', 'signal', 'success']) {
    test = fixture(mode);
    if (mode === 'success') await run(test.args, test.deps);
    else await assert.rejects(run(test.args, test.deps), undefined, mode);
    const applies = test.calls.filter(call => call.includes(' apply '));
    const reachedApply = ['short-blocks', 'wrong-result', 'cleanup-failed', 'early-complete', 'partial-json', 'missing-begin', 'duplicate-begin', 'missing-exit',
      'duplicate-exit', 'failed', 'disconnected', 'signal', 'success'].includes(mode);
    assert.strictEqual(applies.length, reachedApply ? 1 : 0, `${mode}: one apply, no retries`);
    assert(!test.calls.some(call => /preflight|verify-target|reboot|reset|poweroff|nohup|setsid|8141/.test(call)),
      `${mode}: no duplicate scan, boot/helper or background command`);
    assert.strictEqual(test.calls.filter(call => call.includes('busybox rm')).length, mode === 'success' ? 1 : 0);
    assert.strictEqual(fs.existsSync(path.join(test.evidence, 'result.json')), mode === 'success');
    if (mode === 'success') {
      const result = JSON.parse(fs.readFileSync(path.join(test.evidence, 'result.json'), 'utf8'));
      assert.strictEqual(result.independentReadback, false);
      assert.strictEqual(result.nativeBootAcceptance, false);
      assert(result.remoteEvidence.startsWith('/dev/reinvoke-nand-trial-'));
    }
    if (reachedApply) {
      const before = test.calls.length;
      test.args[3] = path.join(test.directory, 'another-evidence');
      await assert.rejects(run(test.args, test.deps), /prior intent exists/);
      assert.strictEqual(test.calls.length, before, 'new evidence path cannot bypass prior intent');
    }
    if (process.argv[2] && process.argv[3]) {
      verifyJournal(fs.readFileSync(process.argv[3], 'utf8').trim(), JSON.parse(fs.readFileSync(process.argv[2], 'utf8')));
    }
  }
  report(`PASS ${cases} mock-only trial cases: default, pins, approval/target/resources, single apply, ambiguous exits, no retry, owned cleanup`);
}

main().finally(() => fs.rmSync(root, { recursive: true })).catch(error => {
  console.error(error);
  process.exitCode = 1;
});
