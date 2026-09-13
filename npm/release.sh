#!/usr/bin/env bash
# Build the dashboard and the binary for every platform, then publish the npm packages.
#   npm/release.sh 0.1.0            publish ai-garcon@0.1.0 and its four platform packages
#   npm/release.sh 0.1.0 --dry-run  build and show what would be published
# Needs: go, npm (logged in: `npm login`), a clean checkout on main.
set -euo pipefail
cd "$(dirname "$0")/.."
V="${1:-}"; DRY="${2:-}"
[[ "$V" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { sed -n '2,5p' "$0"; exit 2; }
[ "$DRY" = "" ] || [ "$DRY" = --dry-run ] || { sed -n '2,5p' "$0"; exit 2; }
if [ -z "$DRY" ]; then
	[ -z "$(git status --porcelain)" ] || { echo "commit or stash changes first" >&2; exit 1; }
	npm whoami >/dev/null 2>&1 || { echo "not logged in to npm; run: npm login" >&2; exit 1; }
fi
TARGETS="linux-x64 linux-arm64 darwin-x64 darwin-arm64"
goarch() { case "$1" in x64) echo amd64 ;; arm64) echo arm64 ;; esac; }

echo "== dashboard"; (cd web && npm ci --no-audit --no-fund --loglevel=error && npm run build >/dev/null)
for t in $TARGETS; do
	os=${t%-*}; cpu=${t#*-}
	echo "== garcon $V for $t"
	CGO_ENABLED=0 GOOS=$os GOARCH=$(goarch "$cpu") go build -trimpath -ldflags "-s -w -X main.version=$V" -o "npm/platforms/ai-garcon-$t/bin/garcon" .
	(cd "npm/platforms/ai-garcon-$t" && npm version --no-git-tag-version --allow-same-version "$V" >/dev/null)
done
(cd npm/ai-garcon && npm version --no-git-tag-version --allow-same-version "$V" >/dev/null && node -e '
	const fs = require("fs"); const p = JSON.parse(fs.readFileSync("package.json"));
	for (const k of Object.keys(p.optionalDependencies)) p.optionalDependencies[k] = process.argv[1];
	fs.writeFileSync("package.json", JSON.stringify(p, null, 2) + "\n");' "$V")

echo "== publish"
for t in $TARGETS; do (cd "npm/platforms/ai-garcon-$t" && npm publish --access public ${DRY:+--dry-run}); done
(cd npm/ai-garcon && npm publish --access public ${DRY:+--dry-run})
if [ -z "$DRY" ]; then
	git add npm/*/package.json npm/platforms/*/package.json
	git commit -q -m "ai-garcon $V" && git tag "v$V"
	echo "published ai-garcon@$V; push with: git push && git push --tags"
fi
