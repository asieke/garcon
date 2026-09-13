'use strict';
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { spawnSync } = require('node:child_process');
const path = require('node:path');
const { globalPrefix } = require('../bin/update.js');

test('updates the owning npm prefix, including spaces; refuses local and npx installs', () => {
 assert.equal(globalPrefix('/home/me/node 22/lib/node_modules/ai-garcon'), '/home/me/node 22');
 assert.equal(globalPrefix('/usr/local/lib/node_modules/ai-garcon'), '/usr/local');
 assert.equal(globalPrefix('/lib/node_modules/ai-garcon'), '/');
 assert.equal(globalPrefix('/work/node_modules/ai-garcon'), null);
 assert.equal(globalPrefix('/home/me/.npm/_npx/123/node_modules/ai-garcon'), null);
});
test('update help and invalid arguments never invoke npm', () => {
 const shim = path.join(__dirname, '../bin/garcon.js');
 for (const [args, status] of [[['--help'], 0], [['typo'], 2]]) {
  const r = spawnSync(process.execPath, [shim, 'update', ...args], { encoding: 'utf8', env: { ...process.env, PATH: '' } });
  assert.equal(r.status, status);
  assert.match(r.stdout, /Usage: garcon update/);
 }
});

test('failed npm install never restarts the service', () => {
 const { update } = require('../bin/update.js');
 const calls = [];
 const result = update([], { packageDir: '/test/lib/node_modules/ai-garcon', spawn: (bin, args) => { calls.push([bin, args]); return { status: 7 }; } });
 assert.equal(result, 7);
 assert.deepEqual(calls, [['npm', ['install', '--global', '--prefix', '/test', 'ai-garcon@latest']]]);
});
test('successful update refreshes a service and waits for the new version', () => {
 const { update } = require('../bin/update.js');
 const calls = [];
 const result = update([], { packageDir: '/test/lib/node_modules/ai-garcon', home: '/test/home', platform: 'linux', exists: () => true,
  spawn: (bin, args) => { calls.push([bin, args]); return { status: 0 }; } });
 assert.equal(result, 0);
 assert.deepEqual(calls.slice(1).map(c => c[1].slice(1)), [['service', 'restart'], ['doctor', '--wait', '10s']]);
});
test('update without a service leaves startup to the user', () => {
 const { update } = require('../bin/update.js');
 const calls = [];
 const result = update([], { packageDir: '/test/lib/node_modules/ai-garcon', home: '/test/home', exists: p => p.endsWith('garcon.js'),
  spawn: (bin, args) => { calls.push([bin, args]); return { status: 0 }; } });
 assert.equal(result, 0);
 assert.equal(calls.length, 1);
});
