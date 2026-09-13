#!/usr/bin/env bash
# Build garcon and install it to ~/.local/bin/garcon.
#   ./install.sh              build + install; restarts the service if one is running
#   ./install.sh --service    also run it in the background now and at every login
#                             (systemd user unit on Linux, LaunchAgent on macOS)
#   ./install.sh --uninstall  stop and remove the service and the binary
set -euo pipefail
cd "$(dirname "$0")"

BIN="$HOME/.local/bin/garcon"
URL="http://127.0.0.1:4141"
OS="$(uname -s)"
UNIT="$HOME/.config/systemd/user/garcon.service"
PLIST="$HOME/Library/LaunchAgents/dev.garcon.plist"
MODE="${1:-}"

case "$MODE" in
	""|--service|--uninstall) ;;
	*) sed -n '2,6p' "$0"; exit 2 ;;
esac

if [ "$MODE" = --uninstall ]; then
	if [ "$OS" = Linux ]; then
		systemctl --user disable --now garcon 2>/dev/null || true
		rm -f "$UNIT"; systemctl --user daemon-reload
	elif [ "$OS" = Darwin ]; then
		launchctl bootout "gui/$(id -u)" "$PLIST" 2>/dev/null || true
		rm -f "$PLIST"
	fi
	rm -f "$BIN"
	echo "garcon removed (usage log kept at ~/.local/share/garcon/usage.jsonl)"
	exit 0
fi

for tool in go npm; do
	command -v "$tool" >/dev/null || { echo "need $tool (Go 1.27+, Node 22+; 'mise install' provides both)" >&2; exit 1; }
done

(cd web && npm ci --no-audit --no-fund --loglevel=error && npm run build >/dev/null)
go build -o garcon .
mkdir -p "$(dirname "$BIN")"
install -m 755 garcon "$BIN"
echo "installed $BIN"

running=false
if [ "$OS" = Linux ]; then
	systemctl --user is-active --quiet garcon 2>/dev/null && running=true
elif [ "$OS" = Darwin ]; then
	launchctl print "gui/$(id -u)/dev.garcon" >/dev/null 2>&1 && running=true
fi

if [ "$MODE" = --service ]; then
	if [ "$OS" = Linux ]; then
		mkdir -p "$(dirname "$UNIT")"
		install -m 644 garcon.service "$UNIT"
		systemctl --user daemon-reload
		systemctl --user enable --now garcon
		systemctl --user restart garcon
		echo "systemd user unit 'garcon' enabled (systemctl --user status garcon; journalctl --user -u garcon)"
	elif [ "$OS" = Darwin ]; then
		mkdir -p "$(dirname "$PLIST")" "$HOME/Library/Logs"
		cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
	<key>Label</key><string>dev.garcon</string>
	<key>ProgramArguments</key><array><string>$BIN</string></array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>StandardOutPath</key><string>$HOME/Library/Logs/garcon.log</string>
	<key>StandardErrorPath</key><string>$HOME/Library/Logs/garcon.log</string>
</dict></plist>
EOF
		launchctl bootout "gui/$(id -u)" "$PLIST" 2>/dev/null || true
		launchctl bootstrap "gui/$(id -u)" "$PLIST"
		echo "LaunchAgent dev.garcon loaded (launchctl print gui/\$(id -u)/dev.garcon; log: ~/Library/Logs/garcon.log)"
	else
		echo "no service support for $OS; run $BIN yourself" >&2
	fi
elif [ "$running" = true ]; then
	if [ "$OS" = Linux ]; then systemctl --user restart garcon; else launchctl kickstart -k "gui/$(id -u)/dev.garcon"; fi
	echo "service restarted with the new binary"
fi

for _ in 1 2 3 4 5 6 7 8 9 10; do
	if curl -sf "$URL/api/usage" >/dev/null 2>&1; then
		echo "garcon is up: $URL"
		exit 0
	fi
	sleep 0.3
done
[ "$MODE" = --service ] || [ "$running" = true ] && { echo "garcon did not answer at $URL" >&2; exit 1; }
echo "not running; start it with: $BIN   (or ./install.sh --service)"
