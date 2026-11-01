-- Dead-letter support for inbound dispatch. A persistently-failing ("poison")
-- update would otherwise hold the poll cursor forever, blocking every newer
-- message behind it. Track per-update attempts; after MAX retries park the
-- update as 'dead' (keeping its payload + last error for inspection) so the
-- cursor can advance past it.
--
-- status: 'pending' (claimed, dispatch in progress / retrying)
--       | 'done'    (dispatched OK — the dedup marker)
--       | 'dead'    (gave up after MAX attempts — a dead letter)
-- Existing rows are already-dispatched dedup markers, so they backfill to 'done'.
ALTER TABLE inbound_dedup
  ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'done',
  ADD COLUMN IF NOT EXISTS attempts INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS last_error TEXT,
  ADD COLUMN IF NOT EXISTS payload JSONB,
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Find dead letters fast for inspection / manual replay.
CREATE INDEX IF NOT EXISTS inbound_dedup_status_idx ON inbound_dedup (status);
