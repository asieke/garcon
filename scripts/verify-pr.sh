#!/usr/bin/env bash
# Test a pull request on this machine and report the result as the "check" status merging requires:
#   scripts/verify-pr.sh <number>
# Checks the PR out into a temporary worktree, runs `scripts/check.sh --report` there, and removes it.
# Run it where the project can really be tested (for example, the Mac with Xcode, or a local app's host).
# Identical in every project: copy it from asieke/workspace templates/common/scripts/verify-pr.sh.
set -euo pipefail
n="${1:?usage: scripts/verify-pr.sh <pr-number>}"
cd "$(dirname "$0")/.."
git fetch --quiet origin main
git fetch --quiet origin "pull/$n/head"
sha=$(git rev-parse FETCH_HEAD)
dir="$(mktemp -d)/pr-$n"
git worktree add --quiet --detach "$dir" "$sha"
trap 'git worktree remove --force "$dir"' EXIT
echo "verify-pr: testing PR #$n at ${sha:0:7} in $dir"
(cd "$dir" && scripts/check.sh --report)
