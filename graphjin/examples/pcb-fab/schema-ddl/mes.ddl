# GraphJin DDL for the writable PCB MES source (Postgres in demo mode).

type customers {
  id: Bigint! @id
  account_id: Bigint!
  name: Text!
  segment: Text!
  priority_tier: Text!
}

type pcb_designs {
  id: Bigint! @id
  account_id: Bigint!
  customer_id: Bigint! @relation(type: customers, field: id)
  design_code: Varchar! @unique
  product_name: Text!
  layer_count: Integer!
  material: Text!
  target_impedance_ohms: Numeric! @type(args: "8, 2")
}

type design_revisions {
  id: Bigint! @id
  design_id: Bigint! @relation(type: pcb_designs, field: id)
  revision: Varchar!
  status: Varchar! @default(value: "'engineering'")
  gerber_path: Text!
  stackup_note: Text!
  created_at: TimestampWithTimeZone!
}

type fab_orders {
  id: Bigint! @id
  account_id: Bigint!
  customer_id: Bigint! @relation(type: customers, field: id)
  design_id: Bigint! @relation(type: pcb_designs, field: id)
  revision_id: Bigint! @relation(type: design_revisions, field: id)
  order_code: Varchar! @unique
  due_date: Date!
  panel_qty: Integer!
  status: Varchar! @default(value: "'engineering_hold'")
  priority: Integer! @default(value: "3")
}

type panels {
  id: Bigint! @id
  account_id: Bigint!
  order_id: Bigint! @relation(type: fab_orders, field: id)
  panel_code: Varchar! @unique
  current_step: Text!
  status: Varchar! @default(value: "'queued'")
  scrap_flag: Boolean! @default(value: "false")
}

type fab_runs {
  id: Bigint! @id
  order_id: Bigint! @relation(type: fab_orders, field: id)
  line: Text!
  scheduled_start: TimestampWithTimeZone!
  planned_minutes: Integer!
  status: Varchar! @default(value: "'planned'")
  engineer: Text!
}

type bom_items {
  id: Bigint! @id
  account_id: Bigint!
  design_id: Bigint! @relation(type: pcb_designs, field: id)
  sku: Varchar!
  description: Text!
  qty_per_board: Numeric! @type(args: "10, 3")
  criticality: Varchar! @default(value: "'standard'")
}

type shipments {
  id: Bigint! @id
  account_id: Bigint!
  order_id: Bigint! @relation(type: fab_orders, field: id)
  ship_date: Date!
  carrier: Text!
  status: Varchar! @default(value: "'pending'")
}

type ncr_reports {
  id: Bigint! @id
  account_id: Bigint!
  order_id: Bigint! @relation(type: fab_orders, field: id)
  opened_at: TimestampWithTimeZone!
  defect_code: Text!
  severity: Varchar! @default(value: "'normal'")
  status: Varchar! @default(value: "'open'")
}
