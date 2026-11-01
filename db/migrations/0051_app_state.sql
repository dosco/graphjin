-- Metadata mirror for records-engine app lifecycle. The authoritative app
-- registry lives in records-db; this row lets navigation, orchestration, and
-- recovery checks remain available when that data plane is degraded.

create table if not exists app_state (
  org_id text not null references organization(id) on delete cascade,
  app_id text not null,
  status text not null default 'draft'
    check (status in ('draft', 'provisioning', 'importing', 'active', 'degraded', 'archived')),
  created_by text references app_user(id) on delete set null,
  created_at timestamptz not null default now(),
  config jsonb not null default '{}'::jsonb,
  primary key (org_id, app_id)
);

create index if not exists app_state_org_status_idx
  on app_state (org_id, status);
