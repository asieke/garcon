# Agent workflow

Read @PROJECT.md (purpose, type, status, merge) and @README.md (scope, run, test, deploy) before starting any work. This repository follows the standard at https://github.com/asieke/workspace/blob/main/STANDARD.md.

1. **Check or create an Issue (for larger tasks):** If you are picking up a task from the backlog or starting a larger multi-step change, use a GitHub Issue and comment on it to claim it and state your branch name. For a small, self-contained change requested directly in chat, you can skip creating an Issue.
2. **Create a branch from the latest `origin/main`:**
   `git fetch origin && git switch -c <type>/<slug> origin/main`
   - `type` must be `feat`, `fix`, `chore`, or `docs`.
   - `slug` is two to five plain words in kebab-case, starting with the Issue number when one exists (for example, `feat/12-add-login` or `fix/login-timeout`).
   - If your cloud session started on an auto-generated branch name such as `claude/...`, rename it first (`git branch -m <type>/<slug>`). You have standing permission to push the renamed branch.
   - Use one branch per task and one agent per branch. Local checkouts are disposable—always push your work to GitHub.
3. **Make the smallest complete change** that solves the task, and update any documentation affected by your change.
4. **Run `scripts/check.sh`** and fix any failures. Never skip, disable, or weaken a check.
5. **Open a pull request to `main`** using the PR template. Fill in the **What**, **Decisions**, and **Agent session** sections, and wait for CI to pass:
   - If `merge: owner` in `PROJECT.md`, leave the PR open—only the owner merges.
   - If `merge: agent` in `PROJECT.md`, you may merge the PR yourself (`gh pr merge --squash --delete-branch`) once the template is filled out and CI is green.
   - Before pushing follow-up edits in the same session, check `gh pr view --json state`; if the previous PR was already merged, branch fresh from `origin/main` and open a new PR.
6. **If you stop before finishing:** Push your branch and leave a comment on the Issue summarizing what is done, what is next, any blockers, and the branch name. (If you already opened or merged a passing PR, no extra Issue comment is needed.)

## Rules

- Never commit secrets to the repository. Put environment variable names in `.env.example`.
- Never commit directly to `main`, never force-push, and never rewrite another agent's branch.
- Follow the deployment and testing instructions in `README.md` under `Deploy` and `Test`.
- Branch names must use only lowercase letters, numbers, and hyphens (up to 50 characters), with no person names, dates, machine names, or random words.

## Ways of working

This project follows the standard defined in https://github.com/asieke/workspace. To create, convert, audit, list, or refresh projects, clone a fresh copy of `asieke/workspace` into a temporary directory and follow `skills/<name>/SKILL.md` (`create-project`, `convert-project`, `audit`, `portfolio`, or `refresh`). Always use a fresh clone of `main` and delete the temporary folder when done.

## Project rules

- **Every merge into `main` is a release.** `.github/workflows/release.yml` publishes a new `ai-garcon` patch version to npm, tags it and creates a GitHub release. Put `[minor]` or `[major]` in the PR title to change the bump. Never commit a real version to `npm/ai-garcon/package.json` (it stays `0.0.0`). The `commit-garcon` skill in `.claude/skills/` covers releases and how to repair one.
- **This repository is public.** Keep credentials, account routing, host paths, raw conversations and private workflow records out of code, documentation, Issues and PR descriptions. Private notes and project context belong in the ignored `.local/` folder (for example `.local/AGENTS.md`); never force-add it. `scripts/check.sh` runs `.github/scripts/check-repository-boundaries.py` to verify that.
- **Tests need no `.env` and no real accounts.** Use disposable fixtures. Never copy authentication profiles or runtime databases into a checkout or worktree.
- **Toolchain:** versions are pinned in `.mise.toml` (`mise install`). `scripts/check.sh` runs the dashboard check, tests and build, `go test ./...` and the npm package tests. When a change touches the build, the npm package or a workflow, also run `scripts/release.sh 9.9.9 --dry-run` (CI runs it on every PR).
- **Development uses port 4242; the live service uses 4141.** Both development modes use real local data. Preparing or merging a PR does not restart the service: the live install is updated only by `scripts/deploy-macos.sh` on macOS or `scripts/deploy.sh` on Linux (see `README.md` under `Deploy`).
- **Worktrees and cleanup** follow [CONTRIBUTING.md](CONTRIBUTING.md#merge-and-cleanup): keep a task's worktree while its PR is open, and remove it only after the merged head is verified and its conversations are captured.
- **Product checks:** [docs/acceptance-checks.md](docs/acceptance-checks.md) defines which routing, usage, widget, privacy and release behaviors need live end-to-end evidence.
- **Skills:** `.claude/skills/` holds `install-garcon`, `update-garcon`, `commit-garcon` and `add-provider`. Follow them for those tasks.
