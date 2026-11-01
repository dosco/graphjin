-- Action approval follows a group. approver_role stays for older images and
-- pack files: 'admin' maps to Administrators, and a policy without a group
-- keeps its role rule.

alter table action_policy
  add column if not exists approver_group_id uuid references user_group(id) on delete set null;

create or replace function action_policy_default_approver_group() returns trigger as $$
begin
  if NEW.approver_group_id is null and NEW.approver_role = 'admin' then
    select id into NEW.approver_group_id from user_group
      where org_id = NEW.org_id and slug = 'administrators';
  end if;
  return NEW;
end;
$$ language plpgsql;

drop trigger if exists action_policy_default_approver_group_trigger on action_policy;
create trigger action_policy_default_approver_group_trigger
  before insert or update of approver_role on action_policy
  for each row execute function action_policy_default_approver_group();

update action_policy p
set approver_group_id = g.id
from user_group g
where p.approver_group_id is null and p.approver_role = 'admin'
  and g.org_id = p.org_id and g.slug = 'administrators';

-- sso_group_mapping had no writer, but a hand-inserted admin mapping becomes
-- an IdP rule to Administrators. The table stays for older images.
insert into idp_group_rule (org_id, sso_group_id, user_group_id)
select distinct m.org_id, s.id, g.id
from sso_group_mapping m
join sso_group s on s.org_id = m.org_id and s.provider = m.provider and s.external_id = m.group_external_id
join user_group g on g.org_id = m.org_id and g.slug = 'administrators'
where m.role = 'admin'
on conflict (org_id, sso_group_id, user_group_id) do nothing;
