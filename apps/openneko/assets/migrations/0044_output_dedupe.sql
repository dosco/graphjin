-- OL8 — card-level dedupe. An identical finding (same workflow + kind +
-- title) within 24h bumps the original card's seen_count ("2× today")
-- instead of creating a new card.

ALTER TABLE workflow_output
  ADD COLUMN IF NOT EXISTS seen_count integer NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS last_seen_at timestamptz,
  ADD COLUMN IF NOT EXISTS dedupe_key text;

UPDATE workflow_output SET last_seen_at = created_at WHERE last_seen_at IS NULL;
ALTER TABLE workflow_output
  ALTER COLUMN last_seen_at SET DEFAULT now();
ALTER TABLE workflow_output
  ALTER COLUMN last_seen_at SET NOT NULL;

CREATE INDEX IF NOT EXISTS workflow_output_dedupe_idx
  ON workflow_output (org_id, dedupe_key, created_at DESC)
  WHERE dedupe_key IS NOT NULL;
