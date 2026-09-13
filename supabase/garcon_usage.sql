-- Garcon cross-device sync: one row per completion call, upserted by every device.
-- Idempotent: safe to run again after upgrading Garcon. Run it in the project's
-- SQL editor, or: supabase db query --project-ref <ref> -f supabase/garcon_usage.sql
create table if not exists public.garcon_usage (
  id            text primary key,           -- sha256 of device id + the row's fields
  device_id     text not null,              -- random id generated once per machine
  device        text not null,              -- the machine's display name
  time          bigint not null,            -- unix milliseconds
  harness       text not null,
  account       text not null,
  provider      text not null,
  model         text not null,
  status        integer not null,
  ms            bigint not null,
  queue_us      bigint,
  reused        boolean,
  connect_ms    bigint,
  dns_ms        bigint,
  tcp_ms        bigint,
  tls_ms        bigint,
  first_byte_ms bigint,
  input         bigint not null,            -- uncached input tokens
  cache_read    bigint not null,
  cache_write   bigint not null,
  output        bigint not null,
  synced_at     timestamptz not null default now()
);
create index if not exists garcon_usage_synced_at on public.garcon_usage (synced_at, id);
create index if not exists garcon_usage_device_time on public.garcon_usage (device_id, time);
-- No policies on purpose: only the project's secret key (which bypasses row level
-- security) can read or write this table. The publishable key sees nothing.
alter table public.garcon_usage enable row level security;
