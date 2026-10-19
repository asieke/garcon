# Subscription limits

Codex's routing card shows available quota and reset times. Open **http://127.0.0.1:4141/usage-widget/** for the compact Codex and Claude allowance view.

These percentages come from the providers and include work done outside Garcon. They aren't calculated from Garcon's token counts or estimated API costs.

## Read the meters

A filled bar means allowance used. The vertical marker shows how much of that reset period has elapsed—an even-use reference, not a forecast. Each account follows its own reset time. Dates use your browser's time zone.

Missing data stays unknown. Snapshots older than ten minutes become stale; past reset deadlines are marked expired until the provider confirms a new window. Garcon never assumes a reset means zero usage. Named Claude limits use their own reported percentages.

## Use the widget

The widget has a section per provider that has a login (Codex, Claude, Google, Facebook) and a block per account: the account name with its status (and a Pin control on Codex accounts), one bar for the weekly allowance (percentage used, a marker for how much of the week has passed, and the time to reset), and a line of text for everything else: the 5-hour window, model-scoped limits such as Fable or AGY's Claude & GPT pool, Codex reset credits, and the time of Muse's last reading (Garcon probes Muse hourly with one minimal prompt, since Muse reports usage only after a model call). A secondary limit at 80% or more turns the warning color, since it can block work while the weekly bar looks fine. Blocks share the window's height above the request ticker and scroll only when it is too short; wide windows put each account on one line. Press **R** to refresh. Extra widgets don't add provider polling.

Click an account email to give it a nickname. Nicknames are stored in local SQLite; older browser nicknames migrate on first use. Save a blank name to restore the email.

The footer shows newly completed requests, checking every two seconds. Reloading skips history; the total counter includes older records.

Codex reset credits appear with their expiration dates. Unknown counts aren't shown as zero, and expired credits stop counting as available. Garcon only reads credits; it never buys or redeems them. A failed credit lookup can retain older credit data while quota updates continue.

Claude saved resets appear on the same text line with their count, expiration date and covered limits, for example **1 reset Oct 22 · 5h + week + included models**. Each grant stays separate when it covers different limits or expires on a different day. Expired, paused or not-yet-started grants are excluded. A reset that requires reaching a limit still counts as saved. Missing expiration or coverage stays unknown; missing or ineligible offers show nothing and do not require attention. Garcon never claims a Claude reset; use Claude's **Settings → Usage → Resets** to spend one.

## Where accounts come from

Garcon scans at startup and every five minutes:

| Provider | Local sources |
| --- | --- |
| Codex | `~/.codex/auth.json`, `~/.codex-*/auth.json` |
| Claude | `~/.claude`, `~/.claude-*`; credential files or profile-specific macOS Keychain entries |
| Hermes | `~/.hermes/auth.json`, `~/.hermes-*/auth.json`; Codex provider and pool entries |

The service's `CODEX_HOME`, `CLAUDE_CONFIG_DIR`, and `HERMES_HOME` add custom directories. Variables set in another shell don't change an already-running service. Discovery doesn't search recursively. Codex keyring-only logins and API keys don't supply subscription meters.

Duplicate profiles for the same provider, person, and workspace share a snapshot. Distinct subscriptions remain separate, even with the same email. Quota discovery is broader than the [Codex routing pool](codex-routing.md); finding an account doesn't enroll it.

For persistent access to several Codex accounts, use independent file-backed CLI
profiles such as `~/.codex-personal`, `~/.codex-dev`, and `~/.codex-work`. Leave
`~/.codex` for the desktop app's current login. A shell alias using `~/.codex`
follows that desktop login; its name does not pin an account. Authenticate each
profile independently instead of copying or sharing `auth.json` files.

When the desktop signs out of an account's only login, its cached limits become
stale. Sign into its independent profile and refresh Limits to recover. Desktop
and CLI profiles for the same account still produce one quota card and one poll.
Independent directories prevent overwrites; provider sessions can still expire.

## Refresh and recovery

Codex quota comes from `/backend-api/wham/usage` on `chatgpt.com`; credits use `/backend-api/wham/rate-limit-reset-credits`. Claude uses `/api/oauth/profile` and `/api/oauth/usage` on `api.anthropic.com`.

Claude's existing usage GET adds `?cedar_ember=1&skip_spend=1` to include saved resets; the normal limit windows remain in that response. There is no second reset request, so resets share the five-minute refresh and usage backoff. `skip_spend` omits spending data that Garcon does not use. The optional `cedar_ember` block is an unpublished, unstable API: grants report `resets_left`, `starts_at`, `ends_at` and `clears`. To receive it, this one read sends Claude Code compatibility headers (`x-app: cli`, a random session ID, JSON content type and `claude-cli/2.1.303 (external, cli)` as User-Agent). The version is the protocol checked against a live response, not a claim about the installed CLI. Provider changes can remove access. A malformed optional block retains dated reset data without breaking quota windows; an absent or ineligible block clears old offers. The separate `/limit-reset` experiment is not polled or displayed.

Garcon sends tokens only to the fixed provider endpoints, with timeouts, response-size limits, and rate-limit backoff. SQLite stores the latest normalized snapshots, not tokens or quota history. A failed refresh preserves the previous values and their original timestamp.

Once a minute, Garcon checks idle Claude logins. Near expiry, an isolated Claude process renews its own credentials; empty input and a loopback relay block model traffic. Attempts have a 45-second timeout and back off after failure. This relies on Claude CLI behavior and may need adjustment after upgrades. Missing or revoked grants require signing in again. Codex logins are also renewed every minute when near expiry, including accounts outside the inference pool. This keeps usage-only accounts refreshing without enrolling them. The routing guide describes the renewal flow.

## API

- `GET /api/limits` returns cached snapshots immediately, plus `refreshing`, `updated_at`, and any error. Each window has `id`, `label`, nullable `used_percent`, `window_seconds`, `resets_at`, and `expired`.
- `POST /api/limits/refresh` requests a background refresh and returns 202. Refreshes coalesce, with a 30-second minimum interval and provider backoff. Requires local Host and matching Origin.

Timestamps are Unix milliseconds; zero means unknown where supported. `reset_credits` has a status and fetch time, a nullable count, and expiration dates. Claude entries additionally carry `count` (resets in that grant) and `limits` (provider window names it covers); an omitted count means one for existing Codex credits. Grant IDs and experiment metadata are not stored. Responses contain neither credentials nor redemption controls.
