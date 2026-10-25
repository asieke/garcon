# Garcon

Read `README.md` (setup, run, test, deploy), `CONTRIBUTING.md` (layout, changes, cleanup) and `PROJECT.md` (purpose and status) before changing anything.

## Project rules

- **Every merge into `main` is a release.** `.github/workflows/release.yml` publishes a new `ai-garcon` patch version to npm, tags it and creates a GitHub release. Put `[minor]` or `[major]` in the PR title to change the bump. Never commit a real version to `npm/ai-garcon/package.json` (it stays `0.0.0`). The `commit-garcon` skill in `.claude/skills/` covers releases and how to repair one.
- **This repository is public.** Keep credentials, account routing, host paths, raw conversations and private workflow records out of code, documentation, Issues and PR descriptions. Private notes and project context belong in the ignored `.local/` folder (for example `.local/AGENTS.md`); never force-add it. `scripts/test.sh` runs `.github/scripts/check-repository-boundaries.py` to verify that.
- **Tests need no `.env` and no real accounts.** Use disposable fixtures. Never copy authentication profiles or runtime databases into a checkout or worktree.
- **Toolchain:** versions are pinned in `.mise.toml` (`mise install`). `scripts/test.sh` runs the dashboard check, tests and build, `go test ./...` and the npm package tests, and also runs the release build in dry-run mode (`scripts/release.sh 9.9.9 --dry-run` without the tests); run that command on its own to see the full package listing.
- **Development uses port 4242; the live service uses 4141.** Both development modes use real local data. Preparing or merging a PR does not restart the service: an install is updated only by an explicit deploy (`scripts/deploy.sh` in the live checkout, `scripts/deploy-macos.sh` on a Mac, `garcon update` for npm installs; see `README.md` under `Deploy`).
- **Worktrees and cleanup** follow [CONTRIBUTING.md](CONTRIBUTING.md#merge-and-cleanup): keep a task's worktree while its PR is open, and remove it only after the merged head is verified and its conversations are captured.
- **Product checks:** [docs/acceptance-checks.md](docs/acceptance-checks.md) defines which routing, usage, widget, privacy and release behaviors need live end-to-end evidence.
- **Skills:** `.claude/skills/` holds `install-garcon`, `update-garcon`, `commit-garcon` and `add-provider`. Follow them for those tasks.
