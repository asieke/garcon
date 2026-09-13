#!/usr/bin/env bash
# Build the dashboard and the binary for every platform, then publish ai-garcon to npm.
#   scripts/release.sh 0.1.2            publish ai-garcon@0.1.2 (one package, four platform binaries inside)
#   scripts/release.sh 0.1.2 --dry-run  build and show what would be published
# Merging into main runs this from .github/workflows/release.yml with the next patch
# version, then tags the commit; run it by hand only to repair a release (`npm login` first).
# The version in npm/ai-garcon/package.json is a placeholder; this script stamps the real one.
set -euo pipefail
cd "$(dirname "$0")/.."
V="${1:-}"; DRY="${2:-}"
[[ "$V" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { sed -n '2,7p' "$0"; exit 2; }
[ "$DRY" = "" ] || [ "$DRY" = --dry-run ] || { sed -n '2,7p' "$0"; exit 2; }
if [ -z "$DRY" ]; then
	# A version stamp from an earlier attempt is fine; anything else must be committed first.
	[ -z "$(git status --porcelain | grep -v 'npm/.*package\.json')" ] || { echo "commit or stash changes first" >&2; exit 1; }
	# GitHub Actions publishes with trusted publishing (OIDC), so there is no login to check there.
	[ -n "${GITHUB_ACTIONS:-}" ] || npm whoami >/dev/null 2>&1 || { echo "not logged in to npm; run: npm login" >&2; exit 1; }
fi
TARGETS="linux-x64 linux-arm64 darwin-x64 darwin-arm64"
goarch() { case "$1" in x64) echo amd64 ;; arm64) echo arm64 ;; esac; }

# The dashboard first: the Go packages embed its build output, so a fresh checkout cannot even compile without it.
echo "== dashboard"; (cd web && npm ci --no-audit --no-fund --loglevel=error && npm run check && npm run build >/dev/null)
go test ./...
node --test npm/ai-garcon/test/*.test.js
for t in $TARGETS; do
	os=${t%-*}; cpu=${t#*-}
	echo "== garcon $V for $t"
	CGO_ENABLED=0 GOOS=$os GOARCH=$(goarch "$cpu") go build -trimpath -ldflags "-s -w -X main.version=$V" -o "npm/ai-garcon/dist/$t/garcon" ./cmd/garcon
done
# Stamp the version for the publish only; the checked-in package.json keeps its placeholder.
saved=$(mktemp); cp npm/ai-garcon/package.json "$saved"
trap 'cp "$saved" npm/ai-garcon/package.json; rm -f "$saved"' EXIT
(cd npm/ai-garcon && npm version --no-git-tag-version --allow-same-version "$V" >/dev/null)

echo "== package"
if [ -n "$DRY" ]; then
	(cd npm/ai-garcon && npm pack --dry-run)
else
	(cd npm/ai-garcon && npm publish --access public)
	echo "published ai-garcon@$V; tag it: git tag v$V && git push origin v$V"
fi
