# Product acceptance and verification

Garcon succeeds when a person can prove which account handled a coding request, understand its usage, and act on a stale or failed connection without guessing. These are acceptance targets, not a claim that every target is implemented or verified in an installed release. Record the tested commit and running binary separately.

## Acceptance criteria

| ID | Outcome and concrete pass condition | Verification |
| --- | --- | --- |
| A1 | **Trace a request to its actual account.** A short coding request completes through Garcon. Its ledger entry has the same request/session ID, harness, model, final status, selected account, and token totals as the observed stream. Within two configured polling intervals, Logs and the matching client session show the entry; identical session IDs across clients remain distinct. A Connected label alone is insufficient. | Match a controlled request to Logs and Sessions; use the proxy attribution and store session tests below. Keep account identifiers in private evidence. |
| A2 | **Keep account selection predictable.** With no manual pin, a new conversation selects an eligible account by remaining quota per hour and retains that assignment across requests and a restart. A manual pin selects the account for new pooled coding conversations; existing conversations retain their persisted assignment. An unavailable selected account returns an explicit error with no replay, alternate account, OpenRouter fallback, or reset-credit redemption. | Exercise selection, continuation, new-conversation pin, existing-conversation continuity, unavailable-account, and restart cases against disposable profiles/databases. Run the routing tests; verify a live request against the active pin without changing production routing solely for a smoke test. |
| A3 | **Explain what the coding pin covers.** Pooled coding, including coding requests with image inputs, uses the selected coding account. Realtime voice and built-in image generation/editing preserve the caller identity and payload outside that pool. Attribute tool-created follow-up coding by the actual request route. Unknown account usage stays unknown; provider-wide quota changes are not proof of Garcon-routed traffic. | Run proxy voice/image boundary tests. Before calling this live-verified, capture one request for each exercised route and compare its observed identity with the applicable rule. |
| A4 | **Show useful ticker activity once.** Both dashboard and compact-widget tickers suppress zero-token activity; a stream qualifies when tokens first arrive. Cache-only requests qualify. Input, cache-read, cache-write, and output tokens are counted once. Initial history, reopen/reconnect, and consumed entries never replay; bursts remain bounded. The target label is account + device + tokens. | Feed zero-token, cached-only, streaming-to-complete, duplicate, restart, and burst fixtures through both queues. Observe a new live event. Device labeling is a target requiring implementation/verification; existing provider labels do not satisfy it. |
| A5 | **Make the widget usable and fresh.** The widget leaves its initial loading state after a successful data response, displays provider/person/workspace seats separately, and shows provider-check time and stale/login errors. Unknown quota/reset/credit values remain unknown. After a successful refresh response, the display updates within two 10-second polling intervals. Dashboard navigation opens the full dashboard in a separate browser tab/window; the native widget delegates to the default browser and remains open. | Check initial load, refresh, empty/error/stale fixtures, and browser navigation. Test the native app separately; an HTML target attribute does not prove native external-browser behavior. |
| A6 | **Preserve trustworthy local data.** Restarting against the same database retains assignments and history. Completed, failed, and interrupted requests remain distinguishable. The ledger stores usage metadata rather than prompt/response bodies or credentials. Normal public staging excludes local project context and session exports. | Run store lifecycle, proxy stream/error, and credential-isolation tests with fixtures. Inspect the Git index and actual ignore behavior for private paths before publishing. |
| A7 | **Run and release the version that was tested.** Production and preview remain separate. The installed binary and running service versions agree; dashboard/widget/API reads work. A PR passes backend tests, frontend checks/tests/build, and npm wrapper tests. After an authorized merge, CI build and npm publication succeed and the published version, tag, package repository metadata, and installed version agree. | Use `garcon doctor`, HTTP/UI checks, the suite below, and the release workflow receipt. Verify npm Trusted Publisher owner/repository/workflow explicitly after a repository transfer. A green build alone does not prove publication or deployment. |

## Scope boundaries

The existing product is local-first. No Garcon account or cross-device synchronization is claimed. An optional future sync milestone should preserve local SQLite and routing while sharing allowed usage metadata only. Before shipping it, verify two-device deduplication, offline replay, a bounded initial backfill, newest-snapshot allowance selection without summing percentages, account isolation, sign-out behavior, and no credentials/prompts/responses in uploads. Keep this milestone explicitly planned until implemented and tested.

Experimental Claude gateway and external CLI delegation need their own opt-in verification. Do not treat an ordinary Claude profile launcher as pooled routing or a mocked gateway test as a real Desktop login. See [routing](codex-routing.md) and [CLI delegation](codex-cli-delegation.md).

## Repeatable automated checks

Run from a clean checkout with the tool versions in `.mise.toml`:

```sh
go test ./...
npm ci --prefix web
npm run check --prefix web
npm test --prefix web
npm run build --prefix web
node --test npm/ai-garcon/test/*.test.js
git diff --check
```

The Go suite uses disposable fixtures and mock upstreams for routing and transport integration. The real login-renewal test is opt-in (`GARCON_TEST_CODEX_PROFILE`); ordinary suite success does not verify a real credential renewal.

Useful existing evidence:

- A1–A3: `internal/proxy/codex_routing_test.go`, `realtime_test.go`, `images_test.go`, and `internal/codexrouting/{router,pin,body}_test.go`.
- A1/A6: `internal/store/{http,sessions,store}_test.go`, proxy completion/tap tests, and limit credential-isolation tests.
- A4/A5: `web/test/ticker.test.mjs`, `web/test/live-requests.test.mjs`, and provider/widget tests. Inspect both ticker suites: coverage for one surface does not establish parity.
- A7: onboarding/service tests, `npm/ai-garcon/test/package.test.js`, and CI/release jobs.

## Live end-to-end verification record

For every run, record date/time and timezone, source commit, installed/running version, surface (API/browser/native), scenario, expected result, observed result, and evidence location. Use **passed**, **failed**, **observed only**, or **not run**. Store real account/device names, request IDs, paths, and screenshots in ignored local records; public reports use synthetic or redacted evidence.

1. Run `garcon doctor --url http://127.0.0.1:4141`. Check dashboard and widget rendering plus read-only config, limits, routing, logs, sessions, analytics, and recent-request endpoints. HTTP 200 establishes availability, not correct provider routing.
2. From a configured client, send one short identifiable request. Record its start time, route, session/request ID, final status, account match, and token usage; confirm it in Logs, Sessions, and the ticker. Existing traffic may supply observational evidence, but is not a controlled end-to-end test.
3. Verify a tokenless event stays out of both tickers and a new nonzero event appears once. Reopen the widget to check no history replay. Verify device labeling explicitly.
4. Check the freshness timestamp and an explicit refresh, then follow the dashboard link. Repeat external-browser navigation in the installed native widget. Record untested error/offline states as not run.
5. Use disposable instances for pin changes, exhausted accounts, interruptions, and restart persistence. A production restart, account change, or release is a separate operation; do not perform it just to fill a verification table.
6. Record failures even when other requests succeed. Reconcile tested source with the running build before marking a release ready.
