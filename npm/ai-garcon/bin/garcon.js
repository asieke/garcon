#!/usr/bin/env node
'use strict';
const { spawnSync } = require('node:child_process');
const { target, binaryPath } = require('./resolve.js');
if (process.argv[2] === 'update') process.exit(require('./update.js').update(process.argv.slice(3)));
const bin = binaryPath();
if (!bin) {
	console.error(`ai-garcon: binary missing for ${target}. Supported: linux/darwin on x64/arm64. Reinstall with npm install -g ai-garcon@latest.`);
	console.error('Build from source instead: https://github.com/asieke/garcon');
	process.exit(1);
}
const r = spawnSync(bin, process.argv.slice(2), { stdio: 'inherit' });
if (r.error) {
	console.error(`ai-garcon: ${r.error.message}`);
	process.exit(1);
}
process.exit(r.status ?? 1);
