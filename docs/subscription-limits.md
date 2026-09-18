# Subscription limits

Open **Limits** for current Codex and Claude subscription allowances. Overview shows
each account's weekly allowance, or its first available limit. These snapshots come
from providers and include usage outside Garcon; token counts and estimated API costs
cannot be converted to a subscription percentage.

The filled bar is allowance consumed on a fixed 0–100% scale. The vertical marker is
elapsed time in that limit's reset period: an even-use reference, not a forecast.
Each account follows its own reset time, rather than a calendar week. Reset dates use
the browser's time zone. Fable uses its own reported percentage, not a multiplier
applied to overall Claude usage. Missing windows are not interpreted as zero usage.

## Compact widget

Open `/usage-widget` (or follow **Compact widget** on Limits) for a standalone view
without dashboard navigation or filters. Colored edge stripes group the provider
rows; the account color key at the bottom identifies each email and shows its Codex
reset credits. Codex and Claude still retain separate allowances. Rows share the
available viewport height: compact laptop windows fit all six accounts, taller
windows use more generous spacing, and smaller windows scroll once rows reach
their readable minimum. The widget also adapts to phone widths and follows the
system light/dark color scheme.
Click an email in the account key to give it a nickname. Nicknames are saved only
in this browser's local storage for the current site, with no cloud sync. Save a
blank nickname to restore the email; hovering a nickname still shows the email.
Press **R** or the refresh button to request a refresh. It reads the same cached
snapshots as Limits, so opening another widget does not add provider polling.

The fixed footer starts with an empty lane and scrolls only new LLM completion rows
recorded locally or received through sync after the widget opens. Each chip shows
account, provider, total tokens, and machine name. Existing history is skipped,
including when reloading the widget. It checks for arrivals every two seconds;
remote requests appear after sync delivers them, in arrival order even if their
original request timestamp is older. Repeated sync rows are deduplicated.
Token totals include input, cache-read, cache-write, and output tokens;
they appear once a request finishes. Requests scroll in arrival order, once each.
New chips enter from the right and scroll across the full lane. When the queue drains,
the footer remains visible with an empty lane. The fixed **Total requests** chip on
the right counts all recorded local and synced requests, including historical rows.
Hovering does not interrupt scrolling. Reduced-motion mode
uses a manually scrollable strip whose new chips clear after twelve seconds.

## Reset credits

Limits, Overview, and the widget display remaining Codex reset credits and their
individual expiration dates. Collection uses a read-only GET to
`https://chatgpt.com/backend-api/wham/rate-limit-reset-credits` with the same account
token and workspace header, once per account per refresh. This is separate from
purchased usage balances. Garcon never redeems or purchases credits.

Dates use the browser's local time zone; hover an expiration for its exact time.
Known expired credits stop counting as available, and the snapshot is marked stale
until refreshed. Unknown counts and expirations are labeled unavailable, never zero.
A failed credit lookup retains its last snapshot without hiding current quota meters.
Reset credits are a Codex-only feature; Claude accounts have no reset-credit UI.

## Automatic discovery

The service reads these local sources at startup and every five minutes:

- Codex: `~/.codex/auth.json` and `~/.codex-*/auth.json` containing ChatGPT logins.
- Claude: `~/.claude` and `~/.claude-*`; `.credentials.json` when present, otherwise
  the directory-specific macOS Keychain entry. The default directory uses
  `Claude Code-credentials`; named directories use that name plus the first eight
  hexadecimal characters of the SHA-256 of their absolute directory path. A named
  profile never borrows the default profile's login.
- Hermes: `~/.hermes/auth.json` and `~/.hermes-*/auth.json`, including the
  `providers.openai-codex.tokens` login and `credential_pool.openai-codex` entries.

The service's `CODEX_HOME`, `CLAUDE_CONFIG_DIR`, and `HERMES_HOME`, when set, add
custom directories. Environment variables set only in an unrelated shell are not
visible to an already running service. Discovery does not recursively search the
filesystem. Codex logins stored only in its OS keyring are not read in this version;
the file-backed profiles configured on this machine are supported.

The quota key combines provider, person, and workspace/organization identifiers;
email is the display label. Duplicate profiles and harnesses are deduplicated before
usage polling. Distinct subscriptions under the same email remain separate and show
their workspace as a subtitle. API-key accounts do not have subscription meters.

## Refresh and privacy

Codex snapshots use `https://chatgpt.com/backend-api/wham/usage`, with the selected
ChatGPT workspace header. Claude identity uses `/api/oauth/profile` and allowances
use `/api/oauth/usage` on `https://api.anthropic.com`, with the OAuth beta header.
Claude's named `limits` entries take precedence over matching legacy windows.
These provider endpoints can change; parsing and network failures retain the last
good values with a stale status. They are isolated from completion proxying.

Garcon never renews, writes, copies, or synchronizes provider credentials, launches
a model request, or consumes reset credits to obtain limits. It reads access tokens
for a refresh and sends them only to fixed provider HTTPS endpoints. Requests have
timeouts and response-size limits, refuse redirects, and honor rate-limit backoff.
Only credential digests and resolved identities are cached between refreshes.

`limits.json`, beside `usage.jsonl`, contains only the latest normalized snapshots
and is written atomically with owner-only permissions. There is no history or
Supabase quota table. A failed refresh preserves the previous update time and values.
Snapshots older than ten minutes are marked stale. Once a reset deadline passes,
the previous usage is shown as expired until the provider confirms a new window;
Garcon never assumes it has reset to zero. Missing or expired logins require opening
the appropriate CLI to refresh its login, after which collection recovers automatically.

## Local API

- `GET /api/limits`: cached account snapshots plus collection status; never blocks
  on provider calls. Windows include `id`, `label`, nullable `used_percent`,
  `window_seconds`, `resets_at` (Unix milliseconds, zero if unknown), and `expired`.
- `POST /api/limits/refresh`: requests a background refresh and returns HTTP 202
  with the current snapshot. Concurrent requests coalesce, manual refreshes have a
  30-second minimum interval, and provider backoff still applies. Requires a local
  Host and rejects a different Origin.

Responses and caches contain no access tokens, refresh tokens, or login file paths.
Codex accounts also include `reset_credits`: nullable `available_count`, a list of
available credit `expires_at` timestamps (Unix milliseconds; zero if not reported),
and independent `status`, `fetched_at`, and optional `error` fields. Claude accounts
omit this field. Credit identifiers and redemption endpoints are not exposed.
Limits and the Overview summary are independent of request-history, harness, and
device filters, and work even when Garcon has not recorded a completion.
