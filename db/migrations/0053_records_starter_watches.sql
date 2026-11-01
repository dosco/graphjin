-- Coordination state between optional records starter workflows and
-- GraphJin's native durable gj_watch/watch-event store.

create table if not exists records_watch_binding (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  app_id text not null,
  watch_key text not null
    check (watch_key in (
      'opportunities_without_activity',
      'unlinked_or_departed_owners',
      'deals_closing_this_month'
    )),
  workflow_id uuid not null references workflow_definition(id) on delete cascade,
  graphjin_watch_id text not null,
  definition_hash text not null,
  enabled boolean not null default true,
  status text not null default 'active'
    check (status in ('active', 'paused', 'error')),
  last_evaluated_at timestamptz,
  last_error text,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (org_id, app_id, watch_key),
  unique (graphjin_watch_id)
);

create index if not exists records_watch_binding_due_idx
  on records_watch_binding (org_id, enabled, status, last_evaluated_at);

create table if not exists records_watch_event_receipt (
  event_id text primary key,
  binding_id uuid not null references records_watch_binding(id) on delete cascade,
  payload_hash text not null,
  received_at timestamptz not null default now(),
  queued_at timestamptz
);

create index if not exists records_watch_event_receipt_binding_idx
  on records_watch_event_receipt (binding_id, received_at desc);
