# ai-garcon

Observability for coding agents. One tiny local proxy sits in front of Claude Code, Codex,
OpenClaw, Hermes or any compatible client, records what every call cost in tokens, and shows
it in one dashboard, across accounts and tools on this machine.
Requests and replies are forwarded unchanged; prompts, replies and keys are never stored.

Documentation: https://asieke.github.io/garcon/ · Source: https://github.com/asieke/garcon

## Install and set up this machine

```sh
npm install -g ai-garcon@latest
garcon setup
```

macOS and Linux, x64 and arm64; Node 18+ is needed to run the npm command.
`setup` starts Garcon at login and verifies http://127.0.0.1:4141. Open the printed
Settings link, choose your harness, and copy its configuration.
Restart the harness, send one short request, and check Logs. Existing usage is preserved when setup is rerun.

To try it without a global install: `npx ai-garcon@latest` runs in the foreground.
For containers or Linux without a systemd user session, run `garcon` in one terminal
and `garcon setup --no-service` in another. A custom foreground address works with
`garcon -listen 127.0.0.1:4242` and `garcon setup --no-service --url http://127.0.0.1:4242`.

If installation fails with EACCES, use a Node version manager or a user-owned npm
prefix ([npm's instructions](https://docs.npmjs.com/resolving-eacces-permissions-errors-when-installing-packages-globally/)).
Run setup as your normal user. If `garcon` is not found, ensure `$(npm prefix -g)/bin`
is on PATH and restart your shell. `type -a garcon` finds competing source/npm installs.

## Update

```sh
garcon update
```

This updates the owning global npm installation, refreshes an installed service,
and waits for its new version to answer. Without a service, it tells you how to start
one or restart your foreground process. Local usage is kept.
Updating briefly restarts the proxy; finish active agent requests first.
For an older Garcon without `update`, use:

```sh
npm install -g ai-garcon@latest
garcon service restart
garcon doctor --wait 10s
```

The service uses its own executable at `~/.local/share/garcon/bin/garcon`, so changing
Node versions or clearing an npx cache cannot remove it. After changing Node versions,
reinstall the npm command in the new environment and run `garcon setup`.

## Check or remove an installation

```sh
garcon doctor                     # reachability, versions, first request
garcon service status
garcon service uninstall          # stop autostart; keep usage and settings
npm uninstall -g ai-garcon
```

Restore each harness's original base URL/provider before removing the proxy, so your
tools can keep connecting. Linux service logs: `journalctl --user -u garcon`.
macOS logs: `~/Library/Logs/garcon.log`.

## Point an agent at it

```sh
# Claude Code, with Remote Control (the session shows in the claude.ai app)
garcon claude            # arguments after -- go to claude

# Claude Code, environment only (no Remote Control)
ANTHROPIC_BASE_URL=http://127.0.0.1:4141/claude \
_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL=1 claude
```

Codex, OpenClaw, Hermes and anything OpenAI- or Anthropic-compatible: Settings → Connect a
harness generates the exact configuration, or see the
[docs](https://asieke.github.io/garcon/connect.html). Make one short call and the row appears
in Logs.


## Local data

Each installation records and displays its own usage. Cross-device syncing has been removed.
Legacy sync settings and remote caches are ignored; local usage history is preserved.
