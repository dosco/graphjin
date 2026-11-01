-- Reckon-compatible webhooks. Each row maps a Reckon workflow id and token to
-- an OpenNeko workflow; each run keeps the Reckon-facing ULID and idempotency
-- key beside the OpenNeko workflow run that executes it.

create table if not exists compat_webhook (
  reckon_workflow_id text primary key,
  org_id text not null references organization(id) on delete cascade,
  workflow_id uuid not null references workflow_definition(id) on delete cascade,
  token_sha256 text not null,
  enabled boolean not null default true,
  params text[],
  batch_chunk_size integer not null default 1000
    check (batch_chunk_size between 1 and 1000),
  created_by_user_id text references app_user(id) on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists compat_webhook_org_idx on compat_webhook (org_id);

create table if not exists compat_webhook_run (
  id text primary key,
  org_id text not null references organization(id) on delete cascade,
  reckon_workflow_id text not null references compat_webhook(reckon_workflow_id) on delete cascade,
  workflow_run_id uuid not null references workflow_run(id) on delete cascade,
  work_run_id uuid not null references work_run(id) on delete cascade,
  execution_mode text not null check (execution_mode in ('single', 'batch')),
  idempotency_key text not null,
  request_fingerprint text not null,
  created_at timestamptz not null default now()
);

create unique index if not exists compat_webhook_run_idempotency_unique
  on compat_webhook_run (reckon_workflow_id, idempotency_key);
create unique index if not exists compat_webhook_run_workflow_run_unique
  on compat_webhook_run (workflow_run_id);
create index if not exists compat_webhook_run_active_idx
  on compat_webhook_run (reckon_workflow_id, created_at desc);
