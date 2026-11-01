-- cancellation-spike.sql
-- Flips ~15% of orders from the past hour to status=6 (cancelled). Hourly
-- Quality Watch workflow should see the cancellation rate spike and emit
-- a mood=watch finding (informational, not action-needing).

\set ON_ERROR_STOP on

DO $$
DECLARE
  v_pool int;
  v_target int;
  v_flipped int;
BEGIN
  SELECT count(*) INTO v_pool
  FROM sales.salesorderheader
  WHERE orderdate > now() - interval '1 hour' AND status <> 6;

  v_target := GREATEST(1, (v_pool * 15) / 100);

  WITH flipped AS (
    UPDATE sales.salesorderheader
    SET status       = 6,
        modifieddate = now()
    WHERE salesorderid IN (
      SELECT salesorderid FROM sales.salesorderheader
      WHERE orderdate > now() - interval '1 hour' AND status <> 6
      ORDER BY random()
      LIMIT v_target
    )
    RETURNING salesorderid
  )
  SELECT count(*) INTO v_flipped FROM flipped;

  RAISE NOTICE '[trial-sim] cancellation-spike: flipped % of % past-hour orders to cancelled',
    v_flipped, v_pool;
END $$;
