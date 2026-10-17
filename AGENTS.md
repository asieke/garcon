# Agent workflow

Read @PROJECT.md (purpose, type, status, merge) and @README.md (scope, run, test, deploy) before starting any work. This repository follows the standard at https://github.com/asieke/workspace/blob/main/STANDARD.md.

**For `local` projects, sync before anything:** at session start, run `scripts/sync.sh --inspect` (Claude Code runs it automatically through `.claude/settings.json`). It fetches and reports the checkout's state without deploying anything. Before running, restarting, or deploying the app, using its live data, or running a scheduled job, run the full `scripts/sync.sh` and follow its output. If it reports `REFUSED`, stop those operations and report to the owner; do not stash, reset, or delete anything. A refusal does not block reviewing a pull request (see **Reviewing a pull request**). Never edit the live checkout: work in a worktree (step 2), and run scheduled jobs as `scripts/sync.sh <command>`.

1. **Check or create an Issue (for larger tasks):** If you are picking up a task from the backlog or starting a larger multi-step change, use a GitHub Issue and comment on it to claim it and state your branch name. For a small, self-contained change requested directly in chat, you can skip creating an Issue.
2. **Create a branch from the latest `origin/main`, in its own worktree:**
   `dir=$(scripts/worktree.sh <type>/<slug>) && cd "$dir"`
   - `type` must be `feat`, `fix`, `chore`, or `docs`.
   - `slug` is two to five plain words in kebab-case, starting with the Issue number when one exists (for example, `feat/12-add-login` or `fix/login-timeout`).
   - This creates `.claude/worktrees/<type>-<slug>` with `.env` linked from the main checkout, or reopens it to resume the task. Work only there, and leave the main checkout on `main`.
   - In a checkout cloned just for this session (such as a cloud session), skip the worktree: `git fetch origin && git switch -c <type>/<slug> origin/main`.
   - If your session or tool started on an auto-generated branch name such as `claude/...` or `worktree-...`, rename it first (`git branch -m <type>/<slug>`), and in a worktree run `scripts/worktree.sh --env`. You have standing permission to push the renamed branch.
   - Use one branch per task and one agent per branch. Local checkouts are disposable—always push your work to GitHub.
3. **Make the smallest complete change** that solves the task, and update any documentation affected by your change.
4. **Run `scripts/check.sh`** and fix any failures. Never skip, disable, or weaken a check.
5. **Push with `git push -u origin <branch>` and open a pull request to `main`** using the PR template. Fill in the **What**, **Decisions**, and **Agent session** sections. Then run `scripts/check.sh --report`, which posts the `check` status that merging requires (there is no CI). If you cannot run the checks here (for example, no Xcode or no `gh`), say so in the PR; the owner runs `scripts/verify-pr.sh <number>` on a machine that can.
   - If `merge: owner` in `PROJECT.md`, leave the PR open—only the owner merges.
   - If `merge: agent` in `PROJECT.md`, you may merge the PR yourself (`gh pr merge --squash`) once the template is filled out and the `check` status is green. GitHub deletes the merged branch; do not add `--delete-branch`, which fails inside a worktree.
   - Before pushing follow-up edits in the same session, check `gh pr view --json state`; if the previous PR was already merged, branch fresh from `origin/main` and open a new PR.
6. **If you stop before finishing:** Push your branch, leave its worktree in place, and leave a comment on the Issue summarizing what is done, what is next, any blockers, and the branch name. (If you already opened or merged a passing PR, no extra Issue comment is needed.)
7. **After the PR merges:** `scripts/worktree.sh --tidy`, run from the main checkout, removes worktrees whose pull request merged, along with their branches, and lists the rest. In `local` projects, every full `scripts/sync.sh` (before live-app operations and in every scheduled job) runs it. It never removes a worktree with uncommitted files or unmerged commits.

## Reviewing a pull request

