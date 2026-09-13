'use strict';
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const { spawnSync } = require('node:child_process');

// Only update the global installation that owns this shim, never an unrelated prefix.
function globalPrefix(packageDir) {
 const suffix = path.join('lib', 'node_modules', 'ai-garcon');
 return packageDir.endsWith(path.sep + suffix) ? packageDir.slice(0, -suffix.length - 1) || '/' : null;
}
function update(args, { packageDir = path.resolve(__dirname, '..'), spawn = spawnSync,
 exists = fs.existsSync, home = os.homedir(), platform = process.platform } = {}) {
 if (args.length) {
  console.log('Usage: garcon update\nUpdates this global npm installation, refreshes an installed service, and checks its version.');
  return args.length === 1 && ['--help', '-h'].includes(args[0]) ? 0 : 2;
 }
 const prefix = globalPrefix(packageDir);
 if (!prefix) {
  console.error('For npx: run npx ai-garcon@latest setup. For a local dependency: npm install ai-garcon@latest, then npx garcon setup.');
  return 1;
 }
 function run(bin, argv) {
  const r = spawn(bin, argv, { stdio: 'inherit' });
  if (r.error) { console.error(r.error.message); return 1; }
  return r.status ?? 1;
 }
 console.log(`Updating ai-garcon in ${prefix}…`);
 const status = run('npm', ['install', '--global', '--prefix', prefix, 'ai-garcon@latest']);
 if (status) {
  console.error('Update failed. For EACCES, use a user-owned npm prefix: https://docs.npmjs.com/resolving-eacces-permissions-errors-when-installing-packages-globally/');
  return status;
 }
 const shim = path.join(packageDir, 'bin/garcon.js');
 if (!exists(shim)) { console.error('Package shim missing after update; reinstall ai-garcon.'); return 1; }
 const unit = platform === 'darwin' ? path.join(home, 'Library/LaunchAgents/dev.garcon.plist') : path.join(home, '.config/systemd/user/garcon.service');
 if (!exists(unit)) { console.log('Updated. Run garcon setup to start the service, or restart your foreground Garcon.'); return 0; }
 const restart = run(process.execPath, [shim, 'service', 'restart']);
 if (restart) return restart;
 return run(process.execPath, [shim, 'doctor', '--wait', '10s']);
}
module.exports = { update, globalPrefix };
