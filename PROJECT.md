---
name: Garcon
type: local
status: active
visibility: public
owner: asieke
merge: agent
deploy: local:macbook:~/apps/garcon
refresh: none
sources: []
standard: 2026-10-14
---

# Garcon

## Purpose
A local proxy and dashboard that connects coding tools (Codex, Claude Code, Pi and other compatible clients) to their providers, assigns conversations to the accounts you choose, and shows requests, usage, limits and errors in one place.

## Audience
People running several coding-agent subscriptions on one machine who need to know which account is doing the work and how much allowance is left.

## Goals and success
- Every Codex conversation lands on a healthy account with quota and stays there; a failed request is never replayed on another account.
- Requests, sessions, usage and limits are visible at http://127.0.0.1:4141 without storing prompts, responses or OAuth tokens.
- Every merge to `main` publishes a working `ai-garcon` release to npm, and `garcon update` or `scripts/deploy.sh` brings an install up to date.

## Context
- Constraints: runs only on the local machine (loopback by default, no app login); data stays in `~/.local/share/garcon`; the repository is public, so credentials, host paths and private notes stay out of it.
- Distribution: npm package `ai-garcon`, built and published by `.github/workflows/release.yml` on every merge to `main`.
- Public site: https://asieke.github.io/garcon/, served by GitHub Pages straight from `docs/` on `main` (no workflow file; GitHub's built-in `pages build and deployment` runs on every push). Unlike `asieke/workspace`, which moved its guide to a GitHub Wiki, Garcon keeps this public page; its wiki is unused.
- Supporting documents: [README.md](README.md), [CONTRIBUTING.md](CONTRIBUTING.md), [docs/](docs/), [product acceptance and verification](docs/acceptance-checks.md).
- Open decisions: see [Issues](https://github.com/asieke/garcon/issues).
