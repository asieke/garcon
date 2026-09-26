# Codex OAuth account routing

Garcon can choose among enrolled local Codex ChatGPT logins for new conversations.
The dashboard, routing API and model proxy remain one process at `127.0.0.1:4141`.
Routing is off by default. Open **Accounts → Codex** and enable the
listed accounts. This explicitly enrolls the discovered accounts at that moment;
a login added later does not silently join the pool. Use the account row controls to enroll or remove individual accounts. All enrolled accounts can receive the content of Codex
conversations routed through this endpoint.

## Connect Codex

Add the following to the relevant Codex user configuration, preserving other settings:

```toml
model_provider = "garcon"

[model_providers.garcon]
name = "Garcon"
base_url = "http://127.0.0.1:4141/codex/backend-api/codex"
wire_api = "responses"
requires_openai_auth = true
supports_websockets = false
```

Restart Codex Desktop or start a fresh CLI process so it loads the provider.
Codex can remain signed into its primary account. Garcon replaces the bearer token
and account header on outbound Codex requests. The primary account displayed in
Codex is not necessarily the account serving a conversation; Garcon's Accounts, Sessions, and
Logs views show routing status and actual usage attribution. Non-Codex traffic
retains its existing pass-through behavior. This integration targets local Codex
model traffic, not cloud jobs or every account-bound Desktop feature.

## Discovery and authentication

Discovery reads ChatGPT OAuth logins in `~/.codex/auth.json`,
`~/.codex-*/auth.json`, and an explicit `CODEX_HOME`. Multiple profiles selecting
the same account are deduplicated, preferring the latest token expiry. Only
file-based Codex logins are enrolled in this version; keyring-only logins,
Hermes credentials, and API keys are not part of this routing pool.

Garcon reads credentials on demand, never persists a copied token, and does not
sync credentials or routing state. Near expiry or after an upstream 401, it calls
`codex app-server` with that profile's `CODEX_HOME`, initializes it, and sends
`account/read` with `refreshToken: true`. Codex performs the refresh and saves its
own login. Garcon checks that a changed, usable token for the same account was
saved. This creates no model request or agent task. Missing or revoked refresh
grants still require signing in through Codex. Codex CLI must be installed.

Each enrolled account's model catalog is checked independently. Both quota and
model information must be current before an account is eligible. Catalog and auth
checks run once a minute; the existing limits collector refreshes every five
minutes. This adapter uses the Codex 0.155.0 catalog protocol and should be checked
when upgrading Codex.

## Selection and conversation continuity

New conversations consider eligible accounts in the lowest-numbered priority group
first. Within that group, the highest **remaining percentage / hours until reset**
wins. When an account has multiple general quota windows, its score is the minimum
of those window scores. Active request counts do not alter this formula. Priority
groups can be reordered, and individual accounts can move between groups.
Different plans have different capacities: the score compares percentages, not
absolute token allowances. Missing, stale, exhausted, or expired
windows are never treated as free usage. Only accounts whose catalog lists the
requested model are eligible. Additional model-specific meters are not yet
mapped to model IDs; the upstream remains authoritative and may reject a call.

The stable Codex session ID identifies the conversation; child threads sharing
that session keep its account. The session ID, its lookup hash, account, and timestamps are saved in SQLite before
forwarding, so restarting Garcon preserves the choice. Existing requests carrying
response or turn state but lacking a saved assignment stay with the original
enrolled caller account. Without an identifiable original account they fail
explicitly. No request is automatically replayed on another account.

An exhausted or unavailable assigned account returns an error; it does not move
the conversation. Start a new conversation to select another eligible account.
Upstream 401/403/429 responses temporarily exclude the affected account from new
assignments; 401 also requests credential renewal. Garcon never redeems reset
credits. Unpinning/migrating existing conversations is not implemented.

This first version uses HTTP streaming. WebSocket upgrades are rejected rather
than forwarded without per-request model checks or usage attribution. Requests
larger than 32 MiB are rejected explicitly. SQLite assignments are never silently evicted, to avoid moving an old conversation between accounts.

## Local controls and verification

`GET /api/routing/codex` returns redacted account status, remaining percentage,
active requests and assignment counts. `PUT` with `{"enabled":true}` enrolls the
currently discovered accounts; `{"enabled":false}` restores pass-through.
An optional `accounts` array restricts enrollment to specified discovered account
IDs. A `priorities` object maps account IDs to integers from 1–99. Duplicate and
unknown selections are rejected; an enabled pool must contain at least one account.
Mutations and routed model requests require a loopback client and local Host,
and reject a different Origin even when Garcon was started with `-allow-remote`.
Successful upstream responses include `X-Garcon-Account-Id` for verification.

## Local SQLite database

`~/.local/share/garcon/usage.db` holds routing preferences, priority groups,
session assignments, request metadata, usage, quota snapshots, price caches,
and service diagnostics. The Go process embeds a pure-Go SQLite driver; no
separate database service is required. WAL mode and user-only file permissions
are enabled.

On first startup, the old usage.jsonl, limits.json, prices.json, codex-routing.json,
and codex-routing-pins.json are imported together in a transaction. Original files
are preserved. An import marker prevents duplicates on subsequent restarts.
Malformed legacy data stops migration with an error rather than silently losing it.
Older hashed-only assignments remain sticky; their readable session ID appears
when that conversation next passes through Garcon.

The **Sessions** view matches session IDs to task titles and workspace paths in
the local Codex task index. Search by title, project, account, model, or full ID;
click a title to inspect that task's requests, or copy its full session ID. Titles
also appear in the request ticker, logs, and request inspector. In-flight sessions
appear first, with elapsed request time. Background `codex-auto-review` calls are
identified separately and do not replace the main conversation model.

Activity describes model traffic only: a task may be running tools or waiting for
input between requests. "No model request" does not mean the Codex task is done.
Task metadata is read on demand from `state_*.sqlite` in `~/.codex`, `~/.codex-*`,
and `CODEX_HOME`, using read-only connections. Only matching titles, workspace
paths, and archive flags are read; they are not copied into Garcon's database.
This is a best-effort adapter for Codex's private schema: unavailable or newer
incompatible indexes fall back to session IDs. Metadata is only exposed to local
clients, including when Garcon uses `-allow-remote`.

The **Sessions** view also shows account assignments and last activity.
**Logs** is a searchable, paginated request ledger including routing failures,
upstream errors, and interrupted streams. Requests are inserted at dispatch and
updated at completion, so in-flight traffic appears in the bottom ticker. A
restart marks unfinished requests interrupted. Prompt text, response bodies,
headers, and OAuth credentials are not written to the ledger.

**Analytics** offers usage, models, and API-equivalent cost, aggregated from
SQLite. Cost estimates use the cached OpenRouter price catalog, then bundled
fallback prices. Models without a rate are explicitly counted as unpriced.
Estimates are not subscription charges.

Read-only APIs: `/api/sessions`, `/api/logs?limit=50&offset=0&q=...&errors=true`,
`/api/events?limit=200&offset=0`, and `/api/analytics?since=<unix-ms>`.

## Add another account

Use a distinct profile name and sign in through the official Codex CLI:

```sh
CODEX_HOME="$HOME/.codex-new-account" codex -c 'cli_auth_credentials_store="file"' login
```

Choose the desired OpenAI account in the browser. Then use **Refresh** in Garcon,
enroll the discovered account, and assign a priority. OAuth credentials remain
in the profile's Codex-owned auth.json; Garcon does not duplicate them in SQLite.
