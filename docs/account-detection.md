# Which account made a request?

Garcon labels requests from the credentials sent upstream. With Codex routing enabled, that's the selected pool account. In pass-through mode, it's the client's account.

## Routes

URLs name the tool and provider, never the account:

| Tool | Base URL on `http://127.0.0.1:4141` |
| --- | --- |
| Codex | `/codex/backend-api/codex` |
| Claude Code | `/claude` |
| Pi with Codex | `/pi/chatgpt/backend-api/codex` |
| Pi with OpenRouter | `/pi/openrouter/api/v1` |
| Hermes with ChatGPT | `/hermes/chatgpt/backend-api/codex` |
| Other clients | `/<harness>/<provider>` plus the API prefix |

See [connection examples](connect.html). Remove account labels from older URLs and restart the client; the old account flag is unsupported.

## Labels you may see

- **ChatGPT:** the selected account header and token claims identify the account. An available email becomes its label; a different selected workspace may appear by ID. The upstream still authenticates the request.
- **Claude OAuth:** Garcon asks Anthropic's profile endpoint for the account email or UUID. Failed lookups get a credential fingerprint instead of borrowing another account's identity.
- **API keys:** a provider-specific fingerprint identifies the key, not a person. Separate keys stay separate; rotating a key creates a new label. An Anthropic API-key header takes precedence over OAuth attribution.
- **No credentials:** the label is `<provider>:unknown`.

A saved OpenRouter key replaces the client's bearer token, so attribution follows that saved key. Without one, Garcon accepts the client's own key; the `garcon-local` placeholder isn't a credential.

Memory caches hold labels and credential digests. The cache holds up to 1,024 entries: successes for an hour, failures for a minute. It clears at restart. Identity lookup failure doesn't change pass-through request contents. The profile endpoint is private and may change.

[Subscription limits](subscription-limits.md) are a separate account-wide snapshot. They include work outside Garcon and don't override pass-through credentials. Old usage records remain intact; display labels may be normalized for grouping.
