# Standard gate, sourced first by every scripts/check.sh:  . "$(dirname "$0")/gate.sh" "$@"
#   scripts/check.sh            hygiene, secret scan, then the project's own checks
#   scripts/check.sh --report   same, then posts the result as the "check" commit status that branch
#                               protection requires before merging (needs gh, a clean tree, and HEAD
#                               pushed to GitHub)
# Identical in every project: copy it from asieke/workspace templates/common/scripts/gate.sh; do not edit.
# check.sh must not set its own EXIT trap: --report uses it to post the result.
cd "$(dirname "${BASH_SOURCE[0]}")/.."
gate_report=; [ "${1:-}" != --report ] || gate_report=1

if [ -n "$gate_report" ]; then
  [ -z "$(git status --porcelain)" ] || { echo "check: --report needs a clean tree" >&2; exit 1; }
  gate_sha=$(git rev-parse HEAD)
  gate_repo=$(gh repo view --json nameWithOwner --jq .nameWithOwner) || { echo "check: --report needs gh signed in to GitHub" >&2; exit 1; }
  gh api "repos/$gate_repo/commits/$gate_sha" --silent 2>/dev/null || { echo "check: --report needs ${gate_sha:0:7} pushed to GitHub first" >&2; exit 1; }
  gate_post() {
    if gh api -X POST "repos/$gate_repo/statuses/$gate_sha" -f state="$1" -f context=check -f description="$2" --silent; then
      echo "check: reported $1 for ${gate_sha:0:7}"
    else
      echo "check: could not report $1 for ${gate_sha:0:7}" >&2
    fi
  }
  trap 'rc=$?; if [ $rc -eq 0 ]; then gate_post success "scripts/check.sh passed on $(hostname -s)"; else gate_post failure "scripts/check.sh failed on $(hostname -s)"; fi; exit $rc' EXIT
fi

if git grep -nE '^(<{7}|={7}|>{7})( |$)' -- . ':(exclude)*scripts/gate.sh'; then
  echo "merge conflict markers found" >&2; exit 1
fi
if git grep -nE 'BEGIN [A-Z0-9 ]*PRIVATE KEY' -- . ':(exclude)*scripts/gate.sh'; then
  echo "private key found" >&2; exit 1
fi
while IFS= read -r f; do
  [ -f "$f" ] && [ "$(wc -c <"$f")" -gt 25600000 ] && { echo "file over 25 MB: $f" >&2; exit 1; }
done < <(git ls-files)

# Secret scan of every commit not yet on origin/main. Put false positives in .gitleaksignore.
gate_gitleaks() {
  command -v gitleaks && return
  local ver=8.21.2 os sum dir
  case "$(uname -s)-$(uname -m)" in
    Linux-x86_64)              os=linux_x64    sum=5bc41815076e6ed6ef8fbecc9d9b75bcae31f39029ceb55da08086315316e3ba;;
    Linux-aarch64|Linux-arm64) os=linux_arm64  sum=654c935542c89f565aabe7bf7c6c500830f116c114f0aeb509d2460c1ac2e6da;;
    Darwin-arm64)              os=darwin_arm64 sum=cad3de5dc9a4d5447d967a70a4d49499c557f04db028274cc324f9ff983f6502;;
    Darwin-x86_64)             os=darwin_x64   sum=5b42c6e4b1fd693eaeb2b5b7faa5f17a1434299d4deb2de63d4b2efd7c753128;;
    *) return 1;;
  esac
  dir="${XDG_CACHE_HOME:-$HOME/.cache}/gitleaks-$ver"
  if [ ! -x "$dir/gitleaks" ]; then
    mkdir -p "$dir" || return 1
    curl -sSfL -o "$dir/gitleaks.tar.gz" "https://github.com/gitleaks/gitleaks/releases/download/v$ver/gitleaks_${ver}_$os.tar.gz" || return 1
    [ "$( (sha256sum "$dir/gitleaks.tar.gz" 2>/dev/null || shasum -a 256 "$dir/gitleaks.tar.gz") | cut -d' ' -f1)" = "$sum" ] \
      || { rm -f "$dir/gitleaks.tar.gz"; echo "check: gitleaks download failed its checksum" >&2; return 1; }
    tar -xzf "$dir/gitleaks.tar.gz" -C "$dir" gitleaks && rm -f "$dir/gitleaks.tar.gz" || return 1
  fi
  echo "$dir/gitleaks"
}
gate_range=HEAD; git rev-parse --verify -q origin/main >/dev/null && gate_range=origin/main..HEAD
if gate_gl=$(gate_gitleaks); then
  "$gate_gl" git --redact --no-banner --log-level=warn --log-opts="$gate_range" . \
    || { echo "check: gitleaks found secrets in $gate_range (false positives go in .gitleaksignore)" >&2; exit 1; }
elif [ -n "$gate_report" ]; then
  echo "check: could not get gitleaks, and --report requires the secret scan" >&2; exit 1
else
  echo "check: WARNING: could not get gitleaks; secret scan skipped (--report will require it)" >&2
fi
