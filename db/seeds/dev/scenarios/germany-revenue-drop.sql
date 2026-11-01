-- germany-revenue-drop.sql
-- Installs a 3-hour filter that the L1 trickle reads, excluding territory
-- 8 (Germany) from its order sampling. Hourly Revenue Drop Alert workflow
-- should fire shortly after the window opens because Germany's hourly
-- revenue cratered vs the trailing 7-day same-hour baseline.

\set ON_ERROR_STOP on

INSERT INTO trial_sim.active_filter (scenario_id, kind, payload, expires_at)
VALUES (
  'germany-revenue-drop',
  'exclude_territory',
  jsonb_build_object('territory_id', 8, 'territory_name', 'Germany'),
  now() + interval '3 hours'
);

DO $$ BEGIN
  RAISE NOTICE '[trial-sim] germany-revenue-drop: excluding territory 8 until %',
    now() + interval '3 hours';
END $$;
