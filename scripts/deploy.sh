#!/usr/bin/env bash
# Deploy this host's part of the app: install, restart, and smoke test. Idempotent; usually run through
# scripts/sync.sh. scripts/live.sh (sourced first, identical in every project) finds this host's live
# checkout in PROJECT.md's deploy field, refuses anything unsafe, fast-forwards it to origin/main, and
# returns here only when there is something to deploy: new commits, any argument (--force deploys
# anyway), or deploy_pending. HOST, CANONICAL_PATH, and DEPLOY_FROM (the commit before this run) are set.
set -euo pipefail
# The live install is the npm release of origin/main, which the Release workflow publishes a few minutes
# after a merge: still pending while the service is not on it, so the next sync installs a release that
# was not out yet, or restarts a service that stopped answering.
deploy_pending() { scripts/deploy-macos.sh --pending; }
. "$(dirname "$0")/live.sh" "$@"
# Install the release, restart the LaunchAgent, and smoke-test that the service on 4141 reports it.
if [ "${1:-}" = --force ]; then scripts/deploy-macos.sh --force; else scripts/deploy-macos.sh; fi
