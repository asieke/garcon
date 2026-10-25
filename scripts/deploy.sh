#!/usr/bin/env bash
# Deploys Garcon on the Mac from its live checkout: installs the npm release of origin/main, restarts the LaunchAgent
# and checks the service reports it (scripts/deploy-macos.sh). Run `scripts/deploy.sh` (README, Deploy);
# `scripts/deploy.sh --force` reinstalls even when the service is already on that release.
set -euo pipefail
cd "$(dirname "$0")/.."
# In a checkout with the ignored scripts/deploy.local.sh (the Mac's development checkout), that deploys to another host.
mac="$(git rev-parse --path-format=absolute --git-common-dir)/.."
[ ! -x "$mac/scripts/deploy.local.sh" ] || exec "$mac/scripts/deploy.local.sh" "$@"
# Deploys only what is merged: a clean main, fast-forwarded to origin/main, runs the version that arrived.
[ "$(git branch --show-current)" = main ] && [ -z "$(git status --porcelain)" ] \
  || { echo "deploy: needs a clean checkout of main" >&2; exit 1; }
if [ -z "${DEPLOY_UPDATED:-}" ]; then
  DEPLOY_FROM=$(git rev-parse HEAD)
  git fetch --quiet origin main
  git merge --quiet --ff-only --no-overwrite-ignore origin/main
  [ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] \
    || { echo "deploy: main has commits that are not on origin/main; deploy only what is merged" >&2; exit 1; }
  DEPLOY_UPDATED=1 DEPLOY_FROM=$DEPLOY_FROM exec scripts/deploy.sh "$@"
fi
unset DEPLOY_UPDATED; export DEPLOY_FROM   # the commit that was here before this deploy
# Every attempt appends `<UTC time> <commit> ok|failed` to .git/deploys, which GitChu reads.
trap 'rc=$?; echo "$(date -u +%FT%TZ) $(git rev-parse HEAD) $([ $rc -eq 0 ] && echo ok || echo failed)" >>"$(git rev-parse --git-path deploys)"' EXIT

case "$*" in ""|--force) ;; *) echo "usage: scripts/deploy.sh [--force]" >&2; exit 2 ;; esac
# A release appears a few minutes after its merge; until then this stops and says so, and a later run installs it.
scripts/deploy-macos.sh "$@"
