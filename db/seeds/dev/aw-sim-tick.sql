-- aw-sim-tick.sql
-- One tick of the AdventureWorks trial simulator. Generates a small batch
-- of realistic sales orders so OpenNeko surfaces are reacting to live
-- data during a trial.
--
-- Runs inside the adventureworks-simulator service in
-- compose.adventureworks.yml. The service wraps this in a sleep loop —
-- each tick inserts AW_SIM_ORDERS_MIN..AW_SIM_ORDERS_MAX orders.
--
-- Realistic-ish patterns (lifted in shape from Amit's local cron script):
--   - Territory weighted by historical order volume
--   - Customer picked from selected territory (preserves customer-territory link)
--   - Stores get larger line-item counts than individuals
--   - Products weighted toward historical top-20 (80% of picks)
--   - Realistic per-line discount distribution

\set ON_ERROR_STOP on

-- The caller sets trial_sim.orders_per_tick before \i'ing this file.
-- psql's :'X' substitution doesn't reach inside $$-quoted blocks, so a
-- session GUC is the cleanest handoff that works across psql versions.
DO $$
DECLARE
  v_orders_per_tick int := current_setting('trial_sim.orders_per_tick')::int;
  v_orderid int;
  v_customerid int;
  v_territoryid int;
  v_salespersonid int;
  v_storeid int;
  v_addressid int;
  v_shipmethodid int;
  v_productid int;
  v_specialofferid int;
  v_qty smallint;
  v_unitprice numeric;
  v_subtotal numeric;
  v_tax numeric;
  v_freight numeric;
  v_order_ts timestamp;
  v_shipdate timestamp;
  v_is_store boolean;
  v_num_items int;
  i int;
  j int;
  k int;

  salesperson_ids int[] := ARRAY[274,275,276,277,278,279,280,281,282,283,284,285,286,287,288,289,290];

  -- Territory historical weights (NW US, NE US, Central US, SW US, SE US,
  -- Canada, France, Germany, Australia, UK)
  terr_weights int[] := ARRAY[4594, 352, 385, 6224, 486, 4067, 2672, 2623, 6843, 3219];
  -- Per-tick effective weights, after applying any active trial_sim filters
  -- (exclude_territory zeroes a slot, boost_territory multiplies it).
  eff_weights int[];
  eff_total int := 0;
  has_trial_sim boolean;
  filter_rec record;

  -- Top-20 historical products
  pop_products int[] := ARRAY[
    712, 870, 711, 715, 708, 707, 864, 873, 884, 714,
    859, 863, 877, 867, 869, 876, 921, 782, 716, 883
  ];
  pop_weights int[] := ARRAY[
    8311, 15131, 21874, 28466, 35004, 41271, 45523, 49388, 53252, 56888,
    60352, 63730, 67049, 70345, 73589, 76755, 79850, 82833, 85813, 88661
  ];
  pop_max int := 88661;
BEGIN
  -- Apply trial_sim.active_filter (L3) if the schema is present. Excludes
  -- zero out a territory's weight; boosts multiply. The schema may be
  -- absent when L1 runs without the L3 injector — fall back to the
  -- historical weights unchanged.
  eff_weights := terr_weights;
  SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'trial_sim')
    INTO has_trial_sim;

  IF has_trial_sim THEN
    FOR filter_rec IN
      SELECT kind, payload FROM trial_sim.active_filter
      WHERE expires_at > now()
    LOOP
      k := (filter_rec.payload->>'territory_id')::int;
      IF k IS NULL OR k < 1 OR k > 10 THEN CONTINUE; END IF;
      IF filter_rec.kind = 'exclude_territory' THEN
        eff_weights[k] := 0;
      ELSIF filter_rec.kind = 'boost_territory' THEN
        eff_weights[k] := eff_weights[k]
          * COALESCE((filter_rec.payload->>'multiplier')::int, 1);
      END IF;
    END LOOP;
  END IF;

  FOR k IN 1..10 LOOP eff_total := eff_total + eff_weights[k]; END LOOP;
  IF eff_total <= 0 THEN
    -- Every territory excluded — degenerate. Restore historical weights so
    -- the tick still makes progress.
    eff_weights := terr_weights;
    eff_total   := 31465;
  END IF;

  FOR i IN 1..v_orders_per_tick LOOP
    -- Pick territory weighted by effective (post-filter) weights.
    DECLARE
      v_rand int := floor(random() * eff_total)::int;
      v_cumul int := 0;
    BEGIN
      v_territoryid := 10;
      FOR k IN 1..10 LOOP
        v_cumul := v_cumul + eff_weights[k];
        IF v_rand < v_cumul THEN
          v_territoryid := k;
          EXIT;
        END IF;
      END LOOP;
    END;

    SELECT customerid, storeid INTO v_customerid, v_storeid
    FROM sales.customer
    WHERE territoryid = v_territoryid
    ORDER BY random() LIMIT 1;

    -- If the AW seed lacks customers for a territory, fall back to any
    -- customer so the tick still makes progress.
    IF v_customerid IS NULL THEN
      SELECT customerid, storeid INTO v_customerid, v_storeid
      FROM sales.customer
      ORDER BY random() LIMIT 1;
    END IF;

    v_is_store := v_storeid IS NOT NULL;

    IF v_is_store OR random() < 0.3 THEN
      v_salespersonid := salesperson_ids[1 + floor(random() * array_length(salesperson_ids, 1))::int];
    ELSE
      v_salespersonid := NULL;
    END IF;

    SELECT a.addressid INTO v_addressid
    FROM person.businessentityaddress bea
    JOIN person.address a ON a.addressid = bea.addressid
    JOIN person.stateprovince sp ON sp.stateprovinceid = a.stateprovinceid
    WHERE sp.territoryid = v_territoryid
    ORDER BY random() LIMIT 1;

    IF v_addressid IS NULL THEN
      SELECT addressid INTO v_addressid FROM person.address ORDER BY random() LIMIT 1;
    END IF;

    v_shipmethodid := 1 + floor(random() * 5)::int;
    v_order_ts := now() - (floor(random() * 30) * interval '1 minute');

    IF random() < 0.7 THEN
      v_shipdate := v_order_ts + interval '1 day' * (2 + floor(random() * 6))::int;
      IF v_shipdate > now() THEN v_shipdate := NULL; END IF;
    ELSE
      v_shipdate := NULL;
    END IF;

    INSERT INTO sales.salesorderheader (
      orderdate, duedate, shipdate, status, onlineorderflag,
      customerid, salespersonid, territoryid,
      billtoaddressid, shiptoaddressid, shipmethodid,
      subtotal, taxamt, freight, modifieddate
    ) VALUES (
      v_order_ts,
      v_order_ts + interval '12 days',
      v_shipdate,
      CASE WHEN v_shipdate IS NOT NULL THEN 5 ELSE 1 END,
      NOT v_is_store,
      v_customerid, v_salespersonid, v_territoryid,
      v_addressid, v_addressid, v_shipmethodid,
      0, 0, 0, v_order_ts
    ) RETURNING salesorderid INTO v_orderid;

    IF v_is_store THEN
      v_num_items := 8 + floor(random() * 18)::int;
    ELSE
      v_num_items := 1 + floor(random() * 4)::int;
    END IF;

    v_subtotal := 0;

    FOR j IN 1..v_num_items LOOP
      IF random() < 0.8 THEN
        DECLARE
          v_prand int := floor(random() * pop_max)::int;
        BEGIN
          v_productid := pop_products[20];
          FOR k IN 1..20 LOOP
            IF v_prand < pop_weights[k] THEN
              v_productid := pop_products[k];
              EXIT;
            END IF;
          END LOOP;
        END;
        SELECT sop.specialofferid, p.listprice
        INTO v_specialofferid, v_unitprice
        FROM sales.specialofferproduct sop
        JOIN production.product p ON p.productid = sop.productid
        WHERE sop.productid = v_productid
        LIMIT 1;
      ELSE
        SELECT sop.productid, sop.specialofferid, p.listprice
        INTO v_productid, v_specialofferid, v_unitprice
        FROM sales.specialofferproduct sop
        JOIN production.product p ON p.productid = sop.productid
        WHERE p.listprice > 0
        ORDER BY random() LIMIT 1;
      END IF;

      IF v_is_store THEN
        v_qty := (1 + floor(random() * 6))::smallint;
      ELSE
        v_qty := (1 + floor(random() * 2))::smallint;
      END IF;

      DECLARE
        v_discount numeric := 0;
      BEGIN
        IF random() < 0.1 THEN
          v_discount := round((random() * 0.15 + 0.05)::numeric, 2);
        END IF;
        INSERT INTO sales.salesorderdetail (
          salesorderid, orderqty, productid, specialofferid,
          unitprice, unitpricediscount, modifieddate
        ) VALUES (
          v_orderid, v_qty, v_productid, v_specialofferid,
          v_unitprice, v_discount, v_order_ts
        );
        v_subtotal := v_subtotal + (v_qty * v_unitprice * (1 - v_discount));
      END;
    END LOOP;

    v_tax := round(v_subtotal * 0.08, 2);
    v_freight := round(v_subtotal * 0.025, 2);
    UPDATE sales.salesorderheader SET
      subtotal = round(v_subtotal, 2),
      taxamt = v_tax,
      freight = v_freight,
      totaldue = round(v_subtotal, 2) + v_tax + v_freight
    WHERE salesorderid = v_orderid;
  END LOOP;

  RAISE NOTICE '[aw-sim] tick inserted % orders at %', v_orders_per_tick, now();
END $$;
