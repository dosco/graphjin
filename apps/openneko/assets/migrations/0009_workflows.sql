-- Operating loop foundation: workflow definitions, runs, and outputs.
-- workflow_run wraps work_run; workflow_output is the product signal layer.
-- subscription, observation, and action_* tables land in follow-up migrations.

create table if not exists workflow_definition (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  name text not null,
  description text not null default '',
  enabled boolean not null default true,
  status text not null default 'active',
  goal text not null default '',
  system_prompt_overlay text not null default '',
  steps jsonb not null default '[]'::jsonb,
  cron text,
  cron_timezone text not null default 'UTC',
  cron_enabled boolean not null default true,
  output_contract jsonb,
  created_by_thread_id uuid references work_thread(id) on delete set null,
  created_by_run_id uuid references work_run(id) on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists workflow_definition_org_idx
  on workflow_definition (org_id, enabled, updated_at desc);

create unique index if not exists workflow_definition_org_name_unique
  on workflow_definition (org_id, name);

create index if not exists workflow_definition_cron_active_idx
  on workflow_definition (org_id, cron_enabled)
  where cron is not null and enabled = true;


create table if not exists workflow_run (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  workflow_id uuid not null references workflow_definition(id) on delete cascade,
  thread_id uuid not null references work_thread(id) on delete cascade,
  work_run_id uuid not null references work_run(id) on delete cascade,
  trigger_kind text not null,
  trigger_payload jsonb not null default '{}'::jsonb,
  triggered_by_subscription_id uuid,
  triggered_by_output_id uuid,
  triggered_by_observation_id uuid,
  chain_depth integer not null default 0,
  status text not null default 'queued',
  started_at timestamptz,
  finished_at timestamptz,
  summary text,
  error text,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create unique index if not exists workflow_run_work_run_unique
  on workflow_run (work_run_id);

create index if not exists workflow_run_workflow_created_idx
  on workflow_run (workflow_id, created_at desc);

create index if not exists workflow_run_org_status_idx
  on workflow_run (org_id, status, created_at desc);


create table if not exists workflow_output (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  workflow_run_id uuid not null references workflow_run(id) on delete cascade,
  work_run_id uuid not null references work_run(id) on delete cascade,
  kind text not null,
  title text not null default '',
  body text not null default '',
  payload jsonb not null default '{}'::jsonb,
  artifact_path text,
  scope text,
  topic text,
  mood text,
  time_window_start timestamptz,
  time_window_end timestamptz,
  freshness_ttl_seconds integer,
  created_at timestamptz not null default now()
);

create index if not exists workflow_output_run_created_idx
  on workflow_output (workflow_run_id, created_at desc);

create index if not exists workflow_output_org_scope_idx
  on workflow_output (org_id, scope, created_at desc)
  where scope is not null;

create index if not exists workflow_output_org_mood_idx
  on workflow_output (org_id, mood, created_at desc)
  where mood is not null;
