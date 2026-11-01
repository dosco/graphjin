-- Reliable inbound: persisted poll cursor + idempotency ledger. Together they
-- make channel inbound exactly-once across worker restarts and provider retries.

-- channel_poll_cursor: the last acknowledged poll offset per (org, channel).
-- Restarting the worker resumes from here instead of re-polling from scratch.
CREATE TABLE IF NOT EXISTS channel_poll_cursor (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id TEXT NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  channel_plugin TEXT NOT NULL,
  cursor TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS channel_poll_cursor_org_plugin_unique
  ON channel_poll_cursor (org_id, channel_plugin);

-- inbound_dedup: a claimed key per dispatched update. The re-poll window after a
-- restart (and webhook retries) can re-deliver an update; claiming it here first
-- makes dispatch exactly-once. Pruned on a TTL by the worker.
CREATE TABLE IF NOT EXISTS inbound_dedup (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id TEXT NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  channel_plugin TEXT NOT NULL,
  update_key TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS inbound_dedup_org_plugin_key_unique
  ON inbound_dedup (org_id, channel_plugin, update_key);

CREATE INDEX IF NOT EXISTS inbound_dedup_created_idx
  ON inbound_dedup (created_at);
