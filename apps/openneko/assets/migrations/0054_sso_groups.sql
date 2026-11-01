-- Immutable IdP group identities and current memberships. Display names are
-- mutable metadata. The provider/tenant/external tuple is the IdP identity;
-- entitlements reference its stable local UUID so external IDs cannot collide.

create table if not exists sso_group (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  provider text not null,
  tenant_id text not null,
  external_id text not null,
  display_name text,
  active boolean not null default true,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (org_id, provider, tenant_id, external_id),
  unique (id, org_id)
);

create table if not exists sso_group_membership (
  org_id text not null references organization(id) on delete cascade,
  group_id uuid not null,
  user_id text not null references app_user(id) on delete cascade,
  synced_at timestamptz not null default now(),
  primary key (org_id, group_id, user_id),
  foreign key (group_id, org_id)
    references sso_group(id, org_id) on delete cascade
);

create index if not exists sso_group_membership_user_idx
  on sso_group_membership (org_id, user_id, group_id);

create table if not exists sso_group_sync_audit (
  id bigint generated always as identity primary key,
  org_id text not null references organization(id) on delete cascade,
  user_id text not null references app_user(id) on delete cascade,
  provider text not null,
  tenant_id text not null,
  external_group_ids text[] not null default '{}',
  synced_at timestamptz not null default now()
);

create index if not exists sso_group_sync_audit_user_idx
  on sso_group_sync_audit (org_id, user_id, synced_at desc);
