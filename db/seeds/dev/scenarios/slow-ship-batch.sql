-- slow-ship-batch.sql
-- Ages 8 random pending orders so they breach the slow-ship SLA. Slow-Ship
-- Operations workflow (daily cron) should pick them up on its next run.

\set ON_ERROR_STOP on

DO $$
DECLARE
  v_aged int;
BEGIN
  WITH aged AS (
    UPDATE sales.salesorderheader
    SET orderdate    = now() - interval '7 days',
        duedate      = now() - interval '2 days',
        modifieddate = now()
    WHERE salesorderid IN (
      SELECT salesorderid FROM sales.salesorderheader
      WHERE status = 1 AND shipdate IS NULL
      ORDER BY random()
      LIMIT 8
    )
    RETURNING salesorderid
  )
  SELECT count(*) INTO v_aged FROM aged;

  RAISE NOTICE '[trial-sim] slow-ship-batch: aged % pending orders past SLA', v_aged;
END $$;
