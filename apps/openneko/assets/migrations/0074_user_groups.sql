-- OpenNeko groups. Administrators replaces app_user.role = 'admin'; Everyone
-- holds every active user implicitly and stores no membership rows.
--
-- app_user.role stays as a mirror of Administrators membership so code that
-- still reads or writes it, and an older image after a downgrade, see the
-- same answer. The triggers converge in one step because each write is
-- skipped when the other side already matches.

create table if not exists user_group (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  slug text not null check (slug ~ '^[a-z0-9][a-z0-9_-]{0,62}$'),
  name text not null check (length(trim(name)) > 0),
  description text,
  kind text not null default 'custom' check (kind in ('builtin', 'custom')),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (org_id, slug),
  unique (id, org_id)
);

create table if not exists user_group_membership (
  org_id text not null references organization(id) on delete cascade,
  group_id uuid not null,
  user_id text not null references app_user(id) on delete cascade,
  source text not null default 'local'
    check (source = 'local' or source ~ '^rule:[0-9a-f-]{36}$'),
  created_at timestamptz not null default now(),
  primary key (org_id, group_id, user_id, source),
  foreign key (group_id, org_id)
    references user_group(id, org_id) on delete cascade
);

create index if not exists user_group_membership_user_idx
  on user_group_membership (org_id, user_id, group_id);

create or replace function seed_builtin_user_groups(p_org_id text) returns void as $$
begin
  insert into user_group (org_id, slug, name, description, kind)
  values
    (p_org_id, 'administrators', 'Administrators',
     'Manage users, groups, plugins, packs and settings. Holds every item.', 'builtin'),
    (p_org_id, 'everyone', 'Everyone',
     'Every active user belongs to this group.', 'builtin')
  on conflict (org_id, slug) do nothing;
end;
$$ language plpgsql;

create or replace function organization_seed_user_groups() returns trigger as $$
begin
  perform seed_builtin_user_groups(NEW.id);
  return NEW;
end;
$$ language plpgsql;

drop trigger if exists organization_seed_user_groups_trigger on organization;
create trigger organization_seed_user_groups_trigger
  after insert on organization
  for each row execute function organization_seed_user_groups();

-- Old writers set app_user.role. Mirror it into Administrators membership.
-- A demotion removes every Administrators membership, whatever its source.
create or replace function app_user_role_to_administrators() returns trigger as $$
declare
  admins uuid;
begin
  if TG_OP = 'UPDATE' and NEW.role is not distinct from OLD.role then
    return NEW;
  end if;
  select id into admins from user_group
    where org_id = NEW.org_id and slug = 'administrators';
  if admins is null then
    return NEW;
  end if;
  if NEW.role = 'admin' then
    insert into user_group_membership (org_id, group_id, user_id, source)
    select NEW.org_id, admins, NEW.id, 'local'
    where not exists (
      select 1 from user_group_membership
      where org_id = NEW.org_id and group_id = admins and user_id = NEW.id)
    on conflict do nothing;
  elsif TG_OP = 'UPDATE' then
    delete from user_group_membership
      where org_id = NEW.org_id and group_id = admins and user_id = NEW.id;
  end if;
  return NEW;
end;
$$ language plpgsql;

drop trigger if exists app_user_role_to_administrators_trigger on app_user;
create trigger app_user_role_to_administrators_trigger
  after insert or update of role on app_user
  for each row execute function app_user_role_to_administrators();

-- New writers change membership. Mirror the result back into app_user.role.
create or replace function administrators_to_app_user_role() returns trigger as $$
declare
  row_data user_group_membership;
  is_admins boolean;
  holds boolean;
begin
  if TG_OP = 'DELETE' then
    row_data := OLD;
  else
    row_data := NEW;
  end if;
  select exists (
    select 1 from user_group
    where id = row_data.group_id and org_id = row_data.org_id and slug = 'administrators'
  ) into is_admins;
  if not is_admins then
    return null;
  end if;
  select exists (
    select 1 from user_group_membership
    where org_id = row_data.org_id and group_id = row_data.group_id and user_id = row_data.user_id
  ) into holds;
  update app_user
    set role = case when holds then 'admin' else 'member' end,
        updated_at = now()
    where id = row_data.user_id
      and org_id = row_data.org_id
      and role is distinct from (case when holds then 'admin' else 'member' end);
  return null;
end;
$$ language plpgsql;

drop trigger if exists administrators_to_app_user_role_trigger on user_group_membership;
create trigger administrators_to_app_user_role_trigger
  after insert or delete on user_group_membership
  for each row execute function administrators_to_app_user_role();

-- Upgrade: every existing org gets the built-in groups, and every admin,
-- including a solo installation's owner and disabled admins, keeps a local
-- Administrators membership.
select seed_builtin_user_groups(id) from organization;

insert into user_group_membership (org_id, group_id, user_id, source)
select u.org_id, g.id, u.id, 'local'
from app_user u
join user_group g on g.org_id = u.org_id and g.slug = 'administrators'
where u.role = 'admin'
on conflict do nothing;
