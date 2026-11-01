-- Watchers run as GraphJin watches. The worker keeps each watcher's GraphJin
-- watch in step with its definition and evaluates the condition when the
-- watch's webhook arrives.
ALTER TABLE watcher ADD COLUMN IF NOT EXISTS graphjin_watch_id text;
ALTER TABLE watcher ADD COLUMN IF NOT EXISTS graphjin_endpoint text;
ALTER TABLE watcher ADD COLUMN IF NOT EXISTS graphjin_watch_hash text;
CREATE UNIQUE INDEX IF NOT EXISTS watcher_graphjin_watch_unique
  ON watcher (graphjin_endpoint, graphjin_watch_id)
  WHERE graphjin_watch_id IS NOT NULL;
