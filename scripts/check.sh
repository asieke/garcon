#!/usr/bin/env bash
# Lint, typecheck and test, after the standard gate (scripts/gate.sh: hygiene and secret scan).
# `scripts/check.sh --secrets` runs only the gate's secret scan, before a push; `scripts/check.sh --report`
# runs everything once on the pushed head and posts the "check" status that merging requires.
# It must pass from a fresh checkout, because scripts/verify-pr.sh runs it in one.
# Toolchain: Go and Node from .mise.toml (`mise install`).
set -euo pipefail
. "$(dirname "$0")/gate.sh" "$@"
python3 .github/scripts/check-repository-boundaries.py
# The Go packages embed the dashboard build, so it comes first. No package lifecycle scripts run.
(cd web && npm ci --ignore-scripts --no-audit --no-fund --loglevel=error && npm run check && npm test && npm run build >/dev/null)
go vet ./...
go test ./...
node --test npm/ai-garcon/test/*.test.js
# The exact build a release runs (four cross-builds and a dry-run pack), without repeating the tests.
RELEASE_SKIP_TESTS=1 scripts/release.sh 9.9.9 --dry-run >/dev/null
echo "scripts/check.sh: all checks passed"
