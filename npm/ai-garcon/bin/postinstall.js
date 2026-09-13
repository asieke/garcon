// After an install or upgrade, restart a running garcon service so it picks up the new
// binary. `garcon service restart` is a no-op when no service is installed, and any
// failure here must never fail the npm install itself.
'use strict';
const { spawnSync } = require('node:child_process');
const { binaryPath } = require('./resolve.js');
const bin = binaryPath();
if (bin) {
	try {
		spawnSync(bin, ['service', 'restart'], { stdio: 'inherit' });
	} catch {}
}
