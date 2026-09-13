// Locate the prebuilt binary for this platform, installed as an optional dependency.
'use strict';
const pkg = `ai-garcon-${process.platform}-${process.arch}`;
function binaryPath() {
	try {
		return require.resolve(`${pkg}/bin/garcon`);
	} catch {
		return null;
	}
}
module.exports = { pkg, binaryPath };
