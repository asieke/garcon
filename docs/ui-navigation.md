# The local routing desk

Garcon has four primary destinations. The main navigation is a left rail on
larger windows and an icon rail with accessible labels on smaller windows.
`?view=accounts|sessions|analytics|logs` links directly to a destination. Previous
`usage`, `cost`, and `models` links open the corresponding Analytics tab.

## Accounts

The home view contains one expandable Codex provider row. It shows discovered
local OAuth profiles, subscription plans, remaining quota, reset countdowns,
routing scores, and eligibility. Duplicated profiles for the same account appear
once. Each account can be enrolled or removed individually.

Lower priority numbers are considered first. The arrows reorder whole groups;
the account's P1/P2/etc. selector moves it between groups. Within a group, the
highest remaining-percentage / hours-to-reset score wins. The lowest score among
an account's general quota windows is used. Existing sessions keep their account.

Add account opens a keyboard-accessible dialog with a named Codex profile and a
copyable login command. Refresh discovers completed logins and updates quota.
Connect Codex provides the stable loopback endpoint and configuration snippet.

## Sessions

This table uses persisted assignments, not an inactivity-based guess. It shows
the session identifier, account, model, request count, active requests, and last
activity. Search filters by session, account, or model. A row opens its requests
in Logs. Older hashed-only assignments are marked as legacy; readable IDs and
activity become available when those sessions resume.

## Logs

Requests are searchable and paginated. Every proxy request is recorded, including
metadata calls, routing failures, upstream errors, and interrupted responses.
The request inspector exposes identity, model, timing, token counts, and a
copyable metadata record. Prompt and response content and credentials are never
persisted. System diagnostics have their own paginated tab.

## Analytics

Usage, Cost, and Models are tabs in one section. Period filters apply to SQLite
aggregates. Cost is an API-equivalent estimate, with missing prices disclosed;
it does not represent a ChatGPT subscription bill. Charts, model rankings, and
account token shares all use observed requests, without fabricated demo values.

## Live activity

The fixed bottom ticker displays model → account and in-flight/completed status.
It can be paused, pauses on hover or keyboard focus, and respects reduced motion.
Selecting an item opens the request inspector. HTTP polling reconnects without
resetting the selected view or current filters.

All persistent application data is local in SQLite. The existing compact usage
widget remains available at `/usage-widget/`; its nicknames migrate from browser
storage into the database on first use.
