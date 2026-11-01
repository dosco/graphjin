-- Operating loop, action tier. Workflows decide; actions mutate.
-- action_policy   - rules that gate state-changing operations
-- action_request  - proposed state change (internal or external)
-- action_execution - append-only log of executor attempts

create table if not exists action_policy (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  name text not null,
  description text not null default '',
  applies_to_kinds text[] not null default '{}',
  applies_to_scopes text[] not null default '{}',
  mode text not null,
  risk_threshold_auto_approve text,
  allowed_targets jsonb,
  denied_targets jsonb,
  limits jsonb not null default '{}'::jsonb,
  approver_role text,
  priority integer not null default 100,
  enabled boolean not null default true,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists action_policy_org_enabled_priority_idx
  on action_policy (org_id, enabled, priority);

create table if not exists action_request (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  workflow_run_id uuid references workflow_run(id) on delete cascade,
  triggered_by_observation_id uuid references observation(id) on delete set null,
  policy_id uuid references action_policy(id) on delete set null,
  scope text not null,
  kind text not null,
  target text,
  payload jsonb not null default '{}'::jsonb,
  risk_level text,
  status text not null default 'pending_approval',
  summary text,
  requested_by_run_id uuid references workflow_run(id) on delete set null,
  approved_by_user_id text references app_user(id) on delete set null,
  approved_at timestamptz,
  rejection_reason text,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists action_request_org_status_idx
  on action_request (org_id, status, created_at desc);

create index if not exists action_request_workflow_run_idx
  on action_request (workflow_run_id, created_at desc);

create index if not exists action_request_pending_idx
  on action_request (org_id, created_at desc);

create table if not exists action_execution (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  action_request_id uuid not null references action_request(id) on delete cascade,
  executor text not null,
  command_or_operation text,
  payload jsonb,
  result jsonb,
  external_ref text,
  status text not null default 'pending',
  error text,
  started_at timestamptz,
  finished_at timestamptz,
  created_at timestamptz not null default now()
);

create index if not exists action_execution_request_idx
  on action_execution (action_request_id, created_at desc);

create index if not exists action_execution_org_status_idx
  on action_execution (org_id, status, created_at desc);
