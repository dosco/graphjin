-- Per-group GraphJin table grants. The config generator writes each group's
-- rules as sources[].access.grants for the role og_<slug>. A rule applies
-- only while the group holds the data_source item for its source.

create table if not exists data_access_rule (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  group_id uuid not null,
  source text not null check (length(trim(source)) > 0),
  table_schema text not null default '',
  table_name text not null check (table_name ~ '^[A-Za-z_][A-Za-z0-9_$]*$'),
  columns text[] not null check (cardinality(columns) > 0),
  row_filter jsonb,
  created_by_user_id text references app_user(id) on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (org_id, group_id, source, table_schema, table_name),
  foreign key (group_id, org_id) references user_group(id, org_id) on delete cascade
);

create index if not exists data_access_rule_source_idx
  on data_access_rule (org_id, source);

-- Group grants change GraphJin's read policy to deny by default, so they stay
-- off until an administrator turns them on with a GraphJin release that
-- supports sources[].access.grants and union role mode.
create table if not exists data_access_settings (
  org_id text primary key references organization(id) on delete cascade,
  group_grants_enabled boolean not null default false,
  enabled_by_user_id text references app_user(id) on delete set null,
  updated_at timestamptz not null default now()
);

-- Read modes of database sources before group grants turned them to admin,
-- restored when group grants are turned off.
alter table data_access_settings add column if not exists previous_read_modes jsonb not null default '{}'::jsonb;
