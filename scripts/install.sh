#!/usr/bin/env bash
# Build garcon from source and install it to ~/.local/bin/garcon.
#   scripts/install.sh              build + install; restarts the service if one is running
#   scripts/install.sh --service    also run it in the background now and at every login
#   scripts/install.sh --update     pull the latest main, then build + install + restart
#   scripts/install.sh --uninstall  stop and remove the service, the binary and the settings file
#                             (which holds the Supabase key, if sync was set up)
# Prefer `npm i -g ai-garcon && garcon service install` unless you want to build from source.
set -euo pipefail
cd "$(dirname "$0")/.."

BIN="$HOME/.local/bin/garcon"
URL="http://127.0.0.1:4141"
MODE="${1:-}"

case "$MODE" in
	""|--service|--update|--uninstall) ;;
	*) sed -n '2,8p' "$0"; exit 2 ;;
esac

if [ "$MODE" = --uninstall ]; then
	if [ -x "$BIN" ]; then "$BIN" service uninstall; fi
	rm -f "$BIN" "$HOME/.config/garcon/config.json"
	echo "garcon removed, with ~/.config/garcon/config.json (usage log and sync state kept in ~/.local/share/garcon)"
	exit 0
fi

if [ "$MODE" = --update ]; then
	command -v git >/dev/null || { echo "need git" >&2; exit 1; }
	git rev-parse --is-inside-work-tree >/dev/null 2>&1 || { echo "not a git checkout; clone github.com/asieke/garcon and run from there" >&2; exit 1; }
	if [ -n "$(git status --porcelain)" ]; then
		echo "uncommitted changes in $(pwd); commit or stash them before updating" >&2; exit 1
	fi
	branch="$(git rev-parse --abbrev-ref HEAD)"
	[ "$branch" = main ] || { echo "switching from $branch to main"; git checkout -q main; }
	before="$(git rev-parse --short HEAD)"
	git pull -q --ff-only origin main
	after="$(git rev-parse --short HEAD)"
	if [ "$before" = "$after" ]; then
		echo "already up to date at $after; rebuilding anyway"
	else
		echo "updated $before -> $after:"
		git --no-pager log --oneline "$before..$after" | sed 's/^/  /'
	fi
	exec "$0" # the freshly pulled script does the build, in case its steps changed
fi

for tool in go npm; do
	command -v "$tool" >/dev/null || { echo "need $tool (Go 1.27+, Node 22+; 'mise install' provides both)" >&2; exit 1; }
done

(cd web && npm ci --no-audit --no-fund --loglevel=error && npm run build >/dev/null)
go build -ldflags "-X main.version=$(git describe --tags --always 2>/dev/null || echo source)" -o garcon ./cmd/garcon
mkdir -p "$(dirname "$BIN")"
install -m 755 garcon "$BIN"
echo "installed $BIN"

if [ "$MODE" = --service ]; then
	"$BIN" service install
else
	"$BIN" service restart
fi

for _ in 1 2 3 4 5 6 7 8 9 10; do
	if curl -sf "$URL/api/usage" >/dev/null 2>&1; then
		echo "garcon is up: $URL"
		exit 0
	fi
	sleep 0.3
done
[ "$MODE" = --service ] && { echo "garcon did not answer at $URL" >&2; exit 1; }
echo "not running; start it with: $BIN   (or scripts/install.sh --service)"
