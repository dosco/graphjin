# GraphJin DDL for the writable roastery operations source.
# Applied by `graphjin serve --demo --path examples/coffee-roastery`.

type customers {
  id: Bigint! @id
  name: Text!
  email: Text! @unique
  segment: Text!
  account_tier: Text!
  preferred_origin: Text
  created_at: TimestampWithTimeZone! @default(value: "now()")
}

type green_lots {
  id: Bigint! @id
  lot_code: Text! @unique
  origin: Text!
  producer: Text!
  process: Text!
  arrival_date: Date!
  remaining_kg: Numeric! @type(args: "10, 2")
  target_cost_per_kg: Numeric! @type(args: "10, 2")
  cupping_score: Numeric! @type(args: "4, 2")
}

type roast_profiles {
  id: Bigint! @id
  profile_name: Text!
  green_lot_id: Bigint! @relation(type: green_lots, field: id)
  charge_temp_c: Numeric! @type(args: "6, 2")
  development_target_seconds: Integer!
  drop_temp_c: Numeric! @type(args: "6, 2")
  notes: Text
}

type roast_schedule {
  id: Bigint! @id
  profile_id: Bigint! @relation(type: roast_profiles, field: id)
  scheduled_for: TimestampWithTimeZone!
  machine_id: Text!
  target_output_kg: Numeric! @type(args: "10, 2")
  status: Text! @default(value: "'planned'")
  operator: Text!
}

type production_orders {
  id: Bigint! @id
  customer_id: Bigint! @relation(type: customers, field: id)
  roast_schedule_id: Bigint @relation(type: roast_schedule, field: id)
  product_name: Text!
  quantity_bags: Integer!
  bag_size_g: Integer!
  requested_ship_date: Date!
  status: Text! @default(value: "'queued'")
  priority: Integer! @default(value: "3")
}

type subscriptions {
  id: Bigint! @id
  customer_id: Bigint! @relation(type: customers, field: id)
  product_name: Text!
  cadence_days: Integer!
  next_ship_date: Date!
  bags_per_shipment: Integer!
  active: Boolean! @default(value: "true")
}

type customer_tickets {
  id: Bigint! @id
  customer_id: Bigint! @relation(type: customers, field: id)
  production_order_id: Bigint @relation(type: production_orders, field: id)
  subject: Text!
  body: Text!
  severity: Text! @default(value: "'normal'")
  status: Text! @default(value: "'open'")
  created_at: TimestampWithTimeZone! @default(value: "now()")
}

type inventory_adjustments {
  id: Bigint! @id
  green_lot_id: Bigint! @relation(type: green_lots, field: id)
  adjustment_kg: Numeric! @type(args: "10, 2")
  reason: Text!
  created_at: TimestampWithTimeZone! @default(value: "now()")
}
