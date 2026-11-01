-- Skill learner state: org/skill flags default off, durable cursor, audit log.

create table if not exists skill_learn_org (
  org_id text primary key references organization(id) on delete cascade,
  enabled boolean not null default false,
  updated_at timestamptz not null default now()
);

create table if not exists skill_learn_state (
  org_id text not null references organization(id) on delete cascade,
  skill_name text not null,
  enabled boolean not null default false,
  high_water_event_id bigint not null default 0,
  pending_settled_count integer not null default 0,
  next_due_at timestamptz,
  current_base_hash text,
  current_learned_hash text,
  lease_owner text,
  lease_until timestamptz,
  updated_at timestamptz not null default now(),
  primary key (org_id, skill_name)
);

create table if not exists skill_learn_event (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  skill_name text not null,
  base_hash text,
  content_hash text,
  run_ids jsonb not null default '[]'::jsonb,
  lesson text,
  rationale text,
  diff text,
  model_trace jsonb not null default '{}'::jsonb,
  decision text not null,
  reason text not null default '',
  created_at timestamptz not null default now(),
  constraint skill_learn_event_decision_check check (
    decision in ('applied', 'skipped', 'stale', 'rejected')
  )
);

create index if not exists skill_learn_event_org_skill_recent_idx
  on skill_learn_event (org_id, skill_name, created_at desc);
