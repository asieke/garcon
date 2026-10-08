# Contributing to Garcon

Use [GitHub Issues](https://github.com/asieke/garcon/issues) for bugs and scoped work, and pull requests for proposed changes. Include the problem, the resulting behavior, and the checks you ran.

## Repository layout

Garcon is one repository. Its packages share the same review and release workflow.

| Location | Purpose |
| --- | --- |
| `cmd/`, `internal/` | CLI, backend, and embedded dashboard |
| `web/` | Dashboard source and frontend checks |
| `macos/` | Native usage widget |
| `npm/` | Package distribution |
| `scripts/` | Development, installation, and release tooling |
| `docs/` | Public technical documentation |

Keep product setup and usage in [README.md](README.md), and engineering details in public documentation. Keep source files in their existing locations.

## Prepare a change

Start from current remote main on a new branch or worktree. Inspect existing changes before editing and preserve work outside your task. Follow root AGENTS.md when present; local agent instructions are optional and excluded from this public repository.

Commit only the files for the proposed change. Keep credentials, account routing, host paths, raw conversations, and private workflow records out of code, documentation, Issues, and PR descriptions. Preserve the existing exclusions for local context. Do not force-add excluded files.

Before publishing, inspect the complete outgoing diff and run:

```sh
python3 .github/scripts/check-repository-boundaries.py
```

This check detects tracked local context and missing ignore rules. It does not scan content for secrets or remove material from earlier commits; review the diff as well. CI runs the same check on pull requests.

## Verify and review

Use the toolchain versions and development commands in [README.md](README.md#work-on-garcon). For application changes, run the relevant backend and frontend checks; PR CI also runs the release build in dry-run mode. For documentation changes, check links and review the rendered text.

State what you verified and any remaining limitations. Passing tests, merging, publishing a release, and installing that release are separate outcomes. Merging to main triggers the release workflow; local installations require an explicit update. Preparing a PR does not restart the service.
