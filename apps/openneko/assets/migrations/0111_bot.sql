-- =========================================================
-- bot: a person's own neko bot. Each bot has one running chat.
-- Idempotent: safe to re-run.
-- =========================================================

create table if not exists bot (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  owner_user_id text not null references app_user(id) on delete cascade,
  name text not null,
  coat text not null,
  pattern text not null check (pattern in ('solid', 'patch', 'tabby')),
  instructions text not null default '',
  is_default boolean not null default false,
  thread_id uuid references work_thread(id) on delete set null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists bot_owner_idx on bot (org_id, owner_user_id);

create unique index if not exists bot_one_default_idx
  on bot (org_id, owner_user_id) where is_default;

alter table bot add column if not exists instructions text not null default '';
