'use strict';
const fs = require('node:fs');
const path = require('node:path');
for (const target of ['linux-x64', 'linux-arm64', 'darwin-x64', 'darwin-arm64']) {
 const binary = path.join(__dirname, '..', 'dist', target, 'garcon');
 try {
  const stat = fs.statSync(binary);
  if (!stat.isFile() || stat.size < 1024 || !(stat.mode & 0o111)) throw new Error('invalid executable');
 } catch {
  console.error(`Missing or invalid ${target} binary. Build all targets with scripts/release.sh VERSION --dry-run before packing or publishing.`);
  process.exit(1);
 }
}
