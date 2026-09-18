# Plan: fold Claude Code Remote Control into Garcon (`garcon claude`)

> Historical design record. Account routing and flags below are superseded by
> [automatic account detection](../docs/account-detection.md); use account-free URLs.

Status: implemented on branch `claude-remote-control` (2026-09-14) as `internal/claude`, following
`claude-remote-control.md`. Verified on Linux against the 0.1.3 service; macOS items below remain
unverified.

## Reviewed against the 0.1.3 hardening (PR #17)

`internal/proxy` did not change, so routes, header pass-through and usage recording are as assumed.
What did change and how the implementation honours it:

- `local.Guard` now wraps every route (`cmd/garcon/main.go:128`): a request whose `Host` is not
  loopback gets 403. The relay therefore uses `httputil.ReverseProxy.Rewrite` with `SetURL`, which
  clears the outbound Host so the transport sends `127.0.0.1:4141`. A `Director`-style proxy would
  forward whatever Host Claude Code sent over the socket and be refused; `TestRelay` sends
  `Host: api.anthropic.com` through the real guard to keep it that way.
- Garcon's `IdleTimeout` is two minutes; the relay's transport closes idle connections at 90 s.
- Garcon added no authentication of its own; the relay needs no Garcon credential.
- The hardened unit sets `MemoryDenyWriteExecute=yes`, which Node inherits and dies under. That is a
  second reason the "daemon inside the service" alternative stays rejected: the renewal child runs
  `claude`, and `garcon claude` runs in the user's shell, never under the unit.

## Context

Remote Control (the Claude Code session showing up in the claude.ai iOS app) was restored
on one machine by moving socket-serving, credential injection and login renewal into
`~/.config/agent-profiles/launch.py` — a Python launcher specific to that machine, outside
Garcon. It works and is verified, but every other Garcon install (other synced devices,
any other `ai-garcon` user) still only has the plain `ANTHROPIC_BASE_URL` snippet and no
Remote Control. This plan folds the same mechanism into the `garcon` binary as a portable
subcommand, so any install gets it with no bespoke launcher.

Carried over unchanged: the canonical `/<harness>/<account>/<provider>/` proxy route, no
loss of usage recording, and Garcon still stores no long-lived credentials — it reads
Claude Code's own credentials file/keychain item live, at the moment it needs one.

## Why this works (verified 2026-09-13/14, live byte capture)

- Remote Control's only objection is an `ANTHROPIC_BASE_URL` host other than
  `api.anthropic.com`. `ANTHROPIC_UNIX_SOCKET` (Claude Code's `claude ssh` transport)
  bypasses that specific check, so `ANTHROPIC_BASE_URL` stays pointed at Garcon's route
  and traffic keeps flowing and recording exactly as today.
- Under the socket, Claude Code treats `CLAUDE_CODE_OAUTH_TOKEN` + `CLAUDE_CODE_OAUTH_SCOPES`
  as the login. It never refreshes an env-supplied token itself, and re-reads
  `<config dir>/.credentials.json` on its next 401.
- Under the socket, Claude Code also polls `GET /api/claude_code/policy_limits` with no
  route prefix and no bearer, expecting the socket's owner to add both. That call must
  succeed or org policy resolves to "deny" and Remote Control is refused.

## Design

### New subcommand

```
garcon claude --account <email> [--config-dir <path>] [--garcon <host:port>] [-- <claude args...>]
```

- `--config-dir` defaults to `$CLAUDE_CONFIG_DIR` or `~/.claude`; `--account` is required
  (the same URL segment Garcon already records usage under); `--garcon` defaults to
  `127.0.0.1:4141`.
- Each invocation is self-contained: its own socket, its own renewal loop, no new
  persistent service and no account registry. Steps:
  1. Read `<config-dir>/.credentials.json` → `claudeAiOauth` (Linux/Windows). On macOS,
     read the `Claude Code-credentials` Keychain item via `security find-generic-password
     -s "Claude Code-credentials" -w` — **unverified, test on macOS**.
  2. Bind a unix socket (e.g. `<runtime dir>/garcon-claude-<pid>.sock`), removed on exit.
  3. Serve it with an `httputil.ReverseProxy` targeting `--garcon`, reusing the pattern in
     `internal/proxy`: if a request path is not already `/claude/<account>/...`, prefix it;
     if it carries no `Authorization` header, add `Bearer <accessToken>` read fresh from
     the credentials file. Every other request (the SDK's own calls, already correctly
     addressed because `ANTHROPIC_BASE_URL` is unchanged) passes through untouched.
  4. Background goroutine, every 60s: if `expiresAt` is within 5 minutes, run one
     `claude -p ok --model <cheap model> --no-session-persistence --strict-mcp-config
     --mcp-config '{"mcpServers":{}}'` with `ANTHROPIC_BASE_URL=http://<garcon>/claude/<account>`
     (no socket vars) so Claude Code renews its own login the normal way; guard with a
     lock file in `<config-dir>` so concurrent invocations for the same account don't race.
  5. Exec the child with `ANTHROPIC_UNIX_SOCKET=<sock>`, unchanged `ANTHROPIC_BASE_URL`,
     `CLAUDE_CODE_OAUTH_TOKEN`, `CLAUDE_CODE_OAUTH_SCOPES` (+ subscriptionType/
     rateLimitTier if present), `CLAUDE_CONFIG_DIR=<config-dir>`, and the passed args —
     as a child process, not an exec-replace, since the parent must keep serving the
     socket and renewal loop until the child exits.
  6. On child exit: stop the loop, remove the socket, exit with the child's status.

### Docs / Settings

- Settings → Connect a harness: add a "Remote Control" variant next to the existing
  Claude snippet: `garcon claude --account <email> -- claude`, with a one-line note that
  the plain env-var snippet has no Remote Control.
- `README.md`, `npm/ai-garcon/README.md`, `docs/connect.html`,
  `.claude/skills/add-provider/SKILL.md`: mirror the same addition.

### Out of scope

- No systemd/launchd unit changes; this rides entirely inside the `garcon` binary.
- No change to `internal/proxy.ParseRoute`, `Providers`, or usage recording.
- Does not touch this machine's `launch.py`, which can later be simplified to call
  `garcon claude` once this ships.

## Not chosen

- A persistent per-account daemon inside the main `garcon` service, configured via a new
  accounts list in `~/.config/garcon/config.json`: needs a new config surface and a
  registration step; the per-invocation design needs neither.
- Garcon holding or caching credentials itself: keeps the "stores no credentials"
  property — it reads the account's own credentials live, only when a request needs one.

## Verification

1. `garcon claude --account me@x -- claude -p ok --model <cheap> --no-session-persistence`:
   confirm a `usage.jsonl` row for that account, and that the socket file is gone after exit.
2. Same curl checks as the machine-specific version (bad bearer → Anthropic's 401; bare
   policy poll → 200) against the new socket.
3. `claude remote-control --help` under `garcon claude` prints help, not the refusal;
   `/rc` from a real session shows up in the iOS app.
4. Force (on a copy of a credentials file, never a real one) an expiry-window renewal and
   confirm the background loop logs it and a live session recovers via its normal 401 path.
5. macOS: verify the Keychain read path and unix socket path length under `$TMPDIR`.
6. Unit tests for the rewrite rule (adds prefix + bearer only when both are missing),
   mirroring `internal/proxy/proxy_test.go`'s table-test style.
