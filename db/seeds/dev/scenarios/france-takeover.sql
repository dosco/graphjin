-- france-takeover.sql
-- Installs a 12-hour filter boosting France (territory 7) orders 3x. L1
-- trickle reads the filter and upweights territory 7 in its sampling.
-- Should trip both Top Territory Watch (weekly cron) and Inventory Watch
-- (subscription) — demonstrates a multi-workflow fan-out from one event.

\set ON_ERROR_STOP on

INSERT INTO trial_sim.active_filter (scenario_id, kind, payload, expires_at)
VALUES (
  'france-takeover',
  'boost_territory',
  jsonb_build_object('territory_id', 7, 'territory_name', 'France', 'multiplier', 3),
  now() + interval '12 hours'
);

DO $$ BEGIN
  RAISE NOTICE '[trial-sim] france-takeover: boosting territory 7 by 3x until %',
    now() + interval '12 hours';
END $$;
