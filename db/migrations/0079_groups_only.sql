-- Groups become the only record of who administers an install and who
-- approves an action. Migrations 0074 and 0078 kept app_user.role and
-- action_policy.approver_role as mirrors; this release drops both, with the
-- triggers that maintained them. Administrators membership answers both
-- questions from here on, whichever sign-in plugin an install runs.

-- Last backfill before the columns go: an administrator by the old column,
-- including a solo owner and a disabled account, keeps a local membership.
insert into user_group_membership (org_id, group_id, user_id, source)
select u.org_id, g.id, u.id, 'local'
from app_user u
join user_group g on g.org_id = u.org_id and g.slug = 'administrators'
where u.role = 'admin'
on conflict do nothing;

-- Every policy that approved by role now names the group.
update action_policy p
set approver_group_id = g.id
from user_group g
where g.org_id = p.org_id and g.slug = 'administrators'
  and p.approver_group_id is null and p.approver_role = 'admin';

drop trigger if exists app_user_role_to_administrators_trigger on app_user;
drop trigger if exists administrators_to_app_user_role_trigger on user_group_membership;
drop trigger if exists action_policy_default_approver_group_trigger on action_policy;
drop function if exists app_user_role_to_administrators();
drop function if exists administrators_to_app_user_role();
drop function if exists action_policy_default_approver_group();

alter table app_user drop column if exists role;
alter table action_policy drop column if exists approver_role;

-- Superseded by idp_group_rule: 0078 converted its admin rows into rules.
drop table if exists sso_group_mapping;
