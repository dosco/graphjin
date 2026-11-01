-- Where each plugin action's targets are in its payload. The worker writes
-- these rows from the installed plugin manifests; policy checks in web and
-- worker read them so rules match the real targets, not the agent's claim.
create table if not exists action_target_spec (
  org_id text not null references organization(id) on delete cascade,
  kind text not null,
  plugin_name text not null,
  spec jsonb not null,
  updated_at timestamptz not null default now(),
  primary key (org_id, kind)
);
