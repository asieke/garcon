# Garcon

A local pass-through proxy for coding agents (Claude Code, Codex, OpenClaw,
Hermes) that records usage per account. One Go file with no dependencies, plus a
SvelteKit dashboard embedded in the binary.

Each agent is pointed at `http://127.0.0.1:4141/<harness>/<account>/<provider>/`,
where `<harness>` names the tool, `<account>` is the login it uses (an email), and
`<provider>` is the upstream: `anthropic`, `openai`, `openrouter` or `chatgpt`.
Claude Code and Codex talk to one provider each, so their URLs omit the segment:
`/claude/<account>/` goes to `api.anthropic.com` and `/codex/<account>/` to
`chatgpt.com`. The proxy forwards every request unchanged and streams the reply
back. Completion calls (`…/messages`, `…/responses`, `…/chat/completions`) are
appended to `~/.local/share/garcon/usage.jsonl` with the model and token counts
the provider reported: uncached input, cache read, cache write and output. No
limits, no retries, nothing rewritten.

Dashboard: http://127.0.0.1:4141. A grouped sidebar organizes Analytics, Explore, and
Diagnostics, with Settings in a separate Administration area. Usage views share
time-range/harness/account filters; Settings always describes the whole instance.
The sidebar becomes a menu on phones. Views can be bookmarked (for example,
`/?view=cost`) and support browser Back/Forward. See the [UI design notes](docs/ui-navigation.md).

- **Overview**, **Usage**, **Models**, **Accounts**: request and token volume, composition, rankings.
- **Cost**: estimated spend at published pay-as-you-go list prices (never a bill: subscriptions
  bill differently), cache savings, blended $/1M tokens, 30-day run rate, cost over time by
  model, cost by account and model, and an editable pricing table. List prices live in
  `web/src/lib/pricing.ts`; edits in the pricing table are overrides stored in the browser only.
  Models with no known price are flagged rather than silently priced at zero.
- **Sessions**: requests grouped into coding sessions per account and harness (a session ends
  after a configurable idle gap), with a per-account timeline, live markers, and per-session
  tokens, peak context, estimated cost and an expandable request list.
- **Activity**: hour-of-day × weekday and calendar heatmaps, streaks, busiest hour and day,
  peak concurrent requests.
- **Performance**: generation speed (output tokens/s) and time-to-first-byte by model, a
  latency vs output-size scatter, and context-size and output-size distributions.
- **Latency**: the connection-setup overhead the proxy itself adds (via `connect_ms`/
  `first_byte_ms` on each record) versus upstream/model response time, which dominates.
- **Logs**: sortable, filterable request table with per-request detail and CSV export.
- **Settings**: how this instance is wired (listen address, usage log, providers and their
  upstreams, every harness and account seen), a snippet builder that produces the exact
  configuration for pointing any harness at any provider, and browser-side state such as
  custom prices. Backed by `GET /api/config`.

Raw data: http://127.0.0.1:4141/api/usage.

## Install

Needs Go 1.27+ and Node 22+ (`mise install` in the repo provides both). Works on macOS
and Linux (including Omarchy).

```sh
./install.sh            # build dashboard + binary, install ~/.local/bin/garcon
./install.sh --service  # also run it now and at every login
```

`--service` creates a systemd user unit `garcon` on Linux (`systemctl --user status garcon`,
`journalctl --user -u garcon`) or a LaunchAgent `dev.garcon` on macOS
(`launchctl print gui/$(id -u)/dev.garcon`, log in `~/Library/Logs/garcon.log`).
Rerun `./install.sh` after pulling changes; it restarts a running service.
`./install.sh --uninstall` removes the service and binary but keeps the usage log.
An agent can do all of this from the `install-garcon` skill in `.claude/skills/`.

To run it by hand instead: `garcon [-listen 127.0.0.1:4141] [-data ~/.local/share/garcon/usage.jsonl]`.

## Pointing a harness at it

`~/.config/agent-profiles/launch.py` does this for the named launchers.
By hand:

```sh
ANTHROPIC_BASE_URL=http://127.0.0.1:4141/claude/me@example.com \
_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL=1 claude

codex -c model_provider="garcon" -c model_providers.garcon.name="OpenAI" \
  -c model_providers.garcon.base_url="http://127.0.0.1:4141/codex/me@example.com/backend-api/codex" \
  -c model_providers.garcon.wire_api="responses" -c model_providers.garcon.requires_openai_auth=true
```

Codex needs its own provider entry because the built-in one ignores base URL
overrides under a ChatGPT login; a custom provider also uses plain HTTP instead of
the websocket transport, which is what lets the proxy read the usage. Only Codex's
model calls are routed; its plugin, app and usage-limit traffic goes to chatgpt.com
directly (those endpoints rely on cookies that do not survive a plain-HTTP proxy).

### OpenClaw

OpenClaw's built-in providers accept a `baseUrl` override in `~/.openclaw/openclaw.json`
(or an agent's `models.json`). Each adapter appends its own path (`/v1/messages`,
`/v1/responses`, `/v1/chat/completions`), so the base URL ends at the provider segment:

```json5
{
  models: {
    mode: "merge",
    providers: {
      anthropic:  { baseUrl: "http://127.0.0.1:4141/openclaw/me@example.com/anthropic" },
      openai:     { baseUrl: "http://127.0.0.1:4141/openclaw/me@example.com/openai/v1" },
      openrouter: { baseUrl: "http://127.0.0.1:4141/openclaw/me@example.com/openrouter/api/v1" },
    },
  },
}
```

Keys stay in OpenClaw's own config or environment; the proxy forwards the
`Authorization` and `x-api-key` headers untouched. The `openai-codex` (ChatGPT OAuth)
provider talks to `chatgpt.com/backend-api`, so its base URL is
`http://127.0.0.1:4141/openclaw/me@example.com/chatgpt/backend-api`.

### Hermes

Hermes uses OpenAI-compatible chat completions by default (OpenRouter, or any
`base_url` ending in `/v1`) and native Anthropic Messages for `api_mode:
anthropic_messages`. It appends `/chat/completions` or `/v1/messages` itself and asks
for `stream_options.include_usage`, which is what lets the proxy see token counts.
In `~/.hermes/config.yaml`:

```yaml
providers:
  openrouter:
    base_url: http://127.0.0.1:4141/hermes/me@example.com/openrouter/api/v1
  anthropic:
    base_url: http://127.0.0.1:4141/hermes/me@example.com/anthropic
  openai:
    base_url: http://127.0.0.1:4141/hermes/me@example.com/openai/v1
```

or, for a one-off, `OPENAI_BASE_URL=http://127.0.0.1:4141/hermes/me@example.com/openai/v1`.
OpenRouter reports models as `anthropic/claude-sonnet-5`; the Cost view prices those by
the bare id.

The `add-provider` skill in `.claude/skills/` walks an agent through any of these, and the
Settings page generates the same snippets for a chosen harness, provider and account.

### Anything else

Any harness name works: `/<name>/<account>/<provider>/…` records rows tagged with
that name, and the dashboard lists it alongside the known ones. OpenAI-compatible
clients only get token counts when they request usage in streamed replies
(`stream_options: {"include_usage": true}`); a client that does not is recorded
with zero tokens.

## Dashboard development

`cd web && npm run dev` serves the dashboard on Vite's port and proxies `/api`
to the running garcon.
