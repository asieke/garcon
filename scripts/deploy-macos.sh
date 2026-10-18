#!/usr/bin/env bash
# Deploy the release of origin/main on macOS. Idempotent; safe to rerun.
# Every merge to main publishes ai-garcon to npm and tags the merge commit v<version>, so
# this installs that exact npm version instead of building from source (no Go needed),
# refreshes the LaunchAgent, and points ~/.local/bin/garcon at the npm install so an older
# source build there cannot shadow it.
#   scripts/deploy-macos.sh           deploy when the service is behind origin/main
#   scripts/deploy-macos.sh --force   reinstall and restart even when up to date
#   scripts/deploy-macos.sh --pending exit 0 if a deploy is due (or the release is not out yet), else 1
set -euo pipefail
cd "$(dirname "$0")/.."

MODE="${1:-}"
case "$MODE" in ""|--force|--pending) ;; *) sed -n '7,9p' "$0"; exit 2 ;; esac
[ "$(uname -s)" = Darwin ] || { echo "deploy-macos: macOS only; Linux builds from source with scripts/install.sh" >&2; exit 1; }
for tool in git npm curl; do
	command -v "$tool" >/dev/null || { echo "deploy-macos: need $tool" >&2; exit 1; }
done

URL="http://127.0.0.1:4141"
SERVICE_BIN="$HOME/.local/share/garcon/bin/garcon"
LINK="$HOME/.local/bin/garcon"
NPM_BIN="$(npm prefix -g)/bin/garcon"

# The release for origin/main is the v* tag on its head commit.
head="$(git ls-remote origin refs/heads/main | cut -f1)"
[ -n "$head" ] || { echo "deploy-macos: cannot read origin/main" >&2; exit 1; }
version="$(git ls-remote --tags origin 'refs/tags/v*' | awk -v h="$head" '$1 == h { sub("refs/tags/v", "", $2); print $2 }' | sort -V | tail -n1)"
if [ -z "$version" ]; then
	[ "$MODE" != --pending ] || exit 0
	echo "deploy-macos: origin/main (${head:0:7}) has no release yet; the Release workflow tags it a few minutes after the merge (gh run list --workflow=release.yml)" >&2
	exit 1
fi

installed="$("$SERVICE_BIN" version 2>/dev/null | awk '{print $2}' || true)"
if [ "$MODE" != --force ] && [ "$installed" = "$version" ] && [ "$(readlink "$LINK" 2>/dev/null)" = "$NPM_BIN" ] &&
	curl -fsS "$URL/api/usage" >/dev/null 2>&1; then
	[ "$MODE" != --pending ] || exit 1
	echo "deploy-macos: already at $version"
	exit 0
fi
[ "$MODE" != --pending ] || exit 0

echo "deploy-macos: ${installed:-no service binary} -> $version (origin/main ${head:0:7})"
npm install --global --no-audit --no-fund --loglevel=error "ai-garcon@$version"
got="$("$NPM_BIN" version | awk '{print $2}')"
[ "$got" = "$version" ] || { echo "deploy-macos: $NPM_BIN reports $got, expected $version; reinstall with npm install -g ai-garcon@$version --force" >&2; exit 1; }

# A source build in ~/.local/bin comes before npm's bin directory on PATH; replace it with a
# link to the npm shim so `garcon` and `garcon update` run the deployed release.
mkdir -p "$(dirname "$LINK")"
if [ -e "$LINK" ] && [ "$(readlink "$LINK" 2>/dev/null)" != "$NPM_BIN" ]; then
	echo "deploy-macos: replacing $LINK ($("$LINK" version 2>/dev/null || echo unknown)) with a link to the npm install"
fi
ln -sfn "$NPM_BIN" "$LINK"

# Copies the release binary to the service location, rewrites the LaunchAgent and restarts it.
"$NPM_BIN" service install

for _ in $(seq 1 20); do
	curl -fsS "$URL/api/usage" >/dev/null 2>&1 && break
	sleep 0.5
done
curl -fsS "$URL/api/usage" >/dev/null || { echo "deploy-macos: garcon did not answer on 4141; see ~/Library/Logs/garcon.log" >&2; exit 1; }
curl -fsS "$URL/api/config" | grep -q "\"version\":\"$version\"" || { echo "deploy-macos: the running service does not report $version" >&2; exit 1; }
echo "deploy-macos: $(date -u +%FT%TZ) $version ${head:0:7}"
