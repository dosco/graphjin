-- Human hours saved: agent-estimated minutes of human effort.
-- Per-action estimate lives on action_request; per-run analysis estimate
-- lives on work_run. Both are server-clamped before insert. Org/workflow
-- rollups sum executed actions + completed-run analysis at query time.

ALTER TABLE action_request
  ADD COLUMN minutes_saved integer,
  ADD COLUMN minutes_saved_basis text,
  ADD COLUMN estimate_source text NOT NULL DEFAULT 'agent',
  ADD COLUMN estimate_version integer NOT NULL DEFAULT 1;

ALTER TABLE work_run
  ADD COLUMN analysis_minutes_saved integer,
  ADD COLUMN analysis_minutes_basis text,
  ADD COLUMN estimate_version integer NOT NULL DEFAULT 1;

-- Window + cumulative rollups filter completed runs by org and finish time.
CREATE INDEX IF NOT EXISTS work_run_org_finished_idx
  ON work_run (org_id, finished_at);
