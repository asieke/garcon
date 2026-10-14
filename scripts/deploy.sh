#!/usr/bin/env bash
# Deploy the canonical checkout to origin/main. Idempotent; safe from cron.
# Refuses unless this is a clean checkout of main at CANONICAL_PATH.
set -euo pipefail
CANONICAL_PATH="$HOME/apps/garcon"
cd "$CANONICAL_PATH"
[ "$(git rev-parse --abbrev-ref HEAD)" = main ] || { echo "deploy: not on main, refusing" >&2; exit 1; }
[ -z "$(git status --porcelain)" ] || { echo "deploy: dirty tree, refusing" >&2; exit 1; }
git fetch --quiet origin main
if [ "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)" ] && [ "${1:-}" != "--force" ]; then
  exit 0
fi
git reset --hard --quiet origin/main
# Build the dashboard and binary, install ~/.local/bin/garcon and restart the service.
scripts/install.sh
# Smoke test: the restarted service answers on its live port.
for _ in $(seq 1 20); do
  curl -fsS http://127.0.0.1:4141/api/usage >/dev/null 2>&1 && break
  sleep 0.5
done
curl -fsS http://127.0.0.1:4141/api/usage >/dev/null || { echo "deploy: garcon did not answer on 4141" >&2; exit 1; }
echo "deploy: $(date -u +%FT%TZ) $(git rev-parse --short HEAD)"
