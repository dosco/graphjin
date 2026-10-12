-- =========================================================
-- A routine saved from a bot chat belongs to the chat's user and
-- runs with their access. Give existing ownerless bot routines
-- that owner, unless the user already has a routine with that name.
-- =========================================================

update workflow_definition w
set owner_user_id = t.created_by_user_id,
    updated_at = now()
from bot b
join work_thread t on t.id = b.thread_id
where b.thread_id = w.created_by_thread_id
  and b.org_id = w.org_id
  and w.owner_user_id = ''
  and t.created_by_user_id is not null
  and t.created_by_user_id <> ''
  and not exists (
    select 1 from workflow_definition other
    where other.org_id = w.org_id
      and other.owner_user_id = t.created_by_user_id
      and other.name = w.name
  );
