-- =========================================================
-- Mark bots that 0113 named after their chat title. The worker
-- gives each one a short name once, unless the user renames it first.
-- =========================================================

alter table bot add column if not exists name_pending boolean not null default false;

update bot b
set name_pending = true
from work_thread t
where t.id = b.thread_id
  and not b.is_default
  and b.created_at = t.created_at
  and b.name = case
    when char_length(btrim(t.title)) > 40 then btrim(left(btrim(t.title), 39)) || '…'
    else coalesce(nullif(btrim(t.title), ''), 'Neko')
  end;

create index if not exists bot_name_pending_idx on bot (id) where name_pending;
