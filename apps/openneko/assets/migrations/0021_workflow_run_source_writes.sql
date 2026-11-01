-- workflow_run.source_writes — array of {table, primary_key} objects
-- recording which data-source rows the run mutated (via action_request
-- adapters). Read by match-handler's source_change cycle check to drop
-- a re-fire whose incoming (table, pk) appears in a recent run of the
-- same subscription.
--
-- jsonb default '[]'::jsonb so existing rows are valid without backfill.
-- Index uses jsonb_path_ops over the array contents — match-handler
-- queries with `source_writes @> '[{"table":"...","primary_key":{...}}]'`.

ALTER TABLE workflow_run
ADD COLUMN source_writes jsonb NOT NULL DEFAULT '[]'::jsonb;

CREATE INDEX workflow_run_source_writes_gin_idx
  ON workflow_run USING gin (source_writes jsonb_path_ops);
