---
name: garcon-add-codex
description: Add a Codex OAuth account to Garcon.
---
On the Garcon host (SSH if remote), run `garcon accounts login codex --profile NEW_NAME` with an unused profile name. Let me complete sign-in. Run `garcon accounts refresh`, then `garcon accounts list` to verify the account; if absent, diagnose discovery instead of repeating sign-in. Use the server's `--data` path if non-default. Preserve existing logins. Keep tokens out of chat. If I request enrollment, read `garcon routing show` and pipe updated JSON to `garcon routing set --json-stdin`, preserving other accounts and priorities. Never enable browser writes.
