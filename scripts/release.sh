#!/usr/bin/env bash
# Build the dashboard and the binary for every platform, then pack or publish ai-garcon.
#   scripts/release.sh 0.1.2            publish ai-garcon@0.1.2 from this machine (npm login first)
#   scripts/release.sh 0.1.2 --pack     build and write npm/ai-garcon/ai-garcon-0.1.2.tgz; publish nothing
#   scripts/release.sh 0.1.2 --dry-run  build and show what would be packed
# Merging into main runs --pack from .github/workflows/release.yml in a job without
# credentials, and a second job publishes the tarball. Publish by hand only to repair a
# release. The version in npm/ai-garcon/package.json is a placeholder; this script stamps
# the real one.
set -euo pipefail
cd "$(dirname "$0")/.."
V="${1:-}"; MODE="${2:-}"
usage() { sed -n '2,9p' "$0"; exit 2; }
[[ "$V" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || usage
case "$MODE" in ""|--pack|--dry-run) ;; *) usage ;; esac
if [ "$MODE" != --dry-run ]; then
	# A version stamp from an earlier attempt is fine; anything else must be committed first.
	[ -z "$(git status --porcelain | grep -v 'npm/.*package\.json')" ] || { echo "commit or stash changes first" >&2; exit 1; }
fi
if [ -z "$MODE" ] && [ -z "${GITHUB_ACTIONS:-}" ]; then
	npm whoami >/dev/null 2>&1 || { echo "not logged in to npm; run: npm login" >&2; exit 1; }
fi
TARGETS="linux-x64 linux-arm64 darwin-x64 darwin-arm64"
goarch() { case "$1" in x64) echo amd64 ;; arm64) echo arm64 ;; esac; }

# The dashboard first: the Go packages embed its build output, so a fresh checkout cannot even compile without it.
# Nothing in the build needs a package's lifecycle script, so none gets to run.
echo "== dashboard"; (cd web && npm ci --ignore-scripts --no-audit --no-fund --loglevel=error && npm run check && npm test && npm run build >/dev/null)
go test ./...
node --test npm/ai-garcon/test/*.test.js
# Only files from this build can end up in the package.
rm -rf npm/ai-garcon/dist npm/ai-garcon/ai-garcon-*.tgz
for t in $TARGETS; do
	os=${t%-*}; cpu=${t#*-}
	echo "== garcon $V for $t"
	CGO_ENABLED=0 GOOS=$os GOARCH=$(goarch "$cpu") go build -trimpath -ldflags "-s -w -X main.version=$V" -o "npm/ai-garcon/dist/$t/garcon" ./cmd/garcon
done
# Stamp the version for the package only; the checked-in package.json keeps its placeholder.
saved=$(mktemp); cp npm/ai-garcon/package.json "$saved"
trap 'cp "$saved" npm/ai-garcon/package.json; rm -f "$saved"' EXIT
(cd npm/ai-garcon && npm version --no-git-tag-version --allow-same-version "$V" >/dev/null)

echo "== package"
case "$MODE" in
	--dry-run) (cd npm/ai-garcon && npm pack --dry-run) ;;
	--pack) (cd npm/ai-garcon && npm pack >/dev/null); echo "packed npm/ai-garcon/ai-garcon-$V.tgz" ;;
	*)
		# Provenance needs the CI's OIDC identity; a repair publish from a laptop goes without.
		(cd npm/ai-garcon && npm publish --access public ${GITHUB_ACTIONS:+--provenance})
		echo "published ai-garcon@$V; tag it: git tag v$V && git push origin v$V" ;;
esac
