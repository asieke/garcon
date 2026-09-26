# Find your way around Garcon

Open **http://127.0.0.1:4141**. The four main views are **Providers, Sessions, Analytics, and Logs**. Deep links use `?view=providers|sessions|analytics|logs`; older view links still work.

## Providers

Connected accounts appear above the client cards. **Add account** opens browser sign-in for ChatGPT or Claude, or a key form for OpenRouter.

Connecting an account and enrolling it are separate steps. Use **In pool** to choose the Codex accounts Garcon may route to. Cards show quota, resets, health, and scores. Priority numbers still affect selection, but the current UI has no group arrows or priority selector; use the routing API to change them.

The Codex, Pi, and Claude cards contain their connection instructions. Adding a login doesn't reconfigure those tools.

## Sessions

Search by Codex task title, project, account, model, or session ID. Click a title for its requests; copy the full ID when you need an exact match. Titles come from Codex's local index. Missing titles and older hashed assignments remain visible.

In-flight sessions come first. Activity distinguishes model responses, background reviews, and the last failed or interrupted request. **No model request** doesn't mean the task has finished—it may be running tools.

## Analytics and Logs

Analytics has Usage, Cost, and Models tabs with time-range filters. Costs estimate API-equivalent usage, not subscription charges; unpriced models are identified.

Logs includes request history and a separate System tab. The inspector shows copyable request metadata and errors. Prompts and replies aren't saved.

The ticker shows recent requests; pause it to read an item or open its inspector. Subscription allowances live at `/usage-widget/`.
