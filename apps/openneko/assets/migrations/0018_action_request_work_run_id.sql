-- Links an action_request back to the /work run that emitted it, so
-- the worker can emit an action_request_result event into the right
-- run (and the chat UI can render the result inline next to the
-- agent's "I'd like to do X" turn).
--
-- Nullable: action_requests emitted by the workflow runner (the
-- existing pattern) don't have a work_run; they reference
-- workflow_run_id instead. The /work tool builder is the first
-- caller that needs this back-reference.
--
-- ON DELETE SET NULL so deleting a thread (which cascades through
-- work_run) doesn't take its action_requests with it — the request
-- log survives for audit even after the originating thread is gone.
ALTER TABLE action_request
ADD COLUMN work_run_id uuid REFERENCES work_run(id) ON DELETE SET NULL;

CREATE INDEX action_request_work_run_idx
  ON action_request(work_run_id, created_at DESC);
