#!/usr/bin/env bash
# Preview the dashboard against the live local service without another writer.
set -euo pipefail
cd "$(dirname "$0")/.."

for tool in go npm; do
	command -v "$tool" >/dev/null || { echo "need $tool (Go 1.27+, Node 22+; 'mise install' provides both)" >&2; exit 1; }
done

(cd web && npm run build)
echo "Starting the development dashboard at http://127.0.0.1:4242."
echo "It uses the running service on 4141; settings changes affect that service."
exec go run ./scripts/dev -backend "${GARCON_BACKEND_URL:-http://127.0.0.1:4141}"
