-- IdP group rules put members of a provider group into an OpenNeko group.
-- Rule memberships carry source 'rule:<id>' and a sync recomputes them;
-- local memberships are never touched by a sync.

create table if not exists idp_group_rule (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  sso_group_id uuid not null,
  user_group_id uuid not null,
  created_by_user_id text references app_user(id) on delete set null,
  created_at timestamptz not null default now(),
  unique (org_id, sso_group_id, user_group_id),
  foreign key (sso_group_id, org_id) references sso_group(id, org_id) on delete cascade,
  foreign key (user_group_id, org_id) references user_group(id, org_id) on delete cascade
);

create index if not exists idp_group_rule_group_idx
  on idp_group_rule (org_id, user_group_id);

-- 'local' for users an administrator or the solo setup created; otherwise
-- the name of the plugin that supplied the user. A directory sync only
-- deactivates users that its own plugin supplied.
alter table app_user add column if not exists source text not null default 'local';

update app_user set source = 'sso' where sub is not null and source = 'local';

create table if not exists directory_sync_state (
  org_id text primary key references organization(id) on delete cascade,
  provider text,
  status text not null default 'never' check (status in ('never', 'running', 'ok', 'failed')),
  started_at timestamptz,
  finished_at timestamptz,
  last_error text,
  stats jsonb not null default '{}'::jsonb,
  updated_at timestamptz not null default now()
);
