#!/usr/bin/env python3
"""Check the public repository's local-context exclusions without reading contents."""
import subprocess
import sys


def main():
    root = subprocess.check_output(["git", "rev-parse", "--show-toplevel"]).decode().strip()
    paths = subprocess.check_output(["git", "-C", root, "ls-files", "-z"]).split(b"\0")
    private_paths = [p for p in paths if p and (
        p.lower() in (b"agents.md", b".local") or p.lower().startswith(b".local/")
    )]
    if private_paths:
        # Do not echo private filenames or contents into public CI logs.
        print("Local context is tracked. Remove it from the index while preserving local files.", file=sys.stderr)
        return 1
    for path in (".local/", ".local/project.json", "AGENTS.md", "agents.md"):
        result = subprocess.run(["git", "-C", root, "check-ignore", "--no-index", "--quiet", "--", path])
        if result.returncode != 0:
            print("Required local-context exclusions are missing or Git could not verify them.", file=sys.stderr)
            return 1
    print("Repository boundaries verified: local context is ignored and untracked.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
