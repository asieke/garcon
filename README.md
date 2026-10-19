# Garcon

See which account is doing the work.

Garcon connects your coding tools to their providers. It assigns Codex conversations to accounts you choose and shows requests, usage, and errors in one place.

Open the app at **http://127.0.0.1:4141**. It has four sections: **Accounts, Sessions, Analytics, and Logs**.

[Documentation](https://asieke.github.io/garcon/) · [Connect your tools](docs/connect.html) · [Routing details](docs/codex-routing.md)

These docs describe the current source. An installed npm release may lag behind unmerged changes.

## Scope

- Does: route Codex conversations across your enrolled accounts by quota, pin new conversations to an account, relay Claude Code, Pi and other compatible clients to their providers, import local Claude Code usage, and show requests, sessions, usage, limits and errors at http://127.0.0.1:4141.
- Does not: store prompts, response bodies or OAuth tokens, replay a failed request on another account, sync across devices, or authenticate dashboard users (keep the loopback binding).

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

Desktop's third-party inference mode manages its own endpoint and ignores the ordinary `ANTHROPIC_BASE_URL` setting. Garcon can optionally accept a local static key and forward Desktop's requests with the login of one Claude Code profile.

Create `~/.config/garcon/claude-gateway.json` with permissions `0600`, then restart the service:

```json
{
  "key": "your-local-gateway-key",
  "profile": "/absolute/path/to/your/claude-profile"
}
```

In Desktop's **Developer → Configure Third-Party Inference**, choose **Gateway**, **Static API key**, base URL `http://127.0.0.1:4141/claude-gateway`, your configured key, and auth scheme **Bearer**. The key belongs to Garcon; it is replaced with the configured profile's credential before forwarding to Anthropic. `x-api-key` is also supported. Credential loading and renewal use the same machinery as the CLI launcher. If that login is unavailable or expired, the request returns an error. To use a different account, change `profile` and restart the service. Garcon has no Claude account pool or pinning; those are Codex only.

The gateway is disabled when the configuration file is absent; remove that file and restart to disable it. Override its path with `-claude-gateway-config`. This route accepts only local application requests, even with `-allow-remote`; existing `/claude` requests keep their pass-through behavior. A simple key such as `test` can be used for a local trial. Model discovery, messages, token counting, and streaming are forwarded through normal usage logging.

This is an experimental subscription-authentication bridge, not a guarantee of compatibility with every Desktop release. Verify a real Desktop request in Logs. Desktop's third-party mode also changes feature availability, including SSH, cloud sessions, and Remote Control; see [Anthropic's Desktop gateway documentation](https://code.claude.com/docs/en/llm-gateway-connect#desktop-app).

## Who handles a request?

| Tool | Route |
| --- | --- |
| Codex | Your enrolled Codex accounts; no OpenRouter fallback |
| Pi | The Codex pool or OpenRouter, selected in Pi |
| Claude Code | The selected profile (`garcon claude` or the Desktop gateway's configured profile); no account pool |
| Other compatible clients | Their configured provider and credentials |

For a new Codex conversation, Garcon checks login health, quota, and model access. It automatically chooses the highest **remaining percentage ÷ hours until reset**. This compares percentages, not absolute token allowances.

In the Usage widget, **Up next** marks the account automatic routing would select for a new conversation (model access can change the choice). Click **Pin** on a Codex account to assign new Codex conversations to that account until you **Unpin**. The pin survives restarts and never silently falls back if the account is unavailable. Pinning and the account pool apply to Codex only; Claude and realtime voice routing are unchanged.

From a terminal, run `garcon codex status` to list accounts, `garcon codex pin ACCOUNT` (an account ID or email) to pin one, and `garcon codex unpin` to resume automatic selection for new conversations. Add `--json` for scripting.

Built-in Codex image generation and editing use the caller's ChatGPT login and image allowance. Garcon forwards their JSON or multipart payloads unchanged. These calls do not require a coding session ID and are outside the coding account pool and manual pin, like realtime voice.

Coding requests containing image inputs stay in the Codex pool. Garcon accepts request bodies up to 256 MiB, including embedded images, while preserving their session assignment and payload bytes. Upstream limits still apply.

A conversation always stays on its assigned account, regardless of later pin changes, even after a restart. If the account becomes unavailable, Garcon returns an error. Start a new conversation to choose another account. It never replays a failed request on a different account or redeems reset credits. An empty pool returns an error.

## Find the task behind a session

**Sessions** tracks Codex, Claude Code, and other clients separately, with client logos and local Codex task titles when available. Search by client, title, project, account, model, or ID. Identical IDs from different clients stay separate.

Click a session to drill into it. The session view shows:

- totals for model calls, tokens, tool calls, subagents, and wall-clock time;
- an activity strip of every call over time, one lane per agent;
- the models used, with reasoning effort;
- how often each tool was called;
- the subagents (spawned agents, reviews, compaction) with their models and calls;
- a timeline grouped by turn. Each call shows its time from the session start, model and effort, tokens, duration, status, and the tools it asked for.

Click a call to inspect it. Click a subagent to see only its calls. **Open in Logs** keeps the filtered request list.

Garcon records only tool names and counts. It never saves tool arguments, tool output, prompts, or responses. Codex calls through Garcon carry turn, subagent, and reasoning details. Claude Code calls carry tool names, and local transcripts also mark subagent (sidechain) calls. Calls recorded before this feature have no tool details.

In-flight sessions appear first, with elapsed time. Background reviews have their own activity label and don't replace the conversation model. Missing task metadata falls back to the ID.

**No model request** means just that. The task might still be running tools or waiting for you. Garcon doesn't track every action inside Codex.

Claude Code can connect directly to Anthropic and still appear in **Analytics** and **Sessions**. Garcon imports usage metadata from local Claude transcript files on startup and checks for new entries every 15 seconds. It discovers the default profile, named `.claude-*` profiles, `CLAUDE_CONFIG_DIR`, and profiles in its launcher registry, including local Desktop and subagent transcripts where available. Remote or cloud-only history is not collected.

Imported records are labelled **Local history** and stay out of the live ticker. They include model, session, timestamp, and input/output/cache tokens; project folders identify sessions without copying conversation text. Account labels are inferred from the profile metadata at import time, so a historical login may be unknown or different. Network status, latency, and live activity are not inferred from transcripts. Provider quota bars continue to use provider usage snapshots independently of imports.

Repeated transcript blocks and copied messages count once. Provider request/message IDs reconcile imported and proxied records in either arrival order; older proxy rows without these IDs are reconciled only when a unique session/model/token match falls within the request interval. Ambiguous legacy overlap cannot be conclusively deduplicated. Import progress and unreadable/malformed-file counts appear in Analytics and Sessions and at `/api/imports/claude`. Start the service with `-claude-import=false` to disable collection; existing imported history is retained.

The ticker and request inspector also show task names. **Analytics** shows tokens, cache use, and estimated API-equivalent cost—not your subscription bill. **Logs** includes failures and interrupted streams.

## Limits and local data

Optional [telemetry blocking](docs/telemetry-blocking.md) blocks selected hosts for clients routed through Garcon and records blocked attempts in Logs. It is off by default, supports custom hosts and allow overrides, and never decrypts HTTPS. Separate client telemetry needs the documented HTTP proxy settings; a model base URL alone does not cover it.

Open [the compact widget](http://127.0.0.1:4141/usage-widget/) for Codex, Claude, Antigravity (AGY) and Muse allowances, reset times, and Codex reset credits. Press **R** to refresh. Provider limits include work done outside Garcon; missing data isn't counted as unused quota.

Muse keeps its subscription usage in memory, learns it only from a model call, and writes none of it to disk, so Garcon reads it with a probe: once an hour (and on **R**, at most every five minutes) it sends one minimal prompt from a memory-only `muse serve` session in its own cache folder, which never appears in your Muse history. Each probe spends a little Muse allowance (about 26k input tokens, mostly Muse's own system prompt). Between probes the widget shows the last reading, marked "as of" its time, and doesn't count it as needing attention. Garcon looks for `muse` in `MUSE_BIN`, `~/.local/bin`, `~/.muse/bin`, Homebrew and `PATH`. Muse sessions are imported from `~/.muse/sessions`, `~/.local/share/muse/sessions` (or `MUSE_HOME`), including subagent logs, which count toward their top-level session.

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

See [Contributing](CONTRIBUTING.md) for the repository layout, review workflow, and publication checks.

Source builds need Go 1.27+ and Node 22+; versions are in `.mise.toml`.

```sh
bash scripts/dev.sh              # build the dashboard and serve it on 4242; API comes from 4141
```

Production stays on **4141**; development uses **4242**. Keep your tools pointed at 4141. The development dashboard uses real local data, so dashboard edits affect your settings. Set `GARCON_BACKEND_URL` to choose a different backend.

Run `scripts/check.sh` before pushing; it includes `go test ./...` and, from `web/`, `npm run check && npm test && npm run build`. Rebuild and install explicitly to update production; the development dashboard doesn't update the installed binary.

### Architecture

One Go binary (`cmd/garcon`, packages in `internal/`) serves the proxy, the JSON API and the dashboard on port 4141. The dashboard is a SvelteKit app in `web/`, built into `internal/dashboard/build` and embedded in the binary. Request metadata, account assignments, preferences and snapshots live in SQLite at `~/.local/share/garcon/usage.db`. The npm package `ai-garcon` (`npm/ai-garcon/`) ships prebuilt binaries for linux and darwin on x64 and arm64. `macos/` holds the optional native usage widget.

Source: `web/` (UI), `internal/` (backend), `docs/` (documentation).

[Product acceptance and verification](docs/acceptance-checks.md) defines concrete routing, usage, ticker, widget, privacy, and release checks, including what requires live end-to-end evidence.

Merges to `main` trigger an npm patch release. `[minor]` or `[major]` in the merge message changes the bump. PRs build and test without publishing.

### External CLI delegation (experimental)

`garcon delegate` lets a local Codex task request a bounded, opt-in task from the
installed Claude Code CLI. It does not replace Codex's native model backend or use
Garcon's account pools. See [architecture, auth boundaries, setup and validation](docs/codex-cli-delegation.md).

## Test

- `scripts/check.sh` first runs the standard gate (`scripts/gate.sh`: merge markers, private keys, files over 25 MB, and a gitleaks scan of commits not yet on `origin/main`), then the repository-boundary check, the dashboard check, tests and build, `go vet ./...`, `go test ./...`, the npm package tests and `scripts/release.sh 9.9.9 --dry-run` without repeating the tests (the four cross-builds and a dry-run pack). There is no CI: `scripts/check.sh --report` posts the `check` status a PR needs before merging, and `scripts/verify-pr.sh` followed by a PR number tests someone else's PR on this machine and posts its `check` status; `scripts/verify-pr.sh --no-report` followed by the PR number tests it and posts nothing (for reviews). On `main` the Release workflow runs the real build.
- Tests need no `.env` file and no real accounts.
- Smoke test after deploy: `curl -fsS http://127.0.0.1:4141/api/usage`

## Deploy

Every merge into `main` publishes a new `ai-garcon` release to npm (see [Work on Garcon](#work-on-garcon)). npm installs pick it up with `garcon update`.

The public site at https://asieke.github.io/garcon/ is GitHub Pages serving `docs/` from `main`. GitHub rebuilds it on every push, so edits to the `.html` pages in `docs/` go live when they merge; there is no separate deploy step.

The owner's live install runs on their Mac (the host in `deploy` in `PROJECT.md`) from the live checkout `~/apps/garcon`, which always stays on `main`. It serves port 4141 as the LaunchAgent `dev.garcon` and keeps data in `~/.local/share/garcon`. Finish active requests before deploying: the service restarts.

- **Secrets:** none in the repository or in `.env`. Settings live in `~/.config/garcon`, provider CLIs own their logins, and a saved OpenRouter key lives in a separate owner-only file. Agents never edit any of them.
- **Live install (npm release):** `scripts/deploy.sh` fast-forwards `~/apps/garcon` to `origin/main` and runs `scripts/deploy-macos.sh`, which you can also run from any checkout on a Mac. It reads the `v*` tag the Release workflow puts on the head of `origin/main`, installs that exact `ai-garcon` version from npm (no Go toolchain needed), links `~/.local/bin/garcon` to the npm install so an older source build cannot shadow it, refreshes the LaunchAgent `dev.garcon` and checks that the running service reports the new version. If the release is still publishing it stops and says so, and the next sync installs it, because `scripts/deploy.sh` treats a service that is not on the release of `origin/main` as a pending deploy. `scripts/deploy.sh` refuses if `~/apps/garcon` is not a clean checkout of `main` or has commits not on `origin/main`, so it never discards work; fix those through a pull request. Both are safe to run repeatedly; `--force` reinstalls when already up to date. First install: `git clone https://github.com/asieke/garcon ~/apps/garcon && ~/apps/garcon/scripts/sync.sh`.
- **Linux (source build):** no Linux host is live. To run Garcon on Linux from source, use `scripts/install.sh --service` (dashboard and binary build, install, service restart).
- **Sync first:** `scripts/sync.sh` runs `scripts/deploy.sh` when called from the live checkout (`~/apps/garcon`) and stops with `REFUSED` if deploy refuses; in any other checkout it only reports how far the branch is behind `origin/main`. Run it before running, restarting, or deploying the app, using its live data, or running a job, and on the Mac after merging a pull request. Unless it refuses, it then removes worktrees whose pull request merged. Run from a worktree, it removes them too, but does not deploy.
- **Session start:** Claude Code runs `scripts/sync.sh --inspect`, and every other agent runs it first. It fetches and reports the live checkout's state and what a full sync would do, but deploys nothing, so a session that only reviews or inspects never touches the app. A full sync that refuses blocks live-app operations and jobs, not reviews.
- **Scheduled jobs:** none today. Any future cron entry runs through `scripts/sync.sh`, which skips the job if sync refuses:
  `SCHEDULE ~/apps/garcon/scripts/sync.sh COMMAND >> ~/apps/garcon.cron.log 2>&1`
- **Branch work:** Every task runs in its own worktree: for example, `scripts/worktree.sh feat/12-add-login` creates `.claude/worktrees/feat-12-add-login` in the checkout you run it from, branched from `origin/main`. Never edit `~/apps/garcon` directly; changes reach the live app only by merging a pull request to `main`. The app and anything that watches its folder ignore `.claude/worktrees/`.
