---
name: add-provider
description: Point a coding harness (Claude Code, Codex, OpenClaw, Hermes, or any OpenAI- or Anthropic-compatible client) at Garcon for one of its four providers (Anthropic, OpenAI, OpenRouter, ChatGPT/Codex backend), or add a fifth provider to the proxy itself. Use when asked to route an agent's traffic through Garcon, record usage for a new account, connect a harness to a provider, or support a new upstream.
---

# Connecting a harness to a provider through Garcon

Garcon records a request when it arrives at `http://127.0.0.1:4141/<harness>/<account>/<provider>/…`
and forwards it unchanged to that provider. Nothing else is configured on the proxy side:
"adding a provider" means giving the harness the right base URL. The Settings tab of the
dashboard generates every snippet below for a chosen harness, provider and account; the
canonical recipes live in `web/src/lib/connect.ts`.

## Providers

| segment | upstream | recorded calls |
| --- | --- | --- |
| `anthropic` | api.anthropic.com | `/v1/messages` |
| `openai` | api.openai.com | `/v1/responses`, `/v1/chat/completions` |
| `openrouter` | openrouter.ai | `/api/v1/chat/completions` |
| `chatgpt` | chatgpt.com | `/backend-api/codex/responses` (ChatGPT-subscription Codex) |

`/claude/<account>/` implies `anthropic` and `/codex/<account>/` implies `chatgpt`; every other
harness name needs the provider segment. `<account>` is a label only: use the email the harness
signs in with so usage lands on the right subscription.

## Recipes

1. **Check the proxy is up**: `curl -s http://127.0.0.1:4141/api/config` (install with the
   `install-garcon` skill if not).
2. **Configure the harness** (replace `me@example.com`):
   - **Claude Code → anthropic**: run `garcon claude --account me@example.com` (arguments for
     claude go after `--`). This keeps Remote Control working. The environment-only form,
     `ANTHROPIC_BASE_URL=http://127.0.0.1:4141/claude/me@example.com _CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL=1 claude`,
     records usage the same way but Remote Control refuses to start under it.
   - **Codex → chatgpt**: needs a custom provider, because the built-in one ignores base URL
     overrides under a ChatGPT login (and a custom provider uses plain HTTP, not websockets,
     which is what lets the proxy see usage). In `~/.codex/config.toml`:
     `model_provider = "garcon"` and `[model_providers.garcon]` with `name = "OpenAI"`,
     `base_url = "http://127.0.0.1:4141/codex/me@example.com/backend-api/codex"`,
     `wire_api = "responses"`, `requires_openai_auth = true`. Or pass the same via `-c` flags.
   - **OpenClaw → any provider**: in `~/.openclaw/openclaw.json` under
     `models: { mode: "merge", providers: { … } }` set `baseUrl` on the built-in provider:
     `anthropic: ".../openclaw/me@example.com/anthropic"`,
     `openai: ".../openclaw/me@example.com/openai/v1"`,
     `openrouter: ".../openclaw/me@example.com/openrouter/api/v1"`,
     `"openai-codex": ".../openclaw/me@example.com/chatgpt/backend-api"`.
     Each adapter appends its own endpoint path.
   - **Hermes → anthropic / openai / openrouter**: in `~/.hermes/config.yaml` under
     `providers:` set `base_url` for `anthropic` (`.../hermes/me@example.com/anthropic`),
     `openai` (`.../openai/v1`) or `openrouter` (`.../openrouter/api/v1`). Hermes appends
     `/v1/messages` or `/chat/completions` and requests `include_usage`. For a one-off,
     `OPENAI_BASE_URL=…` works for the OpenAI-compatible modes.
   - **Anything else**: use `http://127.0.0.1:4141/<name>/<account>/<provider>` plus whatever
     path prefix the client's SDK expects (`/v1` for OpenAI SDKs, `/api/v1` for OpenRouter).
     OpenAI-compatible clients only report tokens on streamed replies when they send
     `stream_options: {"include_usage": true}`.
3. **Verify**: make one short call (`claude -p hi`, `codex exec hi`, or the harness's own
   one-shot mode) and confirm a new row at `http://127.0.0.1:4141/api/usage` with the expected
   harness, account, provider and non-zero tokens. The Logs tab shows the same row.
4. **Keys and cookies**: never put API keys in the base URL or in this repo. Garcon forwards
   `Authorization` and `x-api-key` headers as they are. Endpoints that depend on browser cookies
   (Codex's plugin, app and usage-limit traffic) do not survive a plain-HTTP proxy, so only
   model calls are routed for Codex.

## Adding a fifth upstream

Edit `providers` in `main.go` (segment name → scheme and host), extend `isCompletion` if the
provider's completion path has a new suffix, and teach `fold` any new usage field names. Then
add the provider to `PROVIDERS` and `HARNESS_PROVIDERS` in `web/src/lib/connect.ts`, a pricing
rule in `web/src/lib/pricing.ts`, a case in `TestParseRoute`/`TestFold`, and the README table.
Rebuild with `scripts/install.sh`.
