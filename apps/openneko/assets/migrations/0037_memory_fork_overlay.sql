-- CV2 — memory fork overlay (copy-on-write personal layers).
--   user_id NULL          = team/org layer (every existing row)
--   user_id = <app_user>  = that member's personal layer
--   origin_id             = stable identity across copy/promote (self for
--                           originals)
--   overrides_origin_id   = set when a personal row shadows the team row
--                           of that origin (edit) — readers drop the
--                           shadowed team row for that user
--   suppressed            = copy-on-write delete: hides the overridden
--                           team row for that user without touching it
--   promoted_*            = lineage when an admin promotes a personal
--                           memory into the team layer
-- DATA_LIFECYCLE §2: offboarding hard-deletes user_id = U rows.

ALTER TABLE work_memory
  ADD COLUMN IF NOT EXISTS user_id text REFERENCES app_user(id) ON DELETE CASCADE,
  ADD COLUMN IF NOT EXISTS origin_id uuid,
  ADD COLUMN IF NOT EXISTS overrides_origin_id uuid,
  ADD COLUMN IF NOT EXISTS suppressed boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS promoted_from_id uuid,
  ADD COLUMN IF NOT EXISTS promoted_by text,
  ADD COLUMN IF NOT EXISTS promoted_at timestamptz;

UPDATE work_memory SET origin_id = id WHERE origin_id IS NULL;

CREATE INDEX IF NOT EXISTS work_memory_org_user_idx
  ON work_memory (org_id, user_id, archived_at, updated_at DESC);
CREATE INDEX IF NOT EXISTS work_memory_overrides_idx
  ON work_memory (org_id, user_id, overrides_origin_id)
  WHERE overrides_origin_id IS NOT NULL;
