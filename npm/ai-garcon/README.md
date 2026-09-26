# ai-garcon

See which account is doing the work.

Garcon runs locally between your coding tools and their providers. It routes new Codex conversations across accounts you choose, keeps each conversation on its account, and shows requests, usage, and errors in a dashboard.

[Documentation](https://asieke.github.io/garcon/) · [Source](https://github.com/asieke/garcon)

## Start

```sh
npm install -g ai-garcon@latest
garcon setup
```

macOS or Linux, x64 or arm64; Node 18+. Open **http://127.0.0.1:4141**, add accounts under **Providers**, then copy your tool's connection settings. Browser sign-in needs the provider CLI; pool enrollment is separate.

Codex uses Codex accounts only. Pi can use the Codex pool or OpenRouter. Claude Code uses its selected Claude profile. Sessions shows local Codex task titles, projects, and in-flight model requests; a task may still run tools between requests. Source docs may describe unreleased features.

## Keep it running

```sh
garcon update
garcon doctor --wait 10s
garcon service status
```

Finish active requests before updating—the proxy restarts. To try it without a service, run `npx ai-garcon@latest`.

History stays in local SQLite. Prompts and replies aren't saved. OAuth credentials stay with the provider CLIs; an OpenRouter key is stored separately. API-equivalent costs are estimates, not subscription charges.

Before uninstalling, restore your tools' original provider URLs. Then run `garcon service uninstall` and `npm uninstall -g ai-garcon`. Your usage and settings remain.
