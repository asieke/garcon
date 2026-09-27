# Codex account routing

Codex account selection runs automatically. Existing local logins form the initial pool; use **In pool** to change that selection. Later logins require enrollment. An empty pool returns an error.

## Connect Codex

Merge this into your Codex user configuration, keeping only one `model_provider` setting and one Garcon provider table:

```toml
model_provider = "garcon"

[model_providers.garcon]
name = "Garcon"
base_url = "http://127.0.0.1:4141/codex/backend-api/codex"
wire_api = "responses"
requires_openai_auth = true
supports_websockets = false
```

Start a fresh CLI process or restart Codex Desktop. Garcon replaces the outbound bearer token and account header, so the login shown in Codex may differ from the account serving a task. Check Sessions or Logs for the actual assignment.

This routes local model traffic, not cloud jobs or every account-bound Desktop feature. Codex never falls back to OpenRouter or Claude. Pi's Codex route shares the same pool; other routes use their configured provider.

### Desktop voice

Realtime voice calls (including `gpt-live-1-codex`) retain the caller's OAuth login instead of selecting a pooled account. Codex can create the call through Garcon and then join its control WebSocket directly with its own login; both connections must use the same account. Pooling call creation can produce a successful HTTP 201 followed by a WebSocket 404.

Garcon passes realtime WebSocket upgrades through bidirectionally when the client routes them through the proxy. It records connection metadata, without parsing or storing audio/control frames or estimating their token usage. Keep `supports_websockets = false` for pooled coding requests; this Responses API setting is separate from voice transport.

## How an account is chosen

An account needs a usable login, fresh quota, and access to the requested model. Garcon selects across the enrolled eligible accounts using:

```text
score = remaining quota percentage / hours until reset
```

For multiple general quota windows, the lowest window score wins. Active request counts don't change it. Percentages compare headroom, not absolute tokens across different plans. Missing, expired, stale, or exhausted limits make an account ineligible. The provider can still reject a request, including for a model-specific limit.

## Why a conversation stays put

Garcon saves the session-to-account assignment in SQLite before forwarding. Requests with the same session ID keep that account across restarts. A continuation without an assignment stays on its identifiable original enrolled account; otherwise it fails explicitly.

If the assigned account becomes unavailable, the conversation returns an error. Garcon doesn't move it or replay the call elsewhere. Start a new conversation to select again. Unpinning and migration aren't supported.

Upstream 401, 403, or 429 responses temporarily exclude an account from new assignments. A 401 also requests credential renewal. Reset credits are never redeemed.

## Logins and renewal

Use **Providers → Add account → Codex OAuth account** for browser sign-in. Garcon needs the Codex CLI and creates a separate `~/.codex-garcon-*` profile. Enable the new account in the pool afterward.

Discovery reads `~/.codex/auth.json`, `~/.codex-*/auth.json`, and the service's `CODEX_HOME`. Duplicate accounts prefer the token with the latest expiry. Keyring-only logins, API keys, and Hermes profiles aren't part of this pool.

Garcon checks authentication and model availability every minute; quota collection runs every five minutes. Near expiry or after a 401, it asks `codex app-server` to refresh that profile's login. Codex saves the credentials; Garcon checks the same account received a usable replacement. Missing or revoked grants require signing in again. The catalog adapter uses protocol version `0.155.0` and may need updating when Codex changes.

## See and control routing

Sessions shows task titles, projects, account assignments, and model traffic. Task titles, paths, and archive flags come from Codex's `state_*.sqlite` files, read without modification. Missing or incompatible indexes fall back to IDs. Task metadata stays local and isn't saved in Garcon's database.

`GET /api/routing/codex` returns account health, quota, scores, and assignment counts. `PUT` accepts `accounts` to select enrolled IDs. Omit it to preserve the pool, or send an empty array to remove all accounts. Legacy `enabled` and `priorities` fields remain accepted for compatibility but do not disable routing or influence account selection.

Routed calls and configuration changes require a local client and matching Origin when supplied. Successful pooled responses include `X-Garcon-Account-Id`. Pooled coding requests support HTTP streaming; their WebSocket upgrades and bodies over 32 MiB are rejected. Realtime voice bypasses the pool and retains the client's credentials as described above.

See [the API reference](api.html) for Sessions, Logs, and Analytics endpoints, and [local files](files.html) for storage and migration.
