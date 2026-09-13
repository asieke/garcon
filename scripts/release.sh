#!/usr/bin/env bash
# Build the dashboard and the binary for every platform, then publish the npm packages.
#   scripts/release.sh 0.1.0            publish ai-garcon@0.1.0 (one package, four platform binaries inside)
#   scripts/release.sh 0.1.0 --dry-run  build and show what would be published
# Needs: go, npm (logged in: `npm login`), a clean checkout on main.
set -euo pipefail
cd "$(dirname "$0")/.."
V="${1:-}"; DRY="${2:-}"
[[ "$V" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { sed -n '2,5p' "$0"; exit 2; }
[ "$DRY" = "" ] || [ "$DRY" = --dry-run ] || { sed -n '2,5p' "$0"; exit 2; }
if [ -z "$DRY" ]; then
	# Version bumps from an earlier attempt are fine; anything else must be committed first.
	[ -z "$(git status --porcelain | grep -v 'npm/.*package\.json')" ] || { echo "commit or stash changes first" >&2; exit 1; }
	npm whoami >/dev/null 2>&1 || { echo "not logged in to npm; run: npm login" >&2; exit 1; }
fi
TARGETS="linux-x64 linux-arm64 darwin-x64 darwin-arm64"
goarch() { case "$1" in x64) echo amd64 ;; arm64) echo arm64 ;; esac; }

go test ./...
node --test npm/ai-garcon/test/*.test.js
echo "== dashboard"; (cd web && npm ci --no-audit --no-fund --loglevel=error && npm run check && npm run build >/dev/null)
for t in $TARGETS; do
	os=${t%-*}; cpu=${t#*-}
	echo "== garcon $V for $t"
	CGO_ENABLED=0 GOOS=$os GOARCH=$(goarch "$cpu") go build -trimpath -ldflags "-s -w -X main.version=$V" -o "npm/ai-garcon/dist/$t/garcon" ./cmd/garcon
done
(cd npm/ai-garcon && npm version --no-git-tag-version --allow-same-version "$V" >/dev/null)

echo "== package"
if [ -n "$DRY" ]; then
 (cd npm/ai-garcon && npm pack --dry-run)
else
 (cd npm/ai-garcon && npm publish --access public)
fi
if [ -z "$DRY" ]; then
	git add npm/ai-garcon/package.json
	git diff --cached --quiet || git commit -q -m "ai-garcon $V"
	git tag -f "v$V" >/dev/null
	echo "published ai-garcon@$V; the version-bump commit and tag v$V are local: git push --tags, then open a PR for the commit (main is PR-only)"
fi
