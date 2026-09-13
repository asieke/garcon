---
name: commit-garcon
description: Commit, push, open and merge pull requests in the Garcon repository. Merging into main publishes a new ai-garcon version to npm automatically, so this skill also covers how versions are chosen, how to check a release landed, and how to repair one. Use when asked to commit, push, merge, ship, release, publish or bump the version of Garcon.
---

# Committing and releasing Garcon

`main` is PR-only (a repository ruleset), and **every merge into `main` is a release**:
`.github/workflows/release.yml` tests, builds the dashboard and four platform binaries,
publishes `ai-garcon` to npm, tags the merge commit `vX.Y.Z` and creates a GitHub
release. There is nothing to bump by hand.

## How the version is chosen

- The repository does not store a version. `npm/ai-garcon/package.json` says `0.0.0`
  on purpose; `scripts/release.sh` stamps the real number at publish time and restores
  the placeholder afterwards. Never commit a real version there.
- The workflow takes the highest of the latest `v*` tag and `npm view ai-garcon version`
  and bumps the patch. A merge whose commit message contains `[minor]` or `[major]`
  bumps that part instead: put the marker in the PR title, since GitHub copies the title
  into the merge commit.
- The binary carries the version (`-X main.version`), so the dashboard sidebar shows
  `vX.Y.Z` for npm installs and `git describe` output for source builds.

Merging a docs-only or skill-only change still publishes a release with the same binary
behavior; that is by design (one rule, no forgotten releases). Batch small changes into
one PR when that matters.

## 1. Commit

```sh
git status                      # know what is being committed; never commit unrelated files
git checkout -b <topic>         # from main; use the existing branch if already on one
go test ./... && (cd web && npm run check && npm run build >/dev/null)
git add <files>
git commit -m "<imperative summary>"
```

Do not commit `internal/dashboard/build/` (gitignored) or `npm/ai-garcon/dist/`. Docs
live in `docs/` and `README.md`; update them in the same commit when behavior visible to
users changes. `scripts/release.sh X.Y.Z --dry-run` runs the exact build the workflow
will run, when a change touches the build, the npm package or the workflow itself.

## 2. Push and merge (this is the release)

```sh
git push -u origin <topic>
gh pr create --fill              # or --title/--body; add [minor] or [major] to the title if the bump should not be a patch
gh pr merge --merge --delete-branch <pr>   # merge commit, like the rest of the history
git checkout main && git pull --ff-only
```

Ask before merging unless the user already said "merge" or "ship", because the merge
publishes. Then watch the run:

```sh
gh run watch --exit-status        # the Release workflow for the merge commit
npm view ai-garcon version        # the new version
git fetch --tags && git describe --tags origin/main
```

A running source install picks the change up with `scripts/install.sh --update` (see the
`update-garcon` skill); npm installs with `garcon update`.

## If the release fails

Read the failed step in `gh run view --log-failed`.

- **Publish rejected (E404, ENEEDAUTH, 403)**: npm trusted publishing is not set up or
  points at the wrong workflow. On npmjs.com, package `ai-garcon` > Settings > Trusted
  Publisher must list repository `asieke/garcon` and workflow file `release.yml`. Only
  the npm account owner can fix that; then re-run the workflow (`gh run rerun <id>`).
- **Tests or build failed**: nothing was published or tagged. Fix on a branch and merge
  again; the next run picks the same next version.
- **Published but tag missing** (the run failed after `npm publish`): tag the merge commit
  by hand so the next run does not reuse the number: `git tag vX.Y.Z <sha> && git push
  origin vX.Y.Z`. The version step also reads npm, so a missing tag cannot cause a
  duplicate publish, only a missing tag and release page.
- **Manual publish needed** (workflow unavailable): `npm login`, then from a clean
  `main` checkout `scripts/release.sh X.Y.Z`, then tag and push the tag as above.
- **Two merges close together**: the workflow runs one release at a time
  (`concurrency: release`), so the second waits and gets the next number.
