-- =========================================================
-- Denormalize metric_refresh status onto `metric` so the briefing
-- API doesn't need an N+1 join through processing_job to find which
-- cards failed. Without these columns:
--   - Failed metric_refresh jobs leave a card stuck on "Fetching…"
--     forever in the dashboard (no snapshot ever lands).
--   - The /api/briefing reader can't tell pending from failed.
--
-- Writers: apps/worker/src/jobs/metric-refresh.ts (success/failure paths)
-- and apps/worker/src/jobs/bootstrap-metrics-build.ts (initial 'pending'
-- stamp when bootstrap inserts new metric rows).
--
-- Idempotent: safe to re-run.
-- =========================================================

alter table metric add column if not exists last_refresh_status text;
alter table metric add column if not exists last_refresh_error text;
alter table metric add column if not exists last_refresh_job_id uuid
  references processing_job(id);

-- Quick lookup for the dashboard's "any pending refreshes for this org?" question.
create index if not exists metric_org_refresh_status_idx
  on metric (org_id, last_refresh_status)
  where last_refresh_status is not null;
