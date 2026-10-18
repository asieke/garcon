#!/usr/bin/env bash
# Deploy this host's part of the app: install, restart, and smoke test. Idempotent; usually run through
# scripts/sync.sh. scripts/live.sh (sourced first, identical in every project) finds this host's live
# checkout in PROJECT.md's deploy field, refuses anything unsafe, fast-forwards it to origin/main, and
# returns here only when there is something to deploy: new commits, or any argument (--force deploys
# anyway). HOST, CANONICAL_PATH, and DEPLOY_FROM (the commit before this run) are set.
set -euo pipefail
. "$(dirname "$0")/live.sh" "$@"
# Build the dashboard and binary, install ~/.local/bin/garcon and restart the service.
scripts/install.sh
# Smoke test: the restarted service answers on its live port.
for _ in $(seq 1 20); do
  curl -fsS http://127.0.0.1:4141/api/usage >/dev/null 2>&1 && break
  sleep 0.5
done
curl -fsS http://127.0.0.1:4141/api/usage >/dev/null || { echo "deploy: garcon did not answer on 4141" >&2; exit 1; }
