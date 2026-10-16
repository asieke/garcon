#!/usr/bin/env bash
# Deploy the canonical checkout to origin/main. Idempotent; usually run through scripts/sync.sh.
# `deploy.sh --path` prints CANONICAL_PATH, which sync.sh uses to recognize the live checkout.
# Refuses unless this is a clean checkout of main at CANONICAL_PATH with no commits missing from
# origin/main, so it only ever fast-forwards and never discards work.
set -euo pipefail
CANONICAL_PATH="$HOME/apps/garcon"
[ "${1:-}" != --path ] || { echo "$CANONICAL_PATH"; exit 0; }
cd "$CANONICAL_PATH"
[ "$(git rev-parse --abbrev-ref HEAD)" = main ] || { echo "deploy: not on main, refusing" >&2; exit 1; }
[ -z "$(git status --porcelain)" ] || { echo "deploy: dirty tree, refusing" >&2; exit 1; }
git fetch --quiet origin main
if [ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] && [ "${1:-}" != "--force" ]; then
  exit 0
fi
# --ff-only alone succeeds when HEAD is ahead of origin/main, so also require HEAD to be an ancestor.
git merge-base --is-ancestor HEAD origin/main && git merge --ff-only --quiet origin/main || { echo "deploy: local commits not on origin/main, refusing; push them to a branch and open a PR" >&2; exit 1; }
# Run the rest from the version of this script that just arrived.
[ -n "${DEPLOY_UPDATED:-}" ] || DEPLOY_UPDATED=1 exec "$CANONICAL_PATH/scripts/deploy.sh" --force
# Build the dashboard and binary, install ~/.local/bin/garcon and restart the service.
scripts/install.sh
# Smoke test: the restarted service answers on its live port.
for _ in $(seq 1 20); do
  curl -fsS http://127.0.0.1:4141/api/usage >/dev/null 2>&1 && break
  sleep 0.5
done
curl -fsS http://127.0.0.1:4141/api/usage >/dev/null || { echo "deploy: garcon did not answer on 4141" >&2; exit 1; }
echo "deploy: $(date -u +%FT%TZ) $(git rev-parse --short HEAD)"
