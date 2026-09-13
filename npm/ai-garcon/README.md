# ai-garcon

Observability for coding agents. One tiny local proxy sits in front of Claude Code, Codex,
OpenClaw, Hermes or any compatible client, records what every call cost in tokens, and shows
it in one dashboard, across accounts, tools and, with sync on, every machine you work from.
Requests and replies are forwarded unchanged; prompts, replies and keys are never stored.

Documentation: https://asieke.github.io/garcon/ · Source: https://github.com/asieke/garcon

## Install and set up this machine

```sh
npm install -g ai-garcon@latest
garcon setup
```

macOS and Linux, x64 and arm64; Node 18+ is needed to run the npm command.
`setup` starts Garcon at login and verifies http://127.0.0.1:4141. Open the printed
Settings link, choose your harness and account label, and copy its configuration.
Restart the harness, send one short request, and check Logs. Existing usage and sync
settings are preserved when setup is rerun. Sync is optional.

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
one or restart your foreground process. Usage, settings and device identity are kept.
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
garcon doctor                     # reachability, versions, first request, sync progress/errors
garcon service status
garcon service uninstall          # stop autostart; keep usage and settings
npm uninstall -g ai-garcon
```

Restore each harness's original base URL/provider before removing the proxy, so your
tools can keep connecting. Linux service logs: `journalctl --user -u garcon`.
macOS logs: `~/Library/Logs/garcon.log`.

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


## Sync across devices

Optional: local recording works without Supabase. First, run `garcon setup` on each
machine. Give each machine a distinct device name in Settings → Sync. Use the same
project on every machine; do not copy `sync.json` or another machine's data directory,
because that would duplicate its identity.

**First machine, browser path (no Supabase CLI required):**

1. Create or choose a Supabase project in your own account.
2. Run `garcon connect-supabase --print-sql` and paste the output into that project's SQL Editor.
3. In Garcon Settings → Sync, enter a device name, project URL and secret (`sb_secret_`) key
   from Project Settings → API Keys. Enable sync and Save. Garcon verifies table access
   before saving. Keep this project key in your password manager for the next machine.

**First machine, automated CLI path:** install the [Supabase CLI](https://supabase.com/docs/guides/cli),
run `supabase login`, then explicitly choose project creation or reuse:

```sh
garcon connect-supabase --create-project --name "work laptop"
# Or apply the schema to an existing project:
garcon connect-supabase --project-ref <ref> --name "work laptop"
```

Creation checks CLI capabilities first; `--org-id` and `--region` choose where to create
it. Review your organization's project limits and plan in Supabase. If a later step fails,
reuse the printed project ref instead of creating another project.

**Subsequent machines:** open Settings → Sync, paste the same URL and key, choose a new
device name, enable and Save. No CLI, new project, or SQL needed. For a headless machine,
read the key from a password manager or protected file rather than putting it in shell history:

```sh
garcon connect-supabase --name "home desktop" \
  --project-url https://<ref>.supabase.co --key-stdin < /path/to/protected-key-file
# Or with the Supabase CLI already logged in:
garcon connect-supabase --name "home desktop" --project-ref <ref> --skip-schema
garcon doctor
```

Save verifies table access; syncing happens in the background. Within about a minute,
Settings → Sync should show the other devices. `garcon doctor` reports pending uploads
and the latest sync errors. First enable uploads existing local history.

Only device, time, harness, account, provider, model, status, latency and token counts
are synced. Prompts and replies are not recorded. The project secret key can access other
project data too: use a dedicated project and share the key only with your own trusted
machines. It is stored in `~/.config/garcon/config.json` with mode 0600. Turning sync off
stops push/pull while retaining local usage. See the [sync guide](https://asieke.github.io/garcon/sync.html).
