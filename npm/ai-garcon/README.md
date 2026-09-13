# ai-garcon

Observability for coding agents. One tiny local proxy sits in front of Claude Code, Codex,
OpenClaw, Hermes or any compatible client, records what every call cost in tokens, and shows
it in one dashboard, across accounts, tools and, with sync on, every machine you work from.
Requests and replies are forwarded unchanged; prompts, replies and keys are never stored.

Documentation: https://asieke.github.io/garcon/ · Source: https://github.com/asieke/garcon

## Install

```sh
npm i -g ai-garcon
garcon service install      # run now and at every login (systemd user unit / LaunchAgent)
```

macOS and Linux, x64 and arm64. Node is only needed for the installer; the proxy is one
static binary. The dashboard is at http://127.0.0.1:4141.

## Point an agent at it

```sh
# Claude Code
ANTHROPIC_BASE_URL=http://127.0.0.1:4141/claude/me@example.com \
_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL=1 claude
```

Codex, OpenClaw, Hermes and anything OpenAI- or Anthropic-compatible: Settings → Connect a
harness generates the exact configuration, or see the
[docs](https://asieke.github.io/garcon/connect.html). Make one short call and the row appears
in Logs.

## Commands

```sh
garcon service install|status|restart|uninstall
garcon connect-supabase --name "work laptop"        # first machine: sync via your own Supabase project
garcon connect-supabase --name "desk" --project-ref <ref>
garcon version
garcon [-listen 127.0.0.1:4141] [-data FILE] [-config FILE]   # foreground
```

## Update

```sh
npm i -g ai-garcon@latest && garcon service restart
```

Settings, the usage log and sync state are untouched.

## Sync across devices

Off by default; with the switch off Garcon makes no network calls. On, each machine upserts
its rows into a table in a Supabase project you own and pulls the others' rows in, so every
dashboard shows the union. Only device, time, harness, account, provider, model, status,
latency and token counts leave a machine. Details:
https://asieke.github.io/garcon/sync.html
