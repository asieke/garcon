# Plan: keep Claude Code Remote Control working alongside Garcon

Status: superseded 2026-09-13 (same day, Claude Code 2.1.269). The unix socket row below was
wrong: `ANTHROPIC_UNIX_SOCKET` works with no Garcon change once the launcher (1) keeps
`ANTHROPIC_BASE_URL=http://127.0.0.1:4141/claude/<account>` so the SDK still sends the canonical
route over the socket, (2) passes the profile's real access token in `CLAUDE_CODE_OAUTH_TOKEN`
with its scopes in `CLAUDE_CODE_OAUTH_SCOPES` (the client judges "claude.ai login" and scopes
from those two variables under the socket; the placeholder failed only because the scopes
defaulted to inference-only), and (3) serves the socket itself, adding the route and the
bearer to the one request Claude Code sends bare, `GET /api/claude_code/policy_limits`, which
must succeed or `allow_remote_control` is denied on cache miss. Implemented as
`agent-profiles proxy` in `~/.config/agent-profiles/launch.py` plus a user service; the same
daemon renews the stored login before expiry because a socket session never refreshes an
environment token and only re-reads `.credentials.json` on a 401. Verified end to end:
model calls are recorded as before and `claude remote-control` is accepted. Option B below
is no longer needed; kept for the record.

## Problem

`claude rc` (Remote Control) refuses to start when `ANTHROPIC_BASE_URL` is set to
anything whose host is not exactly `api.anthropic.com`:

```
Error: Remote Control is only available when using Claude via api.anthropic.com.
ANTHROPIC_BASE_URL is set and does not point at api.anthropic.com ...
(_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL does not apply to Remote Control.)
```

Garcon's Claude integration (and `~/.config/agent-profiles/launch.py`) relies on
`ANTHROPIC_BASE_URL=http://127.0.0.1:4141/claude/<account>`, so every Claude profile
is locked out of Remote Control.

## What was ruled out (all tested on 2026-09-13)

| Idea | Result |
| --- | --- |
| `_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL=1` | Explicitly excluded from this one check. |
| Host tricks (`api.anthropic.com:4141`, path prefix) | Check is `new URL(v).host === "api.anthropic.com"`; a port or anything else fails. |
| `CLAUDE_CODE_API_BASE_URL` | Only affects the Files API. Model calls went direct; Garcon logged nothing. |
| `ANTHROPIC_UNIX_SOCKET` (transport used by `claude ssh`) | Bypasses the URL check and speaks plain HTTP on the socket, but the client then treats auth as external. Without a token: "Not logged in". With the `ssh-placeholder` token: Remote Control rejects it as an inference-only token. |

## What was verified to work

1. **CONNECT proxy.** With `ANTHROPIC_BASE_URL` unset and
   `HTTPS_PROXY=http://127.0.0.1:<port>` pointing at a bare tunnel, model calls
   arrive as `CONNECT api.anthropic.com:443` and `claude rc` reaches the
   "Enable Remote Control? (y/n)" prompt. Other hosts also go through the tunnel
   (seen: `http-intake.logs.us5.datadoghq.com:443`).
2. **OpenTelemetry events.** `CLAUDE_CODE_ENABLE_TELEMETRY=1 OTEL_LOGS_EXPORTER=console`
   emits one `claude_code.api_request` log record per API call with the attributes
   Garcon needs: `model`, `input_tokens`, `output_tokens`, `cache_read_tokens`,
   `cache_creation_tokens`, `duration_ms`, `ttft_ms`, `request_id`, plus
   `user.email`, `session.id`, `cost_usd`. There is a sibling `api_error` event.

## Recommended approach: OTLP receiver (option B)

Claude profiles stop using the proxy entirely; Garcon records Claude usage from
telemetry instead. Codex and other harnesses keep the existing proxy route.

### Garcon changes

