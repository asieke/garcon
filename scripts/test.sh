#!/usr/bin/env bash
# Every check a change needs before it merges: the repository boundaries, the dashboard's check, tests and build,
# go vet and go test, the npm package tests, and the release build in dry-run mode. It passes from a fresh checkout.
# Toolchain: Go and Node from .mise.toml (`mise install`).
set -euo pipefail
cd "$(dirname "$0")/.."
python3 .github/scripts/check-repository-boundaries.py
# The Go packages embed the dashboard build, so it comes first. No package lifecycle scripts run.
(cd web && npm ci --ignore-scripts --no-audit --no-fund --loglevel=error && npm run check && npm test && npm run build >/dev/null)
go vet ./...
go test ./...
node --test npm/ai-garcon/test/*.test.js
# The exact build a release runs (four cross-builds and a dry-run pack), without repeating the tests.
RELEASE_SKIP_TESTS=1 scripts/release.sh 9.9.9 --dry-run >/dev/null
echo "scripts/test.sh: all checks passed"
