#!/usr/bin/env bash
# Hygiene, lint, typecheck and test. CI runs this on every PR; run it before pushing.
set -euo pipefail
cd "$(dirname "$0")/.."
if git grep -nE '^(<{7}|={7}|>{7})( |$)' -- . ':(exclude)scripts/check.sh'; then
  echo "merge conflict markers found" >&2; exit 1
fi
if git grep -nE 'BEGIN [A-Z0-9 ]*PRIVATE KEY' -- . ':(exclude)scripts/check.sh'; then
  echo "private key found" >&2; exit 1
fi
while IFS= read -r f; do
  [ -f "$f" ] && [ "$(wc -c <"$f")" -gt 25600000 ] && { echo "file over 25 MB: $f" >&2; exit 1; }
done < <(git ls-files)
python3 .github/scripts/check-repository-boundaries.py
# The Go packages embed the dashboard build, so it comes first. No package lifecycle scripts run.
(cd web && npm ci --ignore-scripts --no-audit --no-fund --loglevel=error && npm run check && npm test && npm run build >/dev/null)
go vet ./...
go test ./...
node --test npm/ai-garcon/test/*.test.js
echo "scripts/check.sh: all checks passed"
