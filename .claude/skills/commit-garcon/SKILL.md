---
name: commit-garcon
description: Commit, push, open and merge pull requests in the Garcon repository, and cut releases so the version in npm/ai-garcon/package.json, the git tag and the published ai-garcon npm package always agree. Use when asked to commit, push, merge, ship, release, publish or bump the version of Garcon.
---

# Committing and releasing Garcon

`main` is PR-only: every change lands as a branch, a pull request and a merge. There is
one version number and it lives in three places that must never drift apart:

| Where | What |
|---|---|
| `npm/ai-garcon/package.json` `"version"` | the last version published to npm |
| git tag `vX.Y.Z` on `main` | the commit that was published |
| `ai-garcon@X.Y.Z` on npm | the binaries the dashboard reports as `vX.Y.Z` |

The dashboard reads the version from the binary (`-X main.version`, stamped by
`scripts/release.sh` for npm builds and by `git describe --tags` for source builds), so
the sidebar shows exactly what was published. Only `scripts/release.sh` may change the
version in `package.json`. Never bump it by hand in a feature commit: a bump without a
publish leaves npm behind the repository, and a publish without a bump is impossible
because the script refuses a non-semver argument.

## 1. Commit

```sh
git status                      # know what is being committed; never commit unrelated files
git checkout -b <topic>         # from main; use the existing branch if already on one
go test ./... && (cd web && npm run check && npm run build >/dev/null)
git add <files>
git commit -m "<imperative summary>"
```

Do not commit `internal/dashboard/build/` (gitignored, rebuilt by `scripts/install.sh`
and `scripts/release.sh`) or `npm/ai-garcon/dist/`. Docs live in `docs/` and
`README.md`; update them in the same commit when behavior visible to users changes.

## 2. Push and merge

```sh
git push -u origin <topic>
gh pr create --fill              # or --title/--body; end the body with the attribution line if the harness asks for one
gh pr merge --merge --delete-branch <pr>   # merge commit, like the rest of the history
git checkout main && git pull --ff-only
```

Ask before merging unless the user already said "merge" or "ship". After a merge, a
running proxy built from source picks the change up with `scripts/install.sh --update`
(see the `update-garcon` skill); npm installs only see it after step 3.

## 3. Release (bump + publish, in one step)

Decide whether the merged change should reach npm users now. Anything that alters the
binary, the dashboard or the npm package should; docs-only or skill-only changes need
not. If yes, from a clean checkout of `main`:

```sh
git checkout main && git pull --ff-only
V=$(node -p "require('./npm/ai-garcon/package.json').version")   # current, e.g. 0.1.1
scripts/release.sh <next> --dry-run     # next = V with the patch bumped unless the user asked for minor/major
scripts/release.sh <next>               # needs `npm login`; runs tests, builds four platforms, publishes
```

The script sets `package.json` to `<next>`, commits `ai-garcon <next>` and tags `v<next>`
locally. Those still have to reach GitHub through a PR, because `main` is PR-only:

```sh
git checkout -b release-<next>
git push -u origin release-<next> && git push origin "v<next>"
gh pr create --title "ai-garcon <next>" --body "Version bump for the published package."
gh pr merge --merge --delete-branch
git checkout main && git pull --ff-only
```

Verify: `npm view ai-garcon version` prints `<next>`, `git describe --tags` on `main`
prints `v<next>`, and `node -p "require('./npm/ai-garcon/package.json').version"` agrees.
Then `garcon update` on an npm install shows `v<next>` in the dashboard sidebar.

## If something is off

- `package.json` is ahead of `npm view ai-garcon version`: someone bumped without
  publishing. Publish that version (`scripts/release.sh <that version>`) rather than
  bumping again; `--allow-same-version` in the script makes this safe.
- Tag missing on `main`: `git tag v<version> <bump commit> && git push origin v<version>`.
- `release.sh` says "commit or stash changes first": only a `package.json` bump from an
  earlier attempt is tolerated; commit or stash everything else.
- Publish failed after building: fix the cause and rerun the script with the same
  version; nothing was committed or tagged yet.
