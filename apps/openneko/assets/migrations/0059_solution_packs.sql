-- First-class, organization-scoped solution-pack lifecycle and provenance.
-- Pack application is a durable saga because GraphJin files and metadata
-- Postgres cannot share one transaction.

create table if not exists pack_install (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  pack_id text not null,
  version text not null,
  status text not null default 'planning'
    check (status in ('planning', 'installing', 'installed', 'upgrading', 'failed', 'removing', 'removed')),
  manifest_hash text not null,
  config jsonb not null default '{}'::jsonb,
  source text not null default 'embedded'
    check (source in ('embedded')),
  installed_by_user_id text references app_user(id) on delete set null,
  operation_id uuid,
  last_error text,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  installed_at timestamptz,
  removed_at timestamptz
);

create unique index if not exists pack_install_org_pack_active_unique
  on pack_install (org_id, pack_id)
  where status <> 'removed';
create index if not exists pack_install_org_status_idx
  on pack_install (org_id, status, updated_at desc);

create table if not exists pack_artifact (
  id uuid primary key default gen_random_uuid(),
  pack_install_id uuid not null references pack_install(id) on delete cascade,
  org_id text not null references organization(id) on delete cascade,
  artifact_kind text not null
    check (artifact_kind in ('source', 'relationships', 'spec', 'saved_query', 'metric', 'workflow', 'watcher', 'action', 'policy', 'skill')),
  artifact_key text not null,
  target_ref text not null,
  desired_hash text not null,
  last_applied_hash text not null,
  ownership text not null default 'managed'
    check (ownership in ('managed', 'modified', 'detached', 'retired')),
  readiness text not null default 'not_applicable'
    check (readiness in ('ready', 'blocked', 'degraded', 'not_applicable')),
  readiness_reason text,
  previous_snapshot jsonb,
  metadata jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  detached_at timestamptz,
  unique (org_id, artifact_kind, target_ref),
  unique (pack_install_id, artifact_kind, artifact_key)
);

create index if not exists pack_artifact_install_idx
  on pack_artifact (pack_install_id, ownership, artifact_kind);
create index if not exists pack_artifact_org_readiness_idx
  on pack_artifact (org_id, readiness, updated_at desc);

create table if not exists pack_action_definition (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  kind text not null,
  description text not null default '',
  definition jsonb not null,
  definition_hash text not null,
  readiness text not null default 'blocked'
    check (readiness in ('ready', 'blocked', 'degraded')),
  readiness_reason text,
  enabled boolean not null default true,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (org_id, kind)
);

create index if not exists pack_action_definition_discovery_idx
  on pack_action_definition (org_id, readiness, enabled, updated_at desc);

create table if not exists pack_operation (
  id uuid primary key default gen_random_uuid(),
  pack_install_id uuid not null references pack_install(id) on delete cascade,
  org_id text not null references organization(id) on delete cascade,
  operation_type text not null
    check (operation_type in ('install', 'upgrade', 'configure', 'doctor', 'uninstall')),
  actor_user_id text references app_user(id) on delete set null,
  status text not null default 'planned'
    check (status in ('planned', 'running', 'compensating', 'succeeded', 'failed')),
  requested_version text,
  idempotency_key text not null,
  plan jsonb not null default '{}'::jsonb,
  plan_hash text not null,
  phase text not null default 'planned',
  failure_phase text,
  error text,
  compensation_status text
    check (compensation_status is null or compensation_status in ('not_required', 'pending', 'running', 'succeeded', 'failed')),
  started_at timestamptz,
  completed_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (org_id, idempotency_key)
);

create index if not exists pack_operation_install_recent_idx
  on pack_operation (pack_install_id, created_at desc);
create index if not exists pack_operation_org_status_idx
  on pack_operation (org_id, status, created_at desc);

alter table pack_install
  add constraint pack_install_operation_fk
  foreign key (operation_id) references pack_operation(id) on delete set null;

alter table watcher
  add column if not exists cooldown_seconds integer not null default 0,
  add column if not exists dedupe_key text,
  add column if not exists activation text,
  add column if not exists variables_json jsonb not null default '{}'::jsonb;
