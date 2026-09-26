# Subscription limits

Codex's routing card shows available quota and reset times. Open **http://127.0.0.1:4141/usage-widget/** for the compact Codex and Claude allowance view.

These percentages come from the providers and include work done outside Garcon. They aren't calculated from Garcon's token counts or estimated API costs.

## Read the meters

A filled bar means allowance used. The vertical marker shows how much of that reset period has elapsed—an even-use reference, not a forecast. Each account follows its own reset time. Dates use your browser's time zone.

Missing data stays unknown. Snapshots older than ten minutes become stale; past reset deadlines are marked expired until the provider confirms a new window. Garcon never assumes a reset means zero usage. Named Claude limits use their own reported percentages.

## Use the widget

The widget groups rows by email, keeping providers separate. Press **R** to refresh. Extra widgets don't add provider polling.

Click an account email to give it a nickname. Nicknames are stored in local SQLite; older browser nicknames migrate on first use. Save a blank name to restore the email.

The footer shows newly completed requests, checking every two seconds. Reloading skips history; the total counter includes older records.

Codex reset credits appear with their expiration dates. Unknown counts aren't shown as zero, and expired credits stop counting as available. Garcon only reads credits; it never buys or redeems them. A failed credit lookup can retain older credit data while quota updates continue.

## Where accounts come from

Garcon scans at startup and every five minutes:

| Provider | Local sources |
| --- | --- |
| Codex | `~/.codex/auth.json`, `~/.codex-*/auth.json` |
| Claude | `~/.claude`, `~/.claude-*`; credential files or profile-specific macOS Keychain entries |
| Hermes | `~/.hermes/auth.json`, `~/.hermes-*/auth.json`; Codex provider and pool entries |

The service's `CODEX_HOME`, `CLAUDE_CONFIG_DIR`, and `HERMES_HOME` add custom directories. Variables set in another shell don't change an already-running service. Discovery doesn't search recursively. Codex keyring-only logins and API keys don't supply subscription meters.

Duplicate profiles for the same provider, person, and workspace share a snapshot. Distinct subscriptions remain separate, even with the same email. Quota discovery is broader than the [Codex routing pool](codex-routing.md); finding an account doesn't enroll it.

## Refresh and recovery

Codex quota comes from `/backend-api/wham/usage` on `chatgpt.com`; credits use `/backend-api/wham/rate-limit-reset-credits`. Claude uses `/api/oauth/profile` and `/api/oauth/usage` on `api.anthropic.com`.

Garcon sends tokens only to the fixed provider endpoints, with timeouts, response-size limits, and rate-limit backoff. SQLite stores the latest normalized snapshots, not tokens or quota history. A failed refresh preserves the previous values and their original timestamp.

Once a minute, Garcon checks idle Claude logins. Near expiry, an isolated Claude process renews its own credentials; empty input and a loopback relay block model traffic. Attempts have a 45-second timeout and back off after failure. This relies on Claude CLI behavior and may need adjustment after upgrades. Missing or revoked grants require signing in again. Codex pool renewal is described in the routing guide.

## API

- `GET /api/limits` returns cached snapshots immediately, plus `refreshing`, `updated_at`, and any error. Each window has `id`, `label`, nullable `used_percent`, `window_seconds`, `resets_at`, and `expired`.
- `POST /api/limits/refresh` requests a background refresh and returns 202. Refreshes coalesce, with a 30-second minimum interval and provider backoff. Requires local Host and matching Origin.

Timestamps are Unix milliseconds; zero means unknown where supported. Codex `reset_credits` has an independent status and fetch time, a nullable count, and expiration dates. Responses contain neither credentials nor redemption controls.
