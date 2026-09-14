# Garcon

Observability for coding agents. A local pass-through proxy in front of Claude Code, Codex,
OpenClaw, Hermes or any compatible client records what every call cost in tokens and shows it
in one dashboard, across accounts, tools and, with sync on, machines. No Go dependencies, dashboard embedded in the binary, loopback only, no credentials stored.

**Docs:** https://asieke.github.io/garcon/

## How it works

Point each agent at `http://127.0.0.1:4141/<harness>/<account>/<provider>/`. Requests are
forwarded unchanged and replies streamed back; completion calls are appended to
`~/.local/share/garcon/usage.jsonl` with model, status, latency and the provider's token
counts (uncached input, cache read, cache write, output). Subscription logins keep working.

| provider | upstream | recorded |
| --- | --- | --- |
| `anthropic` | api.anthropic.com | `/v1/messages` |
| `openai` | api.openai.com | `/v1/responses`, `/v1/chat/completions` |
| `openrouter` | openrouter.ai | `/api/v1/chat/completions` |
| `chatgpt` | chatgpt.com | `/backend-api/codex/responses` |

`/claude/<account>/` implies `anthropic`; `/codex/<account>/` implies `chatgpt`. Any other
harness name is accepted. `<account>` is a label: use the login the tool signs in with.

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
Garcon has no authentication, so it listens on loopback addresses only and answers only
requests addressed to `127.0.0.1`, `localhost` or `[::1]`; `-allow-remote` lifts both, and
opens the dashboard, the settings and the relay to that network.

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

From source (Go 1.27+, Node 22+; `mise install` provides both):
`scripts/install.sh --service` builds and installs; `--update` pulls and rebuilds.

## Connect a harness

Settings → Connect a harness generates these for any harness, provider and account.

```sh
# Claude Code
ANTHROPIC_BASE_URL=http://127.0.0.1:4141/claude/me@example.com \
_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL=1 claude

# Codex: ~/.codex/config.toml (a custom provider is required under a ChatGPT login)
model_provider = "garcon"
[model_providers.garcon]
name = "OpenAI"
base_url = "http://127.0.0.1:4141/codex/me@example.com/backend-api/codex"
wire_api = "responses"
requires_openai_auth = true
```

OpenClaw: set `baseUrl` per provider in `~/.openclaw/openclaw.json` (`…/openclaw/<account>/anthropic`,
`…/openai/v1`, `…/openrouter/api/v1`, `…/chatgpt/backend-api` for `openai-codex`). Hermes: set
`base_url` per provider in `~/.hermes/config.yaml` (`…/hermes/<account>/anthropic`, `…/openai/v1`,
`…/openrouter/api/v1`). Anything else: the base URL plus the SDK's prefix; OpenAI-compatible
clients need `stream_options: {"include_usage": true}` for token counts. Verify with one short
call and a new row in Logs.

## Dashboard

http://127.0.0.1:4141. Overview, Usage, Cost (estimated at list prices, editable), Models,
Accounts, Sessions (per account, harness and device), Activity, Performance, Latency (proxy
overhead vs upstream), Logs (CSV export), Settings. Raw rows: `/api/usage`.

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
machines. It is stored in `~/.config/garcon/config.json` with mode 0600 and is only ever sent
to the project URL it was saved with: changing the URL asks for the key again, and redirects
are never followed. Turning sync off
stops push/pull while retaining local usage. See the [sync guide](https://asieke.github.io/garcon/sync.html).

## Development

```
cmd/garcon/          entry point: flags, subcommands, HTTP wiring
internal/proxy/      routing and the pass-through proxy
internal/usage/      the Record type and usage-block parsing
internal/store/      usage.jsonl and the remote-row cache
internal/syncer/     Supabase push/pull and /api/settings
internal/service/    systemd and launchd management
internal/dashboard/  embedded build output of web/
web/                 SvelteKit dashboard          docs/       GitHub Pages site
scripts/             install.sh (source builds), release.sh
npm/ai-garcon/       the npm package (shim, README, built binaries)
supabase/            sync table schema            .claude/    agent skills
```

`go test ./...`; `cd web && npm run check && npm run dev` (proxies `/api` to a running garcon).
Release: every merge into `main` publishes a new patch version of `ai-garcon` to npm
(`.github/workflows/release.yml`; `[minor]` or `[major]` in the PR title bumps that part).
Pull requests run the same build without publishing (`.github/workflows/ci.yml`). The release
builds and packs in a job with a read-only token, and a separate job publishes that tarball
with provenance.
The version lives in the git tag and on npm, not in the repository; `scripts/release.sh
X.Y.Z --dry-run` runs the same build locally.
Agent skills in `.claude/skills/`: `install-garcon`, `update-garcon`, `add-provider`, `connect-supabase`, `commit-garcon`.
