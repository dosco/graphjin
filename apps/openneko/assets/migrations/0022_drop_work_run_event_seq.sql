-- 0022 — drop the seq column on work_run_event.
--
-- seq existed as a per-run monotonic counter computed by callers (the
-- work-run agent loop AND the action-execute job that fires alongside
-- it). Both callers held their own local counters that they computed
-- by reading getWorkRunEvents().length + 1 — a classic TOCTOU race
-- when two writes landed close in time. The unique constraint on
-- (run_id, seq) surfaced the race as "duplicate key value violates
-- unique constraint work_run_event_run_seq_unique" errors in the run,
-- which the UI then rendered alongside legitimate events.
--
-- The fix is to stop trying to maintain a per-run counter in app code.
-- The `id` column is already a bigserial — globally monotonic, assigned
-- by Postgres, guaranteed unique. Ordering events within a run is now
-- "ORDER BY id ASC", and the cursor for the SSE tail is "id > $lastId".
-- The constraint and seq column both go away; new btree indexes on
-- (run_id, id) and (thread_id, id) cover the new query patterns.
DROP INDEX IF EXISTS work_run_event_thread_seq_idx;
ALTER TABLE work_run_event DROP CONSTRAINT IF EXISTS work_run_event_run_seq_unique;
DROP INDEX IF EXISTS work_run_event_run_seq_unique;
ALTER TABLE work_run_event DROP COLUMN IF EXISTS seq;

CREATE INDEX IF NOT EXISTS work_run_event_run_id_idx
  ON work_run_event (run_id, id);
CREATE INDEX IF NOT EXISTS work_run_event_thread_id_idx
  ON work_run_event (thread_id, id);
