---
name: update-garcon
description: Update an installed Garcon to the latest main branch and restart it. Use when asked to update, upgrade, pull the latest Garcon, or after a Garcon pull request is merged and the running proxy should pick it up.
---

# Updating Garcon

Two ways to install, so first find out which one is in use: `which garcon`. A path under
`npm root -g` means npm; `~/.local/bin/garcon` means a source build.

**npm install:**

```sh
garcon update
```

**Source build**, from the repository checkout:

```sh
scripts/install.sh --update
```

It refuses to run with uncommitted changes, switches to `main` if needed, fast-forwards
from `origin/main`, prints the commits that came in, rebuilds the dashboard and the
binary, installs `~/.local/bin/garcon`, and restarts the service if one is running.
Agents keep their configuration; the usage log, the sync settings and the sync state are
untouched.

## Verify

1. `curl -s http://127.0.0.1:4141/api/config` answers (the script also waits for this).
2. Linux: `systemctl --user status garcon`; macOS: `launchctl print gui/$(id -u)/dev.garcon`.
3. If sync was on: `curl -s http://127.0.0.1:4141/api/settings | jq .settings.sync_enabled`
   is still `true`, and the dashboard's Settings → Sync shows a recent push.

## If it fails

- **uncommitted changes**: the user edited the checkout. Show `git status`, and ask before
  stashing or discarding; never reset their work.
- **not a fast-forward**: local commits on `main` that are not upstream. Show
  `git log origin/main..main` and ask how they want to reconcile.
- **build errors**: read the output; toolchain versions are pinned in `.mise.toml`
  (`mise install`).
- **service did not come back**: `journalctl --user -u garcon -n 50` (Linux) or
  `~/Library/Logs/garcon.log` (macOS). The previous binary is gone at that point, so fix
  forward; `git checkout <previous commit> && scripts/install.sh` rolls back.

Not a git checkout (installed from a copied directory): clone
`https://github.com/asieke/garcon`, run `scripts/install.sh --service` there, and use that
checkout from now on.
