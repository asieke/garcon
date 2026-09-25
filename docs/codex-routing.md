# Codex OAuth account routing

Garcon can choose among enrolled local Codex ChatGPT logins for new conversations.
The dashboard, routing API and model proxy remain one process at `127.0.0.1:4141`.
Routing is off by default. Open **Limits → Codex account routing** and enable the
listed accounts. This explicitly enrolls the discovered accounts at that moment;
a login added later does not silently join the pool. Disable and enable again to
enroll the current set. All enrolled accounts can receive the content of Codex
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
Codex is not necessarily the account serving a conversation; Garcon's Limits and
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

New conversations choose the greatest **minimum remaining percentage** across
their reported general Codex limit windows, divided by one plus the account's
active requests. This balances short-term and weekly headroom and concurrent
work. Different plans have different capacities: this is percentage headroom,
not a measurement of absolute tokens. Missing, stale, exhausted, or expired
windows are never treated as free usage. Only accounts whose catalog lists the
requested model are eligible. Additional model-specific meters are not yet
mapped to model IDs; the upstream remains authoritative and may reject a call.

The stable Codex session ID identifies the conversation; child threads sharing
that session keep its account. A hashed ID-to-account assignment is saved before
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
larger than 32 MiB are rejected explicitly. Assignments are capped at 8,000 and
never silently evicted, to avoid moving an old conversation between accounts.

## Local controls and verification

`GET /api/routing/codex` returns redacted account status, remaining percentage,
active requests and assignment counts. `PUT` with `{"enabled":true}` enrolls the
currently discovered accounts; `{"enabled":false}` restores pass-through.
An optional `accounts` array restricts enrollment to specified discovered account
IDs. Empty, duplicate, and unknown account selections are rejected.
Mutations and routed model requests require a loopback client and local Host,
and reject a different Origin even when Garcon was started with `-allow-remote`.
Successful upstream responses include `X-Garcon-Account-Id` for verification.

`codex-routing.json` and `codex-routing-pins.json` live beside `usage.jsonl`.
They contain account IDs and hashed session assignments, never prompts or tokens,
and are written atomically with user-only permissions. Invalid state fails closed.
