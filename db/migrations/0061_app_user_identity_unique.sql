-- The concurrent first-sign-in race could insert two app_user rows for one
-- identity (same sub, or same email), after which every later sign-in threw
-- "already bound to a different SSO subject" until an operator did manual
-- SQL. Repair existing duplicates, then add the unique indexes that make
-- the race lose loudly instead of persisting.
--
-- Duplicate repair keeps the OLDEST row (the one later sign-ins would have
-- matched first) and neutralizes newer ones in place rather than deleting
-- them: rows may be referenced by runs/personas, so they are detached from
-- the identity (sub cleared, email made unique) and disabled.

with ranked_sub as (
  select id,
         row_number() over (
           partition by org_id, sub
           order by created_at asc, id asc
         ) as rn
  from app_user
  where sub is not null
)
update app_user u
set sub = null,
    disabled_at = coalesce(u.disabled_at, now()),
    updated_at = now()
from ranked_sub r
where u.id = r.id and r.rn > 1;

with ranked_email as (
  select id,
         row_number() over (
           partition by org_id, lower(email)
           order by created_at asc, id asc
         ) as rn
  from app_user
)
update app_user u
set email = u.email || '+duplicate-' || u.id,
    disabled_at = coalesce(u.disabled_at, now()),
    updated_at = now()
from ranked_email r
where u.id = r.id and r.rn > 1;

create unique index if not exists app_user_org_sub_unique
  on app_user (org_id, sub)
  where sub is not null;

create unique index if not exists app_user_org_email_unique
  on app_user (org_id, lower(email));
