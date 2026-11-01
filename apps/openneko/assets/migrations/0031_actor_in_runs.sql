-- K1 — the keystone: every run carries the acting principal. Runs were
-- orgId-only; personas, memory forks, GraphJin RBAC (per-run actor JWTs),
-- channel→user mapping, and dual-identity audit all hang off these
-- columns. Nullable: legacy rows and service-initiated runs have no
-- human actor; actor_role is snapshotted at run start (a later role
-- change doesn't retro-affect a running turn).
--   web run     → (app_user.id, app_user.role)
--   channel run → (NULL, 'member')   until CH3 links the sender
--   cron/workflow run → (NULL, 'service')

ALTER TABLE work_run
  ADD COLUMN IF NOT EXISTS actor_user_id text REFERENCES app_user(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS actor_role text;

ALTER TABLE work_thread
  ADD COLUMN IF NOT EXISTS created_by_user_id text REFERENCES app_user(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS work_run_org_actor_idx
  ON work_run (org_id, actor_user_id);
