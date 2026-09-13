#!/usr/bin/env node
'use strict';
const { spawnSync } = require('node:child_process');
const { target, binaryPath } = require('./resolve.js');
const bin = binaryPath();
if (!bin) {
	console.error(`ai-garcon: no prebuilt binary for ${target} (linux and macOS on x64 and arm64 are included).`);
	console.error('Build from source instead: https://github.com/asieke/garcon');
	process.exit(1);
}
const r = spawnSync(bin, process.argv.slice(2), { stdio: 'inherit' });
if (r.error) {
	console.error(`ai-garcon: ${r.error.message}`);
	process.exit(1);
}
process.exit(r.status ?? 1);