A review is not a task, so skip the workflow steps above: it needs no full sync, Issue, branch, commit, push, or new pull request.
- **Read** the pull request, its discussion, its checks (`gh pr checks <number>`), and its diff, then inspect the code and run the relevant tests. Record the head commit you reviewed (`gh pr view <number> --json headRefOid`).
- **Test in isolation, so the main checkout stays on `main`:** `scripts/verify-pr.sh --no-report <number>` runs `scripts/check.sh` in a temporary worktree and posts nothing. For targeted tests, use a detached review worktree (`git fetch origin pull/<number>/head && git worktree add --detach "$(mktemp -d)/pr-<number>" FETCH_HEAD`) and remove it with `git worktree remove` when done. Never check the PR out in the main checkout. Run `scripts/verify-pr.sh <number>`, which posts the `check` status, only when asked to verify the PR.
- **Compare against the merge base** (what `gh pr diff <number>` shows, or `git diff origin/main...<head>`), not the current `main`. Report integration with the current `main` separately: textual conflicts (`gh pr view <number> --json mergeable`) and changes merged since the merge base that break the PR. Do not ask for a rebase just because `main` moved.
- **Publishing:** "Review this PR" returns findings in the conversation and posts nothing to GitHub. "Review and comment on this PR" also lets you post the findings to that PR without asking again, naming the commit you reviewed. Before posting line-specific findings, check whether the PR head has changed since you reviewed it; if it has, re-check each finding against the new head first. Neither request lets you change source files, commit, push, merge, or deploy. A review told to "post nothing" posts no comments and no status, so it tests only with `scripts/verify-pr.sh --no-report`.

## Rules

- Never commit secrets to the repository. Put environment variable names in `.env.example`.
- Never edit `.env`; in a worktree it is a link to the main checkout's file. If a change needs a new variable, add its name to `.env.example` and say so in the PR, so the owner can set the value before merging.
- Never commit directly to `main`, never force-push, and never rewrite another agent's branch.
- Follow the deployment and testing instructions in `README.md` under `Deploy` and `Test`.
- Branch names must use only lowercase letters, numbers, and hyphens (up to 50 characters), with no person names, dates, machine names, or random words.

## Ways of working

This project follows the standard defined in https://github.com/asieke/workspace. To create, convert, audit, list, or refresh projects, clone a fresh copy of `asieke/workspace` into a temporary directory and follow `skills/<name>/SKILL.md` (`create-project`, `convert-project`, `audit`, `portfolio`, or `refresh`). Always use a fresh clone of `main` and delete the temporary folder when done.

## Project rules

- **Every merge into `main` is a release.** `.github/workflows/release.yml` publishes a new `ai-garcon` patch version to npm, tags it and creates a GitHub release. Put `[minor]` or `[major]` in the PR title to change the bump. Never commit a real version to `npm/ai-garcon/package.json` (it stays `0.0.0`). The `commit-garcon` skill in `.claude/skills/` covers releases and how to repair one.
- **This repository is public.** Keep credentials, account routing, host paths, raw conversations and private workflow records out of code, documentation, Issues and PR descriptions. Private notes and project context belong in the ignored `.local/` folder (for example `.local/AGENTS.md`); never force-add it. `scripts/check.sh` runs `.github/scripts/check-repository-boundaries.py` to verify that.
- **Tests need no `.env` and no real accounts.** Use disposable fixtures. Never copy authentication profiles or runtime databases into a checkout or worktree.
- **Toolchain:** versions are pinned in `.mise.toml` (`mise install`). `scripts/check.sh` runs the dashboard check, tests and build, `go test ./...` and the npm package tests, and also runs the release build in dry-run mode (`scripts/release.sh 9.9.9 --dry-run` without the tests); run that command on its own to see the full package listing.
- **Development uses port 4242; the live service uses 4141.** Both development modes use real local data. Preparing or merging a PR does not restart the service: the live install is updated only by `scripts/deploy-macos.sh` on macOS or `scripts/deploy.sh` on Linux (see `README.md` under `Deploy`).
- **Worktrees and cleanup** follow [CONTRIBUTING.md](CONTRIBUTING.md#merge-and-cleanup): keep a task's worktree while its PR is open, and remove it only after the merged head is verified and its conversations are captured.
- **Product checks:** [docs/acceptance-checks.md](docs/acceptance-checks.md) defines which routing, usage, widget, privacy and release behaviors need live end-to-end evidence.
- **Skills:** `.claude/skills/` holds `install-garcon`, `update-garcon`, `commit-garcon` and `add-provider`. Follow them for those tasks.
