---
name: garcon-add-claude
description: Add a Claude OAuth account to Garcon.
---
On the Garcon host (SSH if remote), run `garcon accounts login claude --profile NEW_NAME` with an unused profile name. Let me complete sign-in. Run `garcon accounts refresh`, then `garcon accounts list` to verify the Claude account; if absent, diagnose discovery instead of repeating sign-in. Use the server's `--data` path if non-default. Preserve existing logins and keep tokens out of chat. Connect using `garcon claude --config-dir "$HOME/.claude-garcon-NEW_NAME"`. Claude accounts do not join the Codex pool. Never enable browser writes.
