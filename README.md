# Garcon

A local pass-through proxy for Claude Code and Codex that records usage. One Go
file with no dependencies, plus a SvelteKit dashboard embedded in the binary.

Each of the six coding-agent profiles is pointed at
`http://127.0.0.1:4141/<harness>/<account>/`, where `<harness>` is `claude` or
`codex` and `<account>` is the profile's email. The proxy forwards the request
unchanged to `api.anthropic.com` or `chatgpt.com` and streams the reply back.
Completion calls (`/v1/messages`, `/codex/responses`) are appended to
`~/.local/share/garcon/usage.jsonl` with the model and token counts the provider
reported: uncached input, cache read, cache write and output. No limits, no
retries, nothing rewritten.

Dashboard: http://127.0.0.1:4141, scoped by a shared time-range/harness/account filter bar:

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

## Dashboard development

`cd web && npm run dev` serves the dashboard on Vite's port and proxies `/api`
to the running garcon.
