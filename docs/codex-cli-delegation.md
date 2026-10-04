# Codex driving an external Claude Code task

Research and prototype, checked 2026-10-01. No live inference was performed.

## Decision

Codex can supervise an external command and consume its result. This prototype adds
`garcon delegate`, a one-shot, opt-in command that starts the installed, unmodified
Claude Code CLI. Codex remains the driver of its own task and retains its own model.
Claude Code owns the delegated inference and authentication.

This does **not** make a Claude subscription the Codex application's native model
backend. No documented Claude CLI-to-Codex provider interface was found. A native
provider would need an actual compatible model endpoint; CLI print JSON is not that
endpoint. No change is made to Codex configuration, model names, app binaries, account
pools, or existing Garcon relay behavior.

```text
Codex app / its current model
  -> authorized local shell command: garcon delegate
     -> unmodified Claude Code CLI, user's own existing identity
        -> its configured provider
     <- bounded, validated task result
  <- external tool output (untrusted context), then Codex continues
```

A future MCP wrapper could expose this same task operation as a tool; it would still
not replace the model. MCP is a documented tool/context extension for local Codex
clients and supports command-launched stdio servers. This PR deliberately needs no
MCP server, plugin installation, daemon, network listener, or new dependency.
[OpenAI MCP documentation](https://learn.chatgpt.com/docs/extend/mcp).

## What was inspected

The isolated worktree started at freshly fetched `asieke/garcon` remote `main`,
`93ff7f2bcbc5370c1218c24b3475bb5ebb8ef3a9`. The GitHub connector confirmed personal
account `asieke`. The original checkout and its local work were left unchanged.

- `internal/proxy/proxy.go` forwards provider payloads; it is not a Responses-to-Messages translator.
- `internal/codexrouting` routes Codex accounts, not Claude Code subprocesses.
- `internal/claude/claude.go` and `relay.go` implement the existing launcher/relay,
  including credential loading, refresh and interception settings. Those are **not**
  used by this prototype: doing so would violate this experiment's credential boundary.
- `cmd/garcon/main.go` dispatches the new command before service/database initialization.
- The local `AGENTS.md` requires personal GitHub identity. Repository commit and
  provider skills were read; no repository `.agents/skills` directory was present.

Read-only local evidence: `claude --version` reported **2.1.285**;
`codex --version` reported **0.155.0**. Their help was inspected. Claude's default
profile reported no login; the existing personal profile's `claude auth status`
reported a first-party Claude account with a Max subscription. No token values,
authentication files or keychain contents were read. Desktop bundle version was not
available at the standard application path; CLI version is not asserted to identify
the running desktop bundle.

## Supported interfaces versus native-backend replacement

| Approach | Finding |
| --- | --- |
| Local external task via `claude --print` | Documented CLI interface; implemented here with synthetic coverage. |
| Codex MCP tool invoking that CLI | Documented extension mechanism; optional future packaging, not implemented. |
| Point Codex at Claude CLI stdout | No such native provider contract found; not implemented. |
| Translate Codex Responses traffic into Claude Messages | Requires a real adapter, authorized upstream access and end-to-end capability tests; not achieved by relabeling URLs or models. |
| Reuse subscription tokens in a custom proxy | Not part of this architecture; no token extraction, forwarding, refresh or identity impersonation. |
| Approved API/cloud gateway | Viable separately authorized backend project; requires access, billing and compatibility validation. |

OpenAI documents custom providers in terms of base URL, wire API and authentication.
It also documents a built-in Amazon Bedrock provider, so this finding is not a claim
that Codex can never use third-party models. Those capabilities do not convert a
personal Claude CLI login into gateway credentials. Cloud/local-access configuration
has different scope from local clients.
[Advanced configuration](https://learn.chatgpt.com/docs/config-file/config-advanced).

The current gateway contract requires Responses endpoints, incremental SSE including
terminal success/error events, conversation replay, matched tool calls/results,
correct model metadata and authentication. An Anthropic Messages endpoint or a
successful text response alone is insufficient. A separately approved gateway must
validate the actual Codex client/model combination, including tools and continuation.
[Gateway compatibility](https://learn.chatgpt.com/docs/enterprise/gateway-compatibility).

Anthropic documents `claude -p` as its programmatic interface, JSON output and exit
status handling. Errors can arrive on stdout during a run, not only on stderr.
SIGTERM ends a running process; an interrupted turn is not a successful result.
`--bare` would skip subscription OAuth/keychain access, so it is intentionally not
used for this existing-login experiment.
[Programmatic CLI](https://code.claude.com/docs/en/headless).

## Authentication and terms boundary

Anthropic's current Claude Code guidance allows the end user to sign in to an
unmodified binary with their own subscription. Its product-hosting conditions include
commercial terms and preserving native authentication methods. It prohibits developers
from collecting/intermediating Claude.ai credentials or routing subscription requests
on behalf of other users. Ordinary individual usage limits still apply. Running the
real CLI is relevant evidence, **not blanket permission for any integration**.
[Claude Code legal and compliance](https://code.claude.com/docs/en/legal-and-compliance).

The applicable agreement depends on account/product use. Review the current
[Consumer Terms](https://www.anthropic.com/legal/consumer-terms) and
[Commercial Terms](https://www.anthropic.com/legal/commercial-terms) before distributing
or hosting this as a service; this personal prototype is not a compliance certification
or authorization to resell inference. The commercial restrictions also address
competing products, reverse engineering and resale absent approval.

OpenAI's terms prohibit circumventing limits or protective measures and make
third-party services subject to their own terms. This implementation uses a normal
command invocation; it does not patch the app, impersonate a first-party client,
scrape its private protocol, or alter its account configuration.
[OpenAI Terms of Use](https://openai.com/policies/terms-of-use/).

The child inherits the environment and existing CLI profile. Garcon does not select
another account or copy credentials. Claude itself may perform its ordinary native
credential refresh. Supported API/cloud identities remain the CLI's responsibility;
this is not a credential manager. Environment-based provider/key overrides may change
billing or identity: verify the exact environment used for a call.
[Claude authentication](https://code.claude.com/docs/en/authentication).

## Build and use

From this checkout, using the pinned Go/Node versions in `.mise.toml`:

```sh
(cd web && npm ci --ignore-scripts --no-audit --no-fund && npm run build)
go build -o /tmp/garcon-delegation ./cmd/garcon
/tmp/garcon-delegation delegate --help
```

Before enabling inference, inspect the **unmodified** executable and its help/status
using the same existing profile/environment you intend to use. For an existing
personal profile, for example:

```sh
CLAUDE_CONFIG_DIR="$HOME/.claude-personal" "$HOME/.local/bin/claude" --version
CLAUDE_CONFIG_DIR="$HOME/.claude-personal" "$HOME/.local/bin/claude" auth status --text
```

Do not substitute a wrapper that routes through `garcon claude`. Do not create a new
login or copy authentication files for this experiment. Stop if the status identifies
the wrong account or access is missing. Check allowance and extra-usage billing in
the provider's own controls. A successful status command is not proof of remaining
allowance, zero incremental cost, or valid inference access.

After explicit authorization for inference and any applicable cost, the caller can
supply a small prompt file through stdin:

```sh
CLAUDE_CONFIG_DIR="$HOME/.claude-personal" /tmp/garcon-delegation delegate \
  --allow-inference --executable "$HOME/.local/bin/claude" \
  --cwd /path/to/trusted/worktree --timeout 2m < prompt.txt
```

In Codex, ask the task to run that command with your authorized prompt file, then
review its `result` as external tool output. Do not change `model_provider` or copy
Claude model names into the Codex model picker. No configuration install is necessary.
The example above was **not run against live Claude inference** during this work.
Without `--allow-inference`, the command fails before reading stdin or spawning a CLI.

The invoked arguments are fixed: print JSON, safe mode, no built-in tools, deny
permission prompts via `dontAsk`, empty strict MCP configuration, and no persisted
conversation. Safe mode disables customizations while retaining authentication;
managed policy can still apply, including policy hooks. This is not an OS sandbox.
Use a trusted binary and working directory, and review applicable managed policy.
[CLI flag reference](https://code.claude.com/docs/en/cli-reference).

## Result, lifecycle and limits

- Prompt bytes travel on stdin, never a shell command or process argument. Prompts
  must be nonempty and at most 1 MiB. No implicit file upload or conversation forwarding.
- A successful run writes one normalized JSON result to stdout, including `type`,
  `subtype`, `is_error` and `result`. Unknown native fields are ignored. The result
  must explicitly be a successful result envelope; malformed/trailing JSON fails.
- The deadline covers stdin and child execution (default 2m, allowed range >0 to 10m).
  SIGINT, SIGTERM and SIGHUP cancel the command. The Unix process group receives TERM,
  then KILL after 300ms if needed. The parent reaps the process; inherited output
  pipes have a one-second wait bound and remaining group members are killed.
- Stdout is capped at 1 MiB and stderr at 64 KiB. Exceeding either cap terminates the
  task. Partial responses never masquerade as success. Diagnostics are discarded
  rather than exposing arbitrary provider error text or secrets to the calling agent.
- Failures distinguish start errors, nonzero exit, invalid result, provider result
  failure, timeout/cancellation and output limits. Failure stdout is empty; the Garcon
  command exits 1. CLI auth/quota failures require separate native diagnosis.
- Inherited relay settings `ANTHROPIC_BASE_URL`, `ANTHROPIC_UNIX_SOCKET` and
  `_CLAUDE_CODE_ASSUME_FIRST_PARTY_BASE_URL` are rejected by name, without logging
  their values. There are no retries, quota-driven account switches or disguised headers.

This deliberately small bridge has no streaming UI, resumable state, tool execution,
image input, model picker, Responses endpoint, usage-database entry or billing cap.
The deadline and opt-in are not spend guarantees. It supports Garcon's macOS/Linux
targets; process groups do not contain a malicious process that detaches into another
session. No claim is made that killing a client refunds provider work already started.

## Validation and remaining blockers

Synthetic tests use the Go test executable as a fake CLI, never a real account.
They exercise literal stdin/arguments, malformed/incomplete/error results, nonzero
exit, missing executable, oversized input/output, explicit opt-in, deadlines,
cancellation, TERM-resistant descendants, inherited pipes and blocked stdin.
Run `go test -race ./internal/delegate` and `go test ./...`.

Live inference remains unverified because incremental-cost authorization/allowance
was not established. Native model-backend replacement remains a separate project
requiring an authorized API/cloud endpoint and full Responses/tool-loop qualification.
No new credentials, account changes, provider configuration, deployment or merge were
performed. PR CI and exact local test results are recorded in the draft PR.
