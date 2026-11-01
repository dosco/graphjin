-- Item grants. A group holds a specific item (item_id) or every current and
-- future item of a type ('*'). Administrators hold every item without rows.
-- Records apps keep their engine grants and are not stored here.

create table if not exists item_grant (
  org_id text not null references organization(id) on delete cascade,
  group_id uuid not null,
  item_type text not null check (item_type in (
    'skill', 'workflow', 'library_collection', 'library_concept', 'metric', 'dashboard',
    'watcher', 'team_memory', 'data_source', 'saved_query', 'api_operation', 'action',
    'integration', 'channel', 'pack')),
  item_id text not null check (length(item_id) between 1 and 500),
  created_by_user_id text references app_user(id) on delete set null,
  action_request_id text,
  created_at timestamptz not null default now(),
  primary key (org_id, group_id, item_type, item_id),
  foreign key (group_id, org_id) references user_group(id, org_id) on delete cascade
);

create index if not exists item_grant_item_idx on item_grant (org_id, item_type, item_id);

create table if not exists item_grant_revision (
  org_id text primary key references organization(id) on delete cascade,
  revision bigint not null default 0,
  updated_at timestamptz not null default now()
);

create table if not exists item_grant_audit (
  id bigint generated always as identity primary key,
  org_id text not null references organization(id) on delete cascade,
  actor_user_id text,
  action text not null check (action in ('grant', 'revoke')),
  group_id uuid not null,
  group_name text not null,
  item_type text not null,
  item_id text not null,
  action_request_id text,
  created_at timestamptz not null default now()
);

create index if not exists item_grant_audit_org_idx on item_grant_audit (org_id, created_at desc);

create or replace function bump_item_grant_revision() returns trigger as $$
declare
  target_org text;
begin
  if TG_OP = 'DELETE' then
    target_org := OLD.org_id;
  else
    target_org := NEW.org_id;
  end if;
  insert into item_grant_revision (org_id, revision, updated_at)
  select target_org, 1, now()
  where exists (select 1 from organization where id = target_org)
  on conflict (org_id) do update
    set revision = item_grant_revision.revision + 1, updated_at = now();
  return null;
end;
$$ language plpgsql;

drop trigger if exists item_grant_revision_trigger on item_grant;
create trigger item_grant_revision_trigger
  after insert or delete on item_grant
  for each row execute function bump_item_grant_revision();

drop trigger if exists user_group_membership_revision_trigger on user_group_membership;
create trigger user_group_membership_revision_trigger
  after insert or delete on user_group_membership
  for each row execute function bump_item_grant_revision();

-- Everyone holds every item type by default, on upgrade and for new
-- organizations, so no user loses access. Administrators narrow it later.
-- API operations are excluded: their allowed_roles already name who may call
-- them, and a default grant would widen exposed mutations.
create or replace function seed_everyone_item_grants(p_org_id text) returns void as $$
begin
  insert into item_grant (org_id, group_id, item_type, item_id)
  select g.org_id, g.id, t.item_type, '*'
  from user_group g
  cross join (values
    ('skill'), ('workflow'), ('library_collection'), ('library_concept'), ('metric'), ('dashboard'),
    ('watcher'), ('team_memory'), ('data_source'), ('saved_query'), ('action'),
    ('integration'), ('channel'), ('pack')) as t(item_type)
  where g.org_id = p_org_id and g.slug = 'everyone'
  on conflict do nothing;
end;
$$ language plpgsql;

create or replace function organization_seed_user_groups() returns trigger as $$
begin
  perform seed_builtin_user_groups(NEW.id);
  perform seed_everyone_item_grants(NEW.id);
  return NEW;
end;
$$ language plpgsql;

select seed_everyone_item_grants(id) from organization;
