-- =========================================================
-- When the user last saw a bot's chat. A newer message marks
-- the bot in the chat list until the user opens the chat.
-- =========================================================

alter table bot add column if not exists seen_at timestamptz not null default now();
