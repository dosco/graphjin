-- mountain-200-spike.sql
-- Injects 25 orders heavily loaded with productid=775 (Mountain-200 Black,
-- 38) so the SKU's hourly rate jumps >2σ above its rolling avg. Should
-- trip Inventory Watch via its subscription on Daily Revenue's output.

\set ON_ERROR_STOP on

DO $$
DECLARE
  v_orders_to_create int := 25;
  v_orderid          int;
  v_customerid       int;
  v_territoryid      int;
  v_salespersonid    int;
  v_storeid          int;
  v_addressid        int;
  v_shipmethodid     int;
  v_specialofferid   int;
  v_unitprice        numeric;
  v_qty              smallint;
  v_subtotal         numeric;
  v_order_ts         timestamp;
  v_is_store         boolean;
  i                  int;
  salesperson_ids    int[] := ARRAY[274,275,276,277,278,279,280,281,282,283,284,285,286,287,288,289,290];
  v_productid        int := 775;  -- Mountain-200 Black, 38
BEGIN
  FOR i IN 1..v_orders_to_create LOOP
    -- Pick any territory and customer; the spike is about productid, not geo.
    SELECT customerid, storeid, territoryid INTO v_customerid, v_storeid, v_territoryid
    FROM sales.customer
    WHERE territoryid IS NOT NULL
    ORDER BY random() LIMIT 1;

    v_is_store := v_storeid IS NOT NULL;
    IF v_is_store OR random() < 0.4 THEN
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
    v_order_ts     := now() - (floor(random() * 30) * interval '1 minute');

    INSERT INTO sales.salesorderheader (
      orderdate, duedate, shipdate, status, onlineorderflag,
      customerid, salespersonid, territoryid,
      billtoaddressid, shiptoaddressid, shipmethodid,
      subtotal, taxamt, freight, modifieddate
    ) VALUES (
      v_order_ts, v_order_ts + interval '12 days', NULL, 1, NOT v_is_store,
      v_customerid, v_salespersonid, v_territoryid,
      v_addressid, v_addressid, v_shipmethodid,
      0, 0, 0, v_order_ts
    ) RETURNING salesorderid INTO v_orderid;

    SELECT sop.specialofferid, p.listprice
    INTO v_specialofferid, v_unitprice
    FROM sales.specialofferproduct sop
    JOIN production.product p ON p.productid = sop.productid
    WHERE sop.productid = v_productid
    LIMIT 1;

    -- 1-3 units per order; stores order in volume
    v_qty := CASE WHEN v_is_store THEN (1 + floor(random() * 5))::smallint
                  ELSE (1 + floor(random() * 2))::smallint END;

    INSERT INTO sales.salesorderdetail (
      salesorderid, orderqty, productid, specialofferid,
      unitprice, unitpricediscount, modifieddate
    ) VALUES (
      v_orderid, v_qty, v_productid, v_specialofferid,
      v_unitprice, 0, v_order_ts
    );

    v_subtotal := v_qty * v_unitprice;
    UPDATE sales.salesorderheader SET
      subtotal = round(v_subtotal, 2),
      taxamt   = round(v_subtotal * 0.08, 2),
      freight  = round(v_subtotal * 0.025, 2),
      totaldue = round(v_subtotal, 2) + round(v_subtotal * 0.08, 2) + round(v_subtotal * 0.025, 2)
    WHERE salesorderid = v_orderid;
  END LOOP;

  RAISE NOTICE '[trial-sim] mountain-200-spike: injected % orders for product 775',
    v_orders_to_create;
END $$;
