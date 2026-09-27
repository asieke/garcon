# Find your way around Garcon

Open **http://127.0.0.1:4141**. The four main views are **Accounts, Sessions, Analytics, and Logs**. Deep links use `?view=accounts|sessions|analytics|logs`.

## Accounts

**Codex** and **Claude Code** use the same account-table layout: account identity, available usage, routing score, and pool membership. **Add account** offers provider-specific sign-in instructions.

Connecting an account and enrolling it are separate steps. Use **In pool** to choose the Codex accounts Garcon may route to. Cards show quota, resets, health, and scores. Selection happens automatically within the harness by quota remaining relative to reset time. There is no routing switch or manual priority setting.

Each card has a connection button at the right. Codex reads the saved user configuration. Claude checks its saved default HTTP gateway and registered profile launchers. Both open a copyable coding-agent setup prompt. Paste the prompt into an agent on the same computer to back up, merge, and verify the client's connection settings. Remote Control is opt-in; keep `remoteControlAtStartup: false`. Copying the prompt or adding a login does not reconfigure the client. After restarting the client, confirm a new request in Logs to verify live traffic.

Claude quota and reset windows come from its local account snapshots. Claude uses the login selected by its local profile; routing-score and pool cells are unavailable for Claude rather than implying Codex-style pooling.

## Sessions

Sessions, logs, and live activity identify each client with a logo, using a generic icon for unknown clients. Search by client, Codex task title, project, account, model, or session ID. Click a session for requests matching both its client and ID. Titles come from Codex's local index and are not applied to another client's session with the same ID. Missing titles and older hashed assignments remain visible.

In-flight sessions come first. Activity distinguishes model responses, background reviews, and the last failed or interrupted request. **No model request** doesn't mean the task has finished—it may be running tools.

## Analytics and Logs

Analytics has Usage, Cost, and Models tabs with time-range filters. Costs estimate API-equivalent usage, not subscription charges; unpriced models are identified.

Logs includes request history and a separate System tab. The inspector shows copyable request metadata and errors. Prompts and replies aren't saved.

The ticker shows recent requests; pause it to read an item or open its inspector. Subscription allowances live at `/usage-widget/`.

Claude’s **CLI launchers configured** badge verifies registered profile launchers against their saved checksums. **Default gateway configured** means the saved default HTTP gateway points to Garcon. Register a verified launcher with `garcon claude --config-dir /absolute/profile/path --register-launcher /absolute/launcher/path`; re-register after editing it. The check never executes the launcher and does not claim a live request or Remote Control connection. Claude Code 2.1.283 rejects Remote Control through Garcon's gateway/socket; explicit `garcon claude rc` requests may therefore be blocked.
