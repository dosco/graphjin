-- ADM2 — multi-source registry. data_source rows get a stable per-org
-- name, a default flag (the source agents use unless told otherwise)
-- and an enabled switch. The oldest row per org becomes the default,
-- preserving today's single-source behavior.

ALTER TABLE data_source
  ADD COLUMN IF NOT EXISTS name text,
  ADD COLUMN IF NOT EXISTS is_default boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS enabled boolean NOT NULL DEFAULT true;

WITH ranked AS (
  SELECT id, row_number() OVER (PARTITION BY org_id ORDER BY created_at, id) AS rn
  FROM data_source
)
UPDATE data_source d
SET name = CASE WHEN r.rn = 1 THEN 'default' ELSE 'source-' || left(d.id::text, 8) END,
    is_default = (r.rn = 1)
FROM ranked r
WHERE r.id = d.id AND d.name IS NULL;

ALTER TABLE data_source
  ALTER COLUMN name SET DEFAULT 'default';
UPDATE data_source SET name = 'default' WHERE name IS NULL;
ALTER TABLE data_source
  ALTER COLUMN name SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS data_source_org_name_unique
  ON data_source (org_id, name);
