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

GitHub `main` is the accepted source of truth. Keep the canonical checkout clean on `main`. Before a new task, inspect status, remotes, and worktrees; fetch the verified remote and fast-forward clean main. Preserve dirty or divergent work instead of resetting it. Create a task branch and worktree from freshly fetched `origin/main`; resume an existing task in its existing worktree.

Follow root AGENTS.md and its local companions when present; private instructions are optional and excluded from this public repository. Standard tests and builds need no `.env` files. If a task needs additional environment files, use only explicitly approved sources, verify destination files are ignored and untracked, preserve restrictive permissions, and never overwrite an existing worktree environment. Do not copy authentication profiles or runtime databases into worktrees. Use disposable fixtures for tests.

Keep README content focused on purpose, scope, setup, and stable navigation. Issues own tasks and blockers; code and PRs own implementation and validation. Keep changing verification evidence in the relevant task or established records.

Commit only the files for the proposed change. Keep credentials, account routing, host paths, raw conversations, and private workflow records out of code, documentation, Issues, and PR descriptions. Preserve the existing exclusions for local context. Do not force-add excluded files.

Before publishing, inspect the complete outgoing diff and run:

```sh
python3 .github/scripts/check-repository-boundaries.py
```

This check detects tracked local context and missing ignore rules. It does not scan content for secrets or remove material from earlier commits; review the diff as well. CI runs the same check on pull requests.

## Verify and review

Use the toolchain versions and development commands in [README.md](README.md#work-on-garcon). For application changes, run the relevant backend and frontend checks; PR CI also runs the release build in dry-run mode. For documentation changes, check links and review the rendered text.

State what you verified and any remaining limitations. Passing tests, merging, publishing a release, and installing that release are separate outcomes. Merging to main triggers the release workflow; local installations require an explicit update. Preparing a PR does not restart the service.

## Merge and cleanup

Push the task branch and open or update its PR into protected main. Merge only with authorization and passing required checks. Keep the task worktree available while its PR is open.

After an authorized merge, fetch and verify GitHub's merged PR state and its final head, including squash merges. Remove a worktree and its branch only when the branch matches that head, no additional work remains, and no active process or session depends on the checkout. Preserve useful ignored artifacts and durable environment sources first. Use normal worktree removal without force. Retain dirty, unpublished, closed-unmerged, or uncertain work; do not delete by age or branch name. Guard remote branch deletion against concurrent head changes. Fast-forward clean canonical main afterward.

See [product acceptance and verification](docs/acceptance-checks.md) for scenario-specific checks. Cleanup does not authorize a release, service restart, or account change.
