# Automatic account detection

Use these base URLs without an account label:

| Tool | Base URL |
| --- | --- |
| Claude Code | `http://127.0.0.1:4141/claude` |
| Codex | `http://127.0.0.1:4141/codex/backend-api/codex` |
| Hermes / Anthropic | `http://127.0.0.1:4141/hermes/anthropic` |
| Hermes / OpenAI | `http://127.0.0.1:4141/hermes/openai/v1` |
| Hermes / OpenRouter | `http://127.0.0.1:4141/hermes/openrouter/api/v1` |
| Hermes / ChatGPT | `http://127.0.0.1:4141/hermes/chatgpt/backend-api/codex` |

`garcon claude` always detects accounts, including its renewal requests.
There is no account flag or account field in the connection settings.

For Hermes with a ChatGPT login, use
`HERMES_CODEX_BASE_URL=http://127.0.0.1:4141/hermes/chatgpt/backend-api/codex hermes chat --provider openai-codex`.

Detection runs on the credentials attached to each completion request, independent
of the tool that sent it. Switching logins or running multiple sessions does not
require changing the base URL. Garcon never consults a shared “current account”
file or reads a prompt to determine identity.

- **ChatGPT:** use `ChatGPT-Account-Id` and the access token's account/profile claims.
  Display an available email as the account name without a workspace suffix.
  Usage with the same email is grouped into one account. A selected account that differs from the token's default wins and is
  displayed by ID. These are attribution hints, not locally verified authentication;
  the upstream remains responsible for authentication.
- **Anthropic OAuth:** tokens beginning with `sk-ant-oat` are resolved through
  `https://api.anthropic.com/api/oauth/profile`. Use the returned account email,
  or account UUID if email is absent. The lookup goes only to Anthropic, refuses
  redirects, times out after two seconds, and reads at most 64 KiB. It happens
  after the completion response has been received, before the usage row is saved;
  lookup time is excluded from the recorded model latency. A first lookup may
  delay closing the response by up to two seconds.
- **API keys:** use a provider-scoped SHA-256 fingerprint truncated to 96 bits,
  displayed as `<provider>:key:<digest>`. This identifies a key, not a person or
  billing account. Different keys cannot automatically be grouped into one account;
  key rotation creates a new label. Anthropic's `x-api-key` takes precedence over
  OAuth attribution when both headers are supplied.
- **Failed OAuth lookup:** use `<provider>:credential:<digest>`; no stale account
  is borrowed from another token. Without credentials, use `<provider>:unknown`.

Only digests and resolved labels are cached in memory, never raw credentials.
The cache holds at most 1,024 entries, coalesces concurrent lookups of the same
token, retains successes for an hour and failures for a minute, and is discarded
at restart. A refreshed token is looked up independently and resolves to the same
account when the provider returns the same identity. Requests and responses are
forwarded unchanged, including when identity lookup fails. Existing usage and
sync data are not rewritten.

All harnesses use account-free routes: `/claude/…`, `/codex/…`, or
`/<harness>/<provider>/…`. Account-labeled routes and the old account flag are
rejected. To upgrade, remove the account segment from each client's base URL and
restart existing sessions so they reload their configuration. Keep any SDK path
suffix, such as `/backend-api/codex`, `/v1`, or `/api/v1`. Historical usage files are not rewritten. The dashboard normalizes previously
generated email-plus-workspace labels to the email so their usage is grouped together.

Provider integration references: [Hermes authentication](https://github.com/NousResearch/hermes-agent/blob/main/hermes_cli/auth_codex.py)
and [Codex authentication](https://developers.openai.com/codex/auth).
Anthropic's OAuth profile endpoint is an internal Claude endpoint and may change;
failed or changed responses use the fallback described above.
