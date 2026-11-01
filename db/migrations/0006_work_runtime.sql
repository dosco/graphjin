create table if not exists work_thread (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  title text not null default '',
  backend_state jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  last_message_at timestamptz not null default now()
);

create index if not exists work_thread_org_recent_idx
  on work_thread (org_id, last_message_at desc, created_at desc);

create table if not exists work_run (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  thread_id uuid not null references work_thread(id) on delete cascade,
  backend text not null,
  status text not null default 'running',
  error text,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  finished_at timestamptz
);

create index if not exists work_run_thread_created_idx
  on work_run (thread_id, created_at asc);

create table if not exists work_message (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  thread_id uuid not null references work_thread(id) on delete cascade,
  run_id uuid references work_run(id) on delete set null,
  role text not null,
  content text not null,
  created_at timestamptz not null default now()
);

create index if not exists work_message_thread_created_idx
  on work_message (thread_id, created_at asc);

create table if not exists work_run_event (
  id bigserial primary key,
  org_id text not null references organization(id) on delete cascade,
  thread_id uuid not null references work_thread(id) on delete cascade,
  run_id uuid not null references work_run(id) on delete cascade,
  seq integer not null,
  kind text not null,
  payload jsonb not null,
  created_at timestamptz not null default now()
);

create unique index if not exists work_run_event_run_seq_unique
  on work_run_event (run_id, seq);

create index if not exists work_run_event_thread_seq_idx
  on work_run_event (thread_id, seq asc);
