-- approved-action-history.sql
-- DEFERRED. The meta scenario auto-approves stale trial-safe action
-- requests to keep the approvals queue from growing indefinitely during
-- long trials. The action_request table lives in the OpenNeko DB
-- (neko-db), not AdventureWorks — supporting it would require either
-- giving this injector container neko-db credentials or using FDW.
--
-- Skipped for the L3 first cut. Re-enable by:
--   1. Adding NEKO_PG_* env vars to the scenario-injector compose service.
--   2. Extending scenario-injector.sh to route this scenario to neko-db
--      (e.g. via a per-row target_db column on trial_sim.scenario).
--   3. Writing the actual UPDATE here.

\set ON_ERROR_STOP on

DO $$ BEGIN
  RAISE NOTICE '[trial-sim] approved-action-history: deferred (cross-DB), no-op';
END $$;
