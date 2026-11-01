-- Channel isolation: record each work thread's origin channel so the web Ask
-- UI lists only its own ("web") threads and other channels (Telegram, …) stay
-- separate. See docs/PER_CHANNEL_RENDERING.md.

ALTER TABLE work_thread
  ADD COLUMN channel text NOT NULL DEFAULT 'web';

-- Backfill the only non-web origin so far: Telegram inbound threads, which
-- startChatRun titles "Telegram <ref>" (or bare "Telegram").
UPDATE work_thread
  SET channel = 'telegram'
  WHERE title = 'Telegram' OR title LIKE 'Telegram %';

CREATE INDEX IF NOT EXISTS work_thread_org_channel_recent_idx
  ON work_thread (org_id, channel, last_message_at DESC);
