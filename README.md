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

### Optional Claude Desktop gateway (experimental)

Desktop's third-party inference mode manages its own endpoint and ignores the ordinary `ANTHROPIC_BASE_URL` setting. Garcon can optionally accept a local static key and automatically select a Claude Code account for each new session.

Create `~/.config/garcon/claude-gateway.json` with permissions `0600`, then restart the service:

```json
{
  "key": "your-local-gateway-key",
  "profile": "/absolute/path/to/your/claude-profile"
}
```

In Desktop's **Developer → Configure Third-Party Inference**, choose **Gateway**, **Static API key**, base URL `http://127.0.0.1:4141/claude-gateway`, your configured key, and auth scheme **Bearer**. The key belongs to Garcon; it is replaced with the session's selected account credential before forwarding to Anthropic. `x-api-key` is also supported. Credential loading and renewal use the same machinery as the CLI launcher. An unavailable pinned account returns an error rather than moving the session or replaying its request. The profile setting is retained for compatibility and includes that directory in discovery; it no longer forces every gateway session to that account.

The gateway is disabled when the configuration file is absent; remove that file and restart to disable it. Override its path with `-claude-gateway-config`. This route accepts only local application requests, even with `-allow-remote`; existing `/claude` requests keep their pass-through behavior. A simple key such as `test` can be used for a local trial. Model discovery, messages, token counting, and streaming are forwarded through normal usage logging.

New gateway sessions use the same quota-per-hour formula as Codex: the most constrained applicable window sets the score. General and applicable model/OAuth-app limits are respected; stale, unknown, exhausted, or expired quota is ineligible. An explicitly unused Claude five-hour window with no reset is conservatively scored over its full five-hour duration. Model access and account identity are verified with Anthropic. The initial discovered account pool is saved once; subsequent new logins are not automatically enrolled.

Requests use Claude's session header or metadata session ID. Assignments live in a separate SQLite table and survive restarts. Existing sessions in Garcon's request history retain their original account. Unknown continuations and completion requests without a stable session ID are rejected. Model discovery and sessionless token counting do not create assignments. View scores and pool membership in Accounts or the read-only `/api/routing/claude` endpoint. Ordinary `/claude` traffic and profile launchers retain their selected login.

This is an experimental subscription-authentication bridge, not a guarantee of compatibility with every Desktop release. Verify a real Desktop request in Logs. Desktop's third-party mode also changes feature availability, including SSH, cloud sessions, and Remote Control; see [Anthropic's Desktop gateway documentation](https://code.claude.com/docs/en/llm-gateway-connect#desktop-app).

## Who handles a request?

| Tool | Route |
| --- | --- |
| Codex | Your enrolled Codex accounts; no OpenRouter fallback |
| Pi | The Codex pool or OpenRouter, selected in Pi |
| Claude Code | Gateway: quota-based selection pinned per session; `garcon claude`: selected profile |
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

On macOS, the small [Swift app](macos/usage-widget.swift) opens that same widget in a native window. Build it with Apple's Command Line Tools installed:

```sh
bash macos/build.sh
open "macos/build/Garcon Usage.app"
```

Garcon must already be running on port 4141. The app loads the page directly from Garcon; it does not bundle a separate server or dashboard. You can move the built app to Applications. **⌘R** reloads, **⌘W** closes, and **⌘Q** quits.

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
