-- Durable substrate samples for records-engine backpressure and the built-in
-- operations watcher. Current enforcement still samples the sidecar directly;
-- this mirror gives Briefing/doctor history and transition-aware alerts.

create table if not exists records_ops_health (
  org_id text not null references organization(id) on delete cascade,
  check_name text not null,
  level text not null
    check (level in ('ok', 'warning', 'critical', 'hard_stop', 'unavailable')),
  sample jsonb not null default '{}'::jsonb,
  sampled_at timestamptz not null,
  updated_at timestamptz not null default now(),
  primary key (org_id, check_name)
);

create index if not exists records_ops_health_org_level_idx
  on records_ops_health (org_id, level, updated_at desc);
