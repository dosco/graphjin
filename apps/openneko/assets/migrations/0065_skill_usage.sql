-- Skill usage projection. Host-derived from tool_start events that load a
-- SKILL.md. One row per (run, skill). Raw traces stay on work_run_event.

create table if not exists skill_usage (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  run_id uuid not null references work_run(id) on delete cascade,
  skill_name text not null,
  content_hash text not null,
  origin text not null,
  pack_id text,
  pack_version text,
  config_commit_sha text,
  source text not null,
  first_event_id bigint not null,
  attempt integer not null default 1,
  created_at timestamptz not null default now(),
  constraint skill_usage_origin_check check (
    origin in ('builtin', 'custom', 'pack')
  ),
  constraint skill_usage_source_check check (
    source in ('hermes', 'read')
  )
);

create unique index if not exists skill_usage_run_skill_unique
  on skill_usage (run_id, skill_name);

create index if not exists skill_usage_org_skill_recent_idx
  on skill_usage (org_id, skill_name, created_at desc);
