# Garcon

Observability for coding agents. A local pass-through proxy in front of Claude Code, Codex,
OpenClaw, Hermes or any compatible client records what every call cost in tokens and shows it
in one dashboard, across accounts, tools and, with sync on, machines. Two Go files, no
dependencies, dashboard embedded in the binary, loopback only, no credentials stored.

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

## Install

```sh
npm i -g ai-garcon && garcon service install   # macOS and Linux, x64 and arm64
npm i -g ai-garcon@latest                       # update: a running service restarts itself
garcon service status|restart|uninstall
```

From source (Go 1.27+, Node 22+; `mise install` provides both): `scripts/install.sh --service`
builds and installs `~/.local/bin/garcon`; `--update` pulls the latest main, rebuilds and
restarts; `--uninstall` removes it. Flags: `-listen 127.0.0.1:4141`,
`-data ~/.local/share/garcon/usage.jsonl`, `-config ~/.config/garcon/config.json`.

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

Off by default; with the switch off Garcon makes no network calls. On, each machine upserts
its rows into a `garcon_usage` table in a Supabase project you own and pulls the others' rows
in, so every dashboard shows the union with a Device filter. First machine, with the Supabase
CLI installed and logged in:

```sh
garcon connect-supabase --name "work laptop"    # creates the project, applies the schema, stores the key
```

Other machines: Settings → Sync, paste the project URL and `sb_secret_` key, or rerun the
command with `--project-ref`. RLS is on with no policies, so only the secret key can read the
table. Per call, only device, time, harness, account, provider, model, status, latency and
token counts leave the machine. Key in `~/.config/garcon/config.json` (0600); device id and
cursors in `~/.local/share/garcon/sync.json`. Details: [docs](https://asieke.github.io/garcon/sync.html).

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
npm/                 ai-garcon and its platform packages
supabase/            sync table schema            .claude/    agent skills
```

`go test ./...`; `cd web && npm run check && npm run dev` (proxies `/api` to a running garcon).
Release: `scripts/release.sh X.Y.Z` cross-compiles for four platforms and publishes `ai-garcon`
plus its platform packages (`npm login` first; `--dry-run` only builds).
Agent skills in `.claude/skills/`: `install-garcon`, `update-garcon`, `add-provider`, `connect-supabase`.
