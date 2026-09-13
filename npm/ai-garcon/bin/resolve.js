// The package ships one static binary per platform under dist/<os>-<arch>/garcon.
'use strict';
const path = require('node:path');
const fs = require('node:fs');
const target = `${process.platform}-${process.arch}`;
function binaryPath() {
	const p = path.join(__dirname, '..', 'dist', target, 'garcon');
	return fs.existsSync(p) ? p : null;
}
module.exports = { target, binaryPath };
