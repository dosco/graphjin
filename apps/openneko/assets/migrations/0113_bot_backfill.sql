-- =========================================================
-- Give each existing web chat thread its own bot, so chats from
-- before bots appear in the chat list with a random neko.
-- Idempotent: threads that already have a bot are skipped.
-- =========================================================

insert into bot (org_id, owner_user_id, name, coat, pattern, thread_id, created_at, updated_at)
select
  t.org_id,
  t.created_by_user_id,
  case
    when char_length(btrim(t.title)) > 40 then btrim(left(btrim(t.title), 39)) || '…'
    else coalesce(nullif(btrim(t.title), ''), 'Neko')
  end,
  (array['snow', 'ginger', 'smoke', 'ink', 'cream', 'cocoa', 'mint', 'lilac', 'sky', 'rose', 'lemon', 'slate'])[1 + floor(random() * 12)::int],
  (array['solid', 'patch', 'tabby'])[1 + floor(random() * 3)::int],
  t.id,
  t.created_at,
  now()
from work_thread t
join app_user u on u.id = t.created_by_user_id
where t.channel = 'web'
  and not exists (select 1 from bot b where b.thread_id = t.id)
  and not exists (select 1 from workflow_run w where w.thread_id = t.id);
