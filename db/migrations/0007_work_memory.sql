create table if not exists work_memory (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  kind text not null,
  scope text not null,
  scope_id text,
  text text not null,
  pinned boolean not null default false,
  confidence real not null default 0.8,
  metadata jsonb not null default '{}'::jsonb,
  source_run_id uuid references work_run(id) on delete set null,
  source_thread_id uuid references work_thread(id) on delete set null,
  use_count integer not null default 0,
  last_used_at timestamptz,
  archived_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists work_memory_org_active_idx
  on work_memory (org_id, archived_at, updated_at desc);

create index if not exists work_memory_org_scope_idx
  on work_memory (org_id, scope, scope_id, archived_at);

create index if not exists work_memory_org_pinned_idx
  on work_memory (org_id, pinned, archived_at, updated_at desc);

create table if not exists work_memory_event (
  id bigserial primary key,
  org_id text not null references organization(id) on delete cascade,
  memory_id uuid references work_memory(id) on delete set null,
  run_id uuid references work_run(id) on delete set null,
  thread_id uuid references work_thread(id) on delete set null,
  action text not null,
  payload jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now()
);

create index if not exists work_memory_event_memory_idx
  on work_memory_event (memory_id, id desc);

create index if not exists work_memory_event_org_recent_idx
  on work_memory_event (org_id, id desc);

create table if not exists work_pending_memory (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  thread_id uuid references work_thread(id) on delete cascade,
  run_id uuid references work_run(id) on delete set null,
  status text not null default 'proposed',
  draft_text text not null,
  draft_kind text not null,
  draft_scope text not null,
  draft_scope_id text,
  confidence real not null,
  reasoning text,
  conflict jsonb,
  decision_text text,
  decided_at timestamptz,
  memory_id uuid references work_memory(id) on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists work_pending_memory_thread_status_idx
  on work_pending_memory (org_id, thread_id, status, created_at desc);

create index if not exists work_pending_memory_run_status_idx
  on work_pending_memory (org_id, run_id, status, created_at desc);

create index if not exists work_pending_memory_org_status_idx
  on work_pending_memory (org_id, status, created_at desc);
