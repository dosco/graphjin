-- low-stock-reorder.sql
-- Drops the on-hand quantity for one popular product below its reorder
-- point. The "Stock Reorder Watch" workflow has a source_change
-- subscription on productinventory; GraphJin re-polls every 5s
-- (subs_poll_duration in db/graphjin/dev.example.yml) so the workflow
-- should fire within seconds of this script committing.
--
-- Picks the highest-quantity inventory row that's currently well above
-- reorder, so the drop is meaningful (i.e. a real "now below reorder"
-- event, not a row that was already below). Robust to whatever AW data
-- the operator's stack happens to have loaded.

\set ON_ERROR_STOP on

DO $$
DECLARE
  v_productid int;
  v_locationid int;
  v_reorder smallint;
  v_target_quantity smallint;
  v_product_name varchar;
BEGIN
  SELECT pi.productid, pi.locationid, p.reorderpoint, p.name
  INTO v_productid, v_locationid, v_reorder, v_product_name
  FROM production.productinventory pi
  JOIN production.product p ON p.productid = pi.productid
  WHERE pi.quantity > p.reorderpoint + 50
    AND p.finishedgoodsflag = 1
  ORDER BY pi.quantity DESC
  LIMIT 1;

  IF v_productid IS NULL THEN
    RAISE NOTICE '[trial-sim] low-stock-reorder: no eligible product found (every row already at or below reorder + 50)';
    RETURN;
  END IF;

  v_target_quantity := GREATEST(v_reorder - 5, 0)::smallint;

  UPDATE production.productinventory
  SET quantity = v_target_quantity,
      modifieddate = now()
  WHERE productid = v_productid AND locationid = v_locationid;

  RAISE NOTICE '[trial-sim] low-stock-reorder: % (productid=%, locationid=%) quantity → % (reorder=%)',
    v_product_name, v_productid, v_locationid, v_target_quantity, v_reorder;
END $$;
