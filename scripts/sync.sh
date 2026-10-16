#!/usr/bin/env bash
# Sync to origin/main, then optionally run a command: scripts/sync.sh [command...]
# Live checkout (CANONICAL_PATH in deploy.sh): runs deploy.sh; if that refuses, exits 1 and runs nothing.
# Any other checkout: reports how far it is behind origin/main. Never stashes, resets, or deletes work.
set -euo pipefail
here="$(cd "$(dirname "$0")/.." && pwd -P)"
cd "$here"
live="$(cd "$(scripts/deploy.sh --path)" 2>/dev/null && pwd -P || true)"
if [ "$here" = "$live" ]; then
  if ! out=$(scripts/deploy.sh 2>&1); then
    printf '%s\n' "$out" "sync: REFUSED: $here could not be synced to origin/main. STOP: do not run, edit, stash, reset, commit, or delete anything here; report this output to the owner." >&2
    git status --short --branch >&2
    exit 1
  fi
  [ -z "$out" ] || echo "$out"
  echo "sync: $here is at origin/main ($(git rev-parse --short HEAD)). OK to proceed."
elif git fetch --quiet origin main 2>/dev/null; then
  behind=$(git rev-list --count HEAD..origin/main)
  if [ "$behind" -eq 0 ]; then echo "sync: up to date with origin/main."
  else echo "sync: $behind commit(s) behind origin/main; start from fresh origin/main or rebase before changing anything."; fi
else
  echo "sync: could not fetch origin; fetch before changing anything."
fi
[ $# -eq 0 ] || exec "$@"
