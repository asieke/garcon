---
name: connect-supabase
description: Set up or repair Garcon's cross-device sync through a Supabase project the user owns. Use when asked to sync Garcon across devices, connect Garcon to Supabase, see usage from all machines in one dashboard, enroll another machine, name a device, or when sync shows an error. Covers creating the project and schema with the Supabase CLI on the first machine and pasting the key on the rest.
---

# Connecting Garcon to Supabase

Sync is **off by default** and gated by one switch, "Enable Supabase sync", in Settings → Sync.
While it is off Garcon makes no network calls of its own. When it is on, this machine upserts
its usage rows into a `garcon_usage` table in the user's own Supabase project and pulls the
other machines' rows into a local cache, so every dashboard shows the union with a Device
filter. Recording never waits on sync. Read `README.md` "Sync across devices" first.

This is a guided walkthrough: explain each step, run what you can, have the user do the two
things that need their browser (the CLI login, and copying the key on a machine without the
CLI), and verify each result before moving on.

## 1. Preconditions

- Garcon is running: `curl -s http://127.0.0.1:4141/api/settings` returns JSON (otherwise the
  `install-garcon` skill).
- A Supabase account.
- First machine only: the Supabase CLI, `supabase --version` (2.x). If missing, install it the
  way this machine manages tools (mise, brew, npm); never system-wide without asking. `jq` and
  `curl` too.

## 2. Log in to the CLI (first machine)

`supabase login` opens the browser; the user completes it. Verify with `supabase orgs list`.
This token belongs to the CLI and is stored by it; it is separate from the project key Garcon
will store.

## 3. Name this machine

Ask the user what label they want to see for this machine in the Device filter and the
devices table, for example "work laptop" or "home desktop". Explain: the name is a label only.
A hidden device id in `~/.local/share/garcon/sync.json` identifies the machine, so renaming
later (Settings → Sync → Device name) is free, and each machine needs a distinct name to be
told apart. The default is the hostname.

## 4. Create the project and schema (first machine)

```sh
garcon connect-supabase --create-project --name "<label>"
```

Walk the user through what it does: picks the organisation (asks for `--org-id` if there is
more than one), creates a project named `garcon` on the free tier in `us-east-1` (or
`--region`), prints the generated database password once (Garcon never needs it; keep it in a
password manager), waits for the project to become healthy, applies
the schema embedded in the binary (`garcon connect-supabase --print-sql` prints it) through the Management API (no link step, no database
password), fetches the project's `sb_secret_` key with `supabase projects api-keys --reveal`
and pipes it straight into `PUT /api/settings` with the device name and the switch on. The
key is never printed and must never be pasted into the chat, the repo, `AGENTS.md` or a
commit; it lives only in owner-only `~/.config/garcon/config.json`.

If the user already has a project, add `--project-ref <ref>` and the creation step is
skipped (the ref is the 20-letter id in the project's URL).

Verify the schema afterwards:

```sh
supabase db query --project-ref <ref> "select count(*) from public.garcon_usage"
supabase db query --project-ref <ref> "select relrowsecurity from pg_class where relname = 'garcon_usage'"
```

Row level security must be `true` with **no policies**: only the secret key, which bypasses
RLS, may touch the table, and the publishable key then sees nothing. Never add policies.

## 5. Schema without the CLI

If the user prefers the dashboard: Supabase → SQL Editor → New query → paste
the schema embedded in the binary (`garcon connect-supabase --print-sql` prints it) (also shown with a Copy button in the documentation site, https://asieke.github.io/garcon/sync.html#sync-table) → Run →
"Success. No rows returned". Check Table Editor shows `garcon_usage` with RLS enabled. The
file is idempotent; rerun it after upgrading Garcon if Save reports a missing column.

## 6. Every other machine

Settings → Sync on that machine: device name (step 3, a different label), Project URL
`https://<ref>.supabase.co`, the **secret** key from Project Settings → API Keys (`sb_secret_…`;
a `sb_publishable_…` key is rejected because it gets the anon role and RLS would hide every
row; a legacy `service_role` JWT also works), switch **Enable Supabase sync** on, Save. Or,
with the CLI logged in there: `garcon connect-supabase --name "<label>" --project-ref <ref> --skip-schema`.

Headless, without a browser:

```sh
curl -s -X PUT -H 'Content-Type: application/json' http://127.0.0.1:4141/api/settings \
  -d '{"sync_enabled":true,"device_name":"<label>","url":"https://<ref>.supabase.co","key":"<sb_secret_…>"}'
```

Omit `key` to keep the stored one. Save probes the table with the given credentials and
answers 400 with the reason instead of storing a configuration that does not work.

## 7. Verify

- `curl -s http://127.0.0.1:4141/api/settings`: `status.pushed` climbs to this machine's row
  count and `pending` reaches 0; `last_push_error` is empty.
- One short call through the proxy (`claude -p hi` or the harness's one-shot mode), then the
  row appears in Supabase's Table Editor and, within a minute, in the other machine's Logs
  view. With two devices reporting, the Device filter appears in the filter bar and
  Sessions stay separate per machine.

## 8. Troubleshooting (Settings → Sync shows the last error)

- **Invalid JWT**: the key is publishable, or a legacy key was pasted with a typo. Use the
  `sb_secret_` key.
- **PGRST204 / could not find column**: schema older than Garcon. Rerun
  the schema embedded in the binary (`garcon connect-supabase --print-sql` prints it), then `alter table public.garcon_usage add column …` for
  anything the README lists that is still missing.
- **relation "garcon_usage" does not exist**: the SQL was not run, or ran in another project.
- **401**: the request did not look like it came from a server; only Garcon itself should
  use the key.
- **pending never drains**: read `last_push_error`; pushes retry with backoff up to five
  minutes and resume as soon as the cause is fixed. Turning the switch off and on wakes the
  loop immediately.
- **A machine appears twice**: `~/.local/share/garcon/sync.json` was deleted, so it got a new
  device id and re-pushed its history under it. Delete the old device's rows in SQL
  (`delete from public.garcon_usage where device_id = '<old id>'`) if it matters.
- **Rotate the key**: create a new secret key in Supabase, paste it in Settings → Sync,
  Save, then revoke the old one.

## 9. Turning it off

The switch off stops all network activity at once and keeps the settings. *Forget key* also
removes the key from the config. `~/.local/share/garcon/remote.jsonl` (the cache of other
machines' rows) can be deleted at any time; the next pull rebuilds it.
