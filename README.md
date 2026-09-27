# Garcon

See which account is doing the work.

Garcon connects your coding tools to their providers. It assigns Codex conversations to accounts you choose and shows requests, usage, and errors in one place.

Open the app at **http://127.0.0.1:4141**. It has four sections: **Accounts, Sessions, Analytics, and Logs**.

[Documentation](https://asieke.github.io/garcon/) · [Connect your tools](docs/connect.html) · [Routing details](docs/codex-routing.md)

These docs describe the current source. An installed npm release may lag behind unmerged changes.

## Get started

```sh
npm install -g ai-garcon@latest
garcon setup
```

Requires macOS or Linux, x64 or arm64, and Node 18+ for the npm command. Run setup as your normal user. It installs a background service and keeps your existing usage history.

1. Open **Accounts → Add account**. Follow the terminal sign-in instructions for Codex or Claude Code using the corresponding CLI.
2. Review the Codex account pool. Existing logins are selected on first setup; later logins can be added with **In pool**.
3. Open the connection button on the Codex or Claude Code table and copy its setup prompt into a coding agent. Restart the client, send a request, and check **Logs**. See [connection recipes](docs/connect.html) for other clients.

The sign-in instructions create a separate profile. Adding a login doesn't change your client configuration or switch Claude Code's selected profile.

## Who handles a request?

| Tool | Route |
| --- | --- |
| Codex | Your enrolled Codex accounts; no OpenRouter fallback |
| Pi | The Codex pool or OpenRouter, selected in Pi |
| Claude Code | The Claude profile used by `garcon claude` |
| Other compatible clients | Their configured provider and credentials |

For a new Codex conversation, Garcon checks login health, quota, and model access. It automatically chooses the highest **remaining percentage ÷ hours until reset**. This compares percentages, not absolute token allowances.

Once assigned, a conversation stays on that account, even after a restart. If the account becomes unavailable, Garcon returns an error. Start a new conversation to choose another account. It never replays a failed request on a different account or redeems reset credits. An empty pool returns an error.

## Find the task behind a session

**Sessions** tracks Codex, Claude Code, and other clients separately, with client logos and local Codex task titles when available. Search by client, title, project, account, model, or ID. Click a session for that client's request history; identical IDs from different clients stay separate.

In-flight sessions appear first, with elapsed time. Background reviews have their own activity label and don't replace the conversation model. Missing task metadata falls back to the ID.

**No model request** means just that. The task might still be running tools or waiting for you. Garcon doesn't track every action inside Codex.

The ticker and request inspector also show task names. **Analytics** shows tokens, cache use, and estimated API-equivalent cost—not your subscription bill. **Logs** includes failures and interrupted streams.

## Limits and local data

Open [the compact widget](http://127.0.0.1:4141/usage-widget/) for Codex and Claude allowances, reset times, and Codex reset credits. Press **R** to refresh. Provider limits include work done outside Garcon; missing data isn't counted as unused quota.

Garcon stores request metadata, account assignments, preferences, and snapshots in `~/.local/share/garcon/usage.db`. It doesn't save prompts, response bodies, or OAuth tokens in the ledger. Provider CLIs own their logins. A saved OpenRouter key lives in a separate owner-only file.

Task titles and paths are read from Codex's local index without changing it. Data stays on this machine; there is no cross-device sync. Garcon has no app login, so keep its default loopback binding. `-allow-remote` exposes the dashboard and relay to the network.

## Update or troubleshoot

```sh
garcon update                    # npm installations
garcon doctor --wait 10s
garcon service status
```

Finish active requests before updating: the service restarts. Source installs use `scripts/install.sh --service`; `--update` pulls and rebuilds.

Logs: `~/Library/Logs/garcon.log` on macOS; `journalctl --user -u garcon` on Linux. See [installation help](docs/install.html) for missing commands or permission errors.

To remove it, restore your tools' original provider URLs first, then run `garcon service uninstall` and `npm uninstall -g ai-garcon`. Usage and settings remain.

## Work on Garcon

Source builds need Go 1.27+ and Node 22+; versions are in `.mise.toml`.

```sh
bash scripts/dev.sh              # build and run on 4242
cd web && npm run dev:frontend    # hot reload; API comes from 4141
```

Production stays on **4141**; development uses **4242**. Keep your tools pointed at 4141. Both development modes use real local data, so dashboard edits affect your settings. Set `GARCON_BACKEND_URL` to choose a different backend for frontend work.

Run `go test ./...` and, from `web/`, `npm run check && npm test && npm run build`. Rebuild and install explicitly to update production; a frontend dev server doesn't update the installed binary.

Source: `web/` (UI), `internal/` (backend), `docs/` (documentation).

Merges to `main` trigger an npm patch release. `[minor]` or `[major]` in the merge message changes the bump. PRs build and test without publishing.