- New route `POST /otel/<harness>/<account>/v1/logs` handled in `cmd/garcon/main.go`
  next to `proxy.ParseRoute`. Accept OTLP/HTTP with `application/json`
  (protobuf can come later if anything needs it). Respond `200 {}`.
- New package `internal/otel`: parse `resourceLogs[].scopeLogs[].logRecords[]`,
  keep records whose `body.stringValue == "claude_code.api_request"`, and map to
  `usage.Record`:
  - `Time` from `timeUnixNano` (or `event.timestamp`), `Harness`/`Account` from the URL,
    `Provider = "anthropic"`, `Model`, `Input = input_tokens`,
    `CacheRead = cache_read_tokens`, `CacheWrite = cache_creation_tokens`,
    `Output = output_tokens`, `Ms = duration_ms`, `FirstByteMs = ttft_ms`, `Status = 200`.
  - `api_error` records: `Status` from the event's status attribute if present, else 0;
    token fields zero.
  - Leave the httptrace fields nil; they are proxy-side measurements that don't apply.
- Deduplicate on `request_id` within a short window; the OTEL exporter retries batches.
- Guard the route with the same `local.Guard` loopback rule as everything else.
- Settings page: add a "Claude Code (telemetry)" configuration snippet alongside the
  base-URL one, and label the base-URL snippet as incompatible with Remote Control.
- Dashboard: nothing structural; rows look like proxy rows. Consider a `source`
  field (`proxy` | `otel`) on `usage.Record`, omitempty, for later filtering.
- Tests: fixture JSON captured from the console exporter; one `api_request`, one
  `api_error`, one unrelated event (`user_prompt`) that must be ignored.

### Launcher changes (`~/.config/agent-profiles/launch.py`)

For `harness == "claude"` replace the two base-URL lines with:

```
CLAUDE_CODE_ENABLE_TELEMETRY=1
OTEL_LOGS_EXPORTER=otlp
OTEL_EXPORTER_OTLP_PROTOCOL=http/json
OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://127.0.0.1:4141/otel/claude/<email>/v1/logs
OTEL_LOG_USER_PROMPTS=0
```

Optionally `OTEL_LOGS_EXPORT_INTERVAL=2000` to shorten the default batching delay.
The "proxy must be running" check in the launcher can stay; a dead endpoint only
loses telemetry, it does not break the harness.

### Trade-offs to accept

- No HTTP status for successes beyond "it worked", no proxy-side latency breakdown.
- Rows arrive a few seconds after the call, batched, rather than inline.
- Metrics/logs also include `user.email` and `session.id`; store only what
  `usage.Record` needs.

## Alternative: TLS-intercepting forward proxy (option A)

Keeps the proxy model for Claude but is considerably more work:

1. Garcon accepts `CONNECT`. For `api.anthropic.com:443` it terminates TLS with a
   leaf cert signed by a Garcon-generated CA (stored under `~/.local/share/garcon/`),
   then reuses the existing `internal/proxy` tap on the decrypted stream. Every
   other host is tunnelled byte-for-byte.
2. Launcher sets `HTTPS_PROXY=http://127.0.0.1:4141` and
   `NODE_EXTRA_CA_CERTS=~/.local/share/garcon/ca.pem` instead of the base URL.
   Both are documented Claude Code settings.
3. Account attribution: the URL label is gone, so either run one listener port per
   account or derive the account from the OAuth token / a `Proxy-Authorization`
   value set per profile.

Pick this only if the inline status/latency fields turn out to matter more than the
extra TLS and CA management.

## Next steps when picking this up

1. Capture a raw OTLP/HTTP JSON payload (point `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` at a
   `nc -l` or a tiny Python server) to use as the test fixture; the console exporter's
   output is not the wire format.
2. Implement `internal/otel` + route + tests.
3. Update the launcher for one profile (`claude-personal`), run `claude rc` and a
   normal session, confirm rows appear in the dashboard.
4. Roll out to the other Claude profiles and update README / docs/install.html.
