-- Operating loop, subscription + observation tier.
-- subscription      — a workflow declares which signals it observes
-- observation       — consumer-written lineage row (workflow B "noticed"
--                     output X). Multiple observation rows per output
--                     possible — one per consumer that acted on it.
-- workflow_output_source_observation — many-to-many lineage: which
--                     observations did the producing run of this output
--                     itself consume?

create table if not exists subscription (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  workflow_id uuid not null references workflow_definition(id) on delete cascade,
  source_kind text not null,
  filter jsonb not null default '{}'::jsonb,
  enabled boolean not null default true,
  debounce_ms integer not null default 0,
  max_concurrent_runs integer not null default 5,
  max_chain_depth_override integer,
  idempotency_key_template text,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists subscription_workflow_idx
  on subscription (workflow_id);

create index if not exists subscription_org_enabled_idx
  on subscription (org_id, enabled)
  where enabled = true;

create index if not exists subscription_source_kind_idx
  on subscription (source_kind)
  where enabled = true;


create table if not exists observation (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  source_output_id uuid references workflow_output(id) on delete set null,
  consumer_kind text not null,
  consumer_workflow_id uuid references workflow_definition(id) on delete set null,
  consumer_run_id uuid references workflow_run(id) on delete set null,
  consumer_user_id text,
  subscription_id uuid references subscription(id) on delete set null,
  title text,
  body text,
  mood text,
  status text not null default 'active',
  first_seen_at timestamptz not null default now(),
  last_seen_at timestamptz not null default now(),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists observation_source_output_idx
  on observation (source_output_id)
  where source_output_id is not null;

create index if not exists observation_consumer_workflow_idx
  on observation (consumer_workflow_id, created_at desc)
  where consumer_workflow_id is not null;

create index if not exists observation_subscription_idx
  on observation (subscription_id, created_at desc)
  where subscription_id is not null;

create index if not exists observation_org_status_idx
  on observation (org_id, status, created_at desc);


create table if not exists workflow_output_source_observation (
  workflow_output_id uuid not null references workflow_output(id) on delete cascade,
  observation_id uuid not null references observation(id) on delete cascade,
  created_at timestamptz not null default now(),
  primary key (workflow_output_id, observation_id)
);

create index if not exists workflow_output_source_obs_obs_idx
  on workflow_output_source_observation (observation_id);
