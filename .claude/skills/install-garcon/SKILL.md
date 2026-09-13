---
name: install-garcon
description: Install, upgrade, or remove Garcon, the local Claude Code and Codex usage proxy, on macOS or Linux (including Omarchy), optionally as a background service that starts at login, and point the coding harnesses at it. Use when asked to set up Garcon on a machine, get it running in the background, rebuild after pulling changes, or repair a broken install.
---

# Installing Garcon

Read `README.md` first for what the proxy does and how harnesses are pointed at it.

The normal path is npm: `npm i -g ai-garcon && garcon service install` (Node 18+; macOS
and Linux on x64 or arm64). That installs a prebuilt binary and runs it now and at every
login: a systemd user unit named `garcon` on Linux, a LaunchAgent `dev.garcon` on macOS.
`npm i -g ai-garcon@latest && garcon service restart` upgrades it (the `update-garcon` skill).

Building from source instead, with `scripts/install.sh`:

1. **Check prerequisites**: Go 1.27+ and Node 22+ on `PATH`. If `mise` is installed,
   `mise install` inside the repo provides both (pinned in `.mise.toml`). Do not install
   toolchains system-wide without asking.
2. **Build and install**: `scripts/install.sh` builds the dashboard and the binary and installs
   `~/.local/bin/garcon`. Add `--service` to also run it now and at every login:
   a systemd user unit named `garcon` on Linux, a LaunchAgent `dev.garcon` on macOS.
   To upgrade later, `scripts/install.sh --update` pulls the latest `main` first (the
   `update-garcon` skill).
3. **Verify**: `curl -s http://127.0.0.1:4141/api/usage` returns a JSON array and
   http://127.0.0.1:4141 shows the dashboard. Linux: `systemctl --user status garcon`.
   macOS: `launchctl print gui/$(id -u)/dev.garcon`; log in `~/Library/Logs/garcon.log`.
4. **Point harnesses at it** using the environment variables and Codex provider settings
   in the README's "Pointing a harness at it" section, one base URL per account. Then
   run one short `claude -p` or `codex exec` call and confirm a new row appears at
   `/api/usage`.
5. **Remove**: `garcon service uninstall && npm rm -g ai-garcon`, or `scripts/install.sh --uninstall`
   for a source build. The usage log at
   `~/.local/share/garcon/usage.jsonl` is left in place; delete it only if asked.

Notes: the proxy binds to 127.0.0.1 only and stores no credentials, so nothing needs
securing beyond the usage log, which contains account emails and token counts. On Linux
the service runs in the user session; enabling `loginctl enable-linger` is only needed
if it must run while nobody is logged in. Use the omarchy skill for any Omarchy desktop
configuration beyond this.
