#!/usr/bin/env bash
# Connect this machine's garcon to a Supabase project for cross-device sync.
#   ./connect-supabase.sh --name "work laptop"                  create a project, apply the schema, connect
#   ./connect-supabase.sh --name "home desktop" --project-ref REF   reuse an existing project
# Options: --org-id ID (when you belong to several), --region R (default us-east-1),
#          --url http://127.0.0.1:4141 (a garcon on another port)
# Needs the Supabase CLI (logged in: `supabase login`), jq and curl. The secret key is
# fetched by the CLI and piped straight into garcon: it is never printed.
set -euo pipefail
cd "$(dirname "$0")"

NAME="" REF="" ORG="" REGION="us-east-1" GARCON="http://127.0.0.1:4141"
while [ $# -gt 0 ]; do
	case "$1" in
		--name) NAME="$2"; shift 2 ;;
		--project-ref) REF="$2"; shift 2 ;;
		--org-id) ORG="$2"; shift 2 ;;
		--region) REGION="$2"; shift 2 ;;
		--url) GARCON="$2"; shift 2 ;;
		*) sed -n '2,8p' "$0"; exit 2 ;;
	esac
done
[ -n "$NAME" ] || { echo "--name is required: the label for this machine in the dashboard" >&2; sed -n '2,8p' "$0"; exit 2; }

for tool in supabase jq curl; do
	command -v "$tool" >/dev/null || { echo "need $tool" >&2; exit 1; }
done
curl -sf "$GARCON/api/settings" >/dev/null || { echo "garcon is not answering at $GARCON (./install.sh --service)" >&2; exit 1; }
orgs="$(supabase orgs list -o json 2>/dev/null)" || { echo "not logged in to the Supabase CLI; run: supabase login" >&2; exit 1; }

if [ -z "$REF" ]; then
	if [ -z "$ORG" ]; then
		count="$(echo "$orgs" | jq 'length')"
		if [ "$count" = 1 ]; then
			ORG="$(echo "$orgs" | jq -r '.[0].id')"
		else
			echo "you belong to $count organisations; pass --org-id with one of:" >&2
			echo "$orgs" | jq -r '.[] | "  \(.id)  \(.name)"' >&2
			exit 1
		fi
	fi
	password="$(openssl rand -base64 24 | tr -d '/+=' | cut -c1-24)"
	echo "creating project 'garcon' in $REGION…"
	out="$(supabase projects create garcon --org-id "$ORG" --region "$REGION" --db-password "$password" -o json)"
	REF="$(echo "$out" | jq -r '.id // .ref // .project_ref // empty')"
	[ -n "$REF" ] || { echo "could not read the project ref from: $out" >&2; exit 1; }
	echo "database password (garcon never needs it; keep it in your password manager): $password"
	printf 'waiting for %s to become healthy' "$REF"
	for _ in $(seq 1 60); do
		status="$(supabase projects list -o json | jq -r --arg r "$REF" '.[] | select(.id == $r or .ref == $r) | .status')"
		[ "$status" = ACTIVE_HEALTHY ] && break
		printf .; sleep 5
	done
	echo
	[ "$status" = ACTIVE_HEALTHY ] || { echo "project is still '$status'; rerun with --project-ref $REF once it is ready" >&2; exit 1; }
fi

echo "applying supabase/garcon_usage.sql to $REF…"
supabase db query --linked --project-ref "$REF" -f supabase/garcon_usage.sql >/dev/null

echo "fetching the secret key and handing it to garcon…"
response="$(supabase projects api-keys --project-ref "$REF" --reveal -o json \
	| jq --arg name "$NAME" --arg url "https://$REF.supabase.co" \
		'[.[] | .api_key // empty | select(startswith("sb_secret_"))][0] as $key
		 | if $key == null then error("no sb_secret_ key found; create one under Project Settings > API Keys")
		   else {sync_enabled: true, device_name: $name, url: $url, key: $key} end' \
	| curl -s -X PUT -H 'Content-Type: application/json' -d @- -w '\n%{http_code}' "$GARCON/api/settings")"
code="${response##*$'\n'}"
if [ "$code" != 200 ]; then
	echo "garcon refused the settings (HTTP $code): ${response%$'\n'*}" >&2
	exit 1
fi

cat <<MSG
connected: sync is ON for "$NAME" → https://$REF.supabase.co
  progress: $GARCON/?view=settings (or curl $GARCON/api/settings)
  another machine: ./connect-supabase.sh --name "<its label>" --project-ref $REF
                   or paste the URL and the sb_secret_ key (Project Settings > API Keys)
                   into Settings > Sync in its dashboard.
MSG
