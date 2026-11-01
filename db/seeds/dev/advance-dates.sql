-- advance-dates.sql
-- Idempotently shift operational AdventureWorks date columns forward so the
-- most-recent row in each logical group lands on or after CURRENT_DATE.
-- Runs at most once per 12 hours per database.
--
-- Why: AW2014 ships frozen historical data. The L1 trial sim only ticks
-- sales.salesorderheader (orderdate, shipdate). Production/manufacturing/
-- purchasing tables stay frozen and metric agents end up computing TTMs
-- against year-old data.
--
-- Sales is shifted in two pieces: the historical block gets pulled forward
-- so its max lands flush against where the sim begins producing, and the
-- sim-written rows are left untouched.
--
-- HR, product lifecycle, BOM, cost/listprice history, currency rate, and
-- product reviews are intentionally left alone — they are real historical
-- reference data, not operational throughput.

\set ON_ERROR_STOP on

DO $$
DECLARE
  last_at      timestamptz;
  shift_d      int;
  today        date := CURRENT_DATE;
  -- Orders whose modifieddate falls inside this window are sim-written.
  sim_cutoff   date := CURRENT_DATE - 30;
BEGIN
  SELECT advance_dates_at INTO last_at FROM trial_sim.state;
  IF last_at IS NOT NULL AND now() - last_at < interval '12 hours' THEN
    RAISE NOTICE '[advance-dates] skip — last run %', last_at;
    RETURN;
  END IF;

  -- Group 1: workorder + workorderrouting (one shift, applied to both).
  SELECT GREATEST(0, today - MAX(enddate)::date) INTO shift_d
  FROM production.workorder WHERE enddate IS NOT NULL;
  IF shift_d > 0 THEN
    RAISE NOTICE '[advance-dates] production.workorder + workorderrouting +% days', shift_d;
    UPDATE production.workorder
       SET startdate = startdate + (shift_d || ' days')::interval,
           enddate   = enddate   + (shift_d || ' days')::interval,
           duedate   = duedate   + (shift_d || ' days')::interval;
    UPDATE production.workorderrouting
       SET actualstartdate    = actualstartdate    + (shift_d || ' days')::interval,
           actualenddate      = actualenddate      + (shift_d || ' days')::interval,
           scheduledstartdate = scheduledstartdate + (shift_d || ' days')::interval,
           scheduledenddate   = scheduledenddate   + (shift_d || ' days')::interval;
  END IF;

  -- Group 2: production.transactionhistory.
  SELECT GREATEST(0, today - MAX(transactiondate)::date) INTO shift_d
  FROM production.transactionhistory;
  IF shift_d > 0 THEN
    RAISE NOTICE '[advance-dates] production.transactionhistory +% days', shift_d;
    UPDATE production.transactionhistory
       SET transactiondate = transactiondate + (shift_d || ' days')::interval;
  END IF;

  -- Group 3: sales historical block. The sim stamps modifieddate with the
  -- order time; historical rows keep their original modifieddate. The most
  -- recent historical order lands the day before the sim's first order, or
  -- yesterday when the sim has written nothing yet, so live data follows the
  -- history without a gap.
  SELECT GREATEST(0, (COALESCE(
           (SELECT MIN(orderdate)::date FROM sales.salesorderheader WHERE modifieddate >= sim_cutoff),
           today) - 1) - MAX(orderdate)::date) INTO shift_d
  FROM sales.salesorderheader WHERE modifieddate < sim_cutoff;
  IF shift_d > 0 THEN
    RAISE NOTICE '[advance-dates] sales.salesorderheader (historical) +% days', shift_d;
    UPDATE sales.salesorderheader
       SET orderdate = orderdate + (shift_d || ' days')::interval,
           shipdate  = shipdate  + (shift_d || ' days')::interval,
           duedate   = duedate   + (shift_d || ' days')::interval
     WHERE modifieddate < sim_cutoff;
  END IF;

  -- Group 4: purchasing (header + detail + productvendor.lastreceiptdate, one shift).
  SELECT GREATEST(0, today - MAX(orderdate)::date) INTO shift_d
  FROM purchasing.purchaseorderheader;
  IF shift_d > 0 THEN
    RAISE NOTICE '[advance-dates] purchasing.purchaseorderheader/detail + productvendor +% days', shift_d;
    UPDATE purchasing.purchaseorderheader
       SET orderdate = orderdate + (shift_d || ' days')::interval,
           shipdate  = shipdate  + (shift_d || ' days')::interval;
    UPDATE purchasing.purchaseorderdetail
       SET duedate = duedate + (shift_d || ' days')::interval;
    UPDATE purchasing.productvendor
       SET lastreceiptdate = lastreceiptdate + (shift_d || ' days')::interval
     WHERE lastreceiptdate IS NOT NULL;
  END IF;

  UPDATE trial_sim.state SET advance_dates_at = now();
END $$;
