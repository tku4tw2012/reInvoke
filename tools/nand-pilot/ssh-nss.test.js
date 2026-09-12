// Copyright (c) Microsoft Corporation.
// SPDX-License-Identifier: MIT
'use strict';
const assert = require('node:assert/strict');
const test = require('node:test');
const { localAccountNSS } = require('./private-config');

test('account NSS uses local files without changing network or other databases', () => {
  const input = '# synthetic NSS fixture\npasswd: compat\ngroup: compat\nshadow: compat\n' +
    '\nhosts: files dns wins\nnetworks: files\nservices: db files\n#netgroup: nis\n';
  const fixed = localAccountNSS(input);
  assert.equal(fixed, input.replace(/^(passwd|group|shadow): compat/gm, '$1: files'));
  assert.equal(localAccountNSS(fixed), fixed);
});

test('missing account databases are added, including an absent configuration', () => {
  assert.equal(localAccountNSS(''), 'passwd: files\ngroup: files\nshadow: files\n');
  assert.equal(localAccountNSS('hosts: files dns'), 'hosts: files dns\n' +
    'passwd: files\ngroup: files\nshadow: files\n');
  assert.equal(localAccountNSS('passwd: files\n'), 'passwd: files\ngroup: files\nshadow: files\n');
});

test('whitespace, duplicate account policies, and CRLF cannot retain compat', () => {
  const input = ' passwd : compat\r\npasswd: nis files\r\n\tgroup:\tcompat\r\n' +
    'shadow: compat\r\nhosts: files dns\r\n# passwd: compat\r\n';
  assert.equal(localAccountNSS(input),
    'passwd: files\r\npasswd: files\r\ngroup: files\r\nshadow: files\r\n' +
    'hosts: files dns\r\n# passwd: compat\r\n');
});
