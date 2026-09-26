#!/usr/bin/env bash
# Build and run the dashboard, API, and harness proxy in one Garcon process.
set -euo pipefail
cd "$(dirname "$0")/.."

for tool in go npm; do
	command -v "$tool" >/dev/null || { echo "need $tool (Go 1.27+, Node 22+; 'mise install' provides both)" >&2; exit 1; }
done

(cd web && npm run build)
go build -o garcon ./cmd/garcon
echo "Starting Garcon dev at http://127.0.0.1:4141 (dashboard, API, and harness proxy)."
echo "Stop the installed Garcon before starting dev; both use the same port."
exec ./garcon -listen 127.0.0.1:4141
