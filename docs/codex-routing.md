# Codex account routing

Enable routing in **Providers → Codex**. Enrolled ChatGPT accounts form the pool and can receive routed conversations. Adding a login doesn't enroll it.

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

## How an account is chosen

An account needs a usable login, fresh quota, and access to the requested model. Garcon considers the lowest-numbered eligible priority group first. Within that group:

```text
score = remaining quota percentage / hours until reset
```

For multiple general quota windows, the lowest window score wins. Active request counts don't change it. Percentages compare headroom, not absolute tokens across different plans. Missing, expired, stale, or exhausted limits make an account ineligible. The provider can still reject a request, including for a model-specific limit.

## Why a conversation stays put

Garcon saves the session-to-account assignment in SQLite before forwarding. Requests with the same session ID keep that account across restarts. A continuation without an assignment stays on its identifiable original enrolled account; otherwise it fails explicitly.

If the assigned account becomes unavailable, the conversation returns an error. Garcon doesn't move it or replay the call elsewhere. Start a new conversation to select again. Unpinning and migration aren't supported.

Upstream 401, 403, or 429 responses temporarily exclude an account from new assignments. A 401 also requests credential renewal. Reset credits are never redeemed.

## Logins and renewal

Use **Add provider → Codex → Copy skill** in either local or remote mode, or run `garcon accounts login codex --profile NAME` on the server. Garcon needs the Codex CLI and creates a separate `~/.codex-garcon-*` profile. Run `garcon accounts refresh`, verify with `garcon accounts list`, then enroll the new account separately.

Discovery reads `~/.codex/auth.json`, `~/.codex-*/auth.json`, and the service's `CODEX_HOME`. Duplicate accounts prefer the token with the latest expiry. Keyring-only logins, API keys, and Hermes profiles aren't part of this pool.

Garcon checks authentication and model availability every minute; quota collection runs every five minutes. Near expiry or after a 401, it asks `codex app-server` to refresh that profile's login. Codex saves the credentials; Garcon checks the same account received a usable replacement. Missing or revoked grants require signing in again. The catalog adapter uses protocol version `0.155.0` and may need updating when Codex changes.

## See and control routing

Sessions shows task titles, projects, account assignments, and model traffic. Task titles, paths, and archive flags come from Codex's `state_*.sqlite` files, read without modification. Missing or incompatible indexes fall back to IDs. Task metadata stays local and isn't saved in Garcon's database.

`GET /api/routing/codex` returns account health, quota, scores, and assignment counts. `PUT` requires `enabled`; optional `accounts` selects enrolled IDs and `priorities` maps IDs to integers from 1–99. Enabling without `accounts` enrolls currently discovered logins. Disabling restores pass-through credentials.

Routed calls and configuration changes require a local client and matching Origin when supplied. Successful upstream responses include `X-Garcon-Account-Id`. Routing supports HTTP streaming; WebSocket upgrades and bodies over 32 MiB are rejected.

See [the API reference](api.html) for Sessions, Logs, and Analytics endpoints, and [local files](files.html) for storage and migration.

## Remote dashboard

With `-allow-remote`, routing changes and all other dashboard writes return 403, even from localhost or through a reverse proxy. Use `garcon routing show`, then pipe JSON to `garcon routing set --json-stdin` on the server. Include all selected account IDs and priorities to preserve the existing pool; append `--data FILE` when using a custom database. Existing sessions stay pinned.
