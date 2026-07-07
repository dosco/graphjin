# GraphJin DDL for the writable corrugated ERP source (MySQL in demo mode).

type customers {
  id: Bigint! @id
  account_id: Bigint!
  name: Text!
  segment: Text!
  ship_city: Text!
  priority_tier: Text!
}

type board_grades {
  id: Bigint! @id
  grade_code: Varchar! @unique
  flute: Text!
  liner_gsm: Integer!
  medium_gsm: Integer!
  target_strength_ect: Numeric! @type(args: "10, 2")
}

type paper_rolls {
  id: Bigint! @id
  account_id: Bigint!
  roll_code: Varchar! @unique
  paper_type: Text!
  gsm: Integer!
  remaining_kg: Numeric! @type(args: "10, 2")
  reorder_point_kg: Numeric! @type(args: "10, 2")
  supplier: Text!
  status: Varchar! @default(value: "'available'")
}

type corrugator_machines {
  id: Bigint! @id
  machine_code: Varchar! @unique
  name: Text!
  max_m_per_min: Integer!
  status: Varchar! @default(value: "'ready'")
}

type work_orders {
  id: Bigint! @id
  account_id: Bigint!
  customer_id: Bigint! @relation(type: customers, field: id)
  board_grade_id: Bigint! @relation(type: board_grades, field: id)
  order_code: Varchar! @unique
  due_date: Date!
  quantity_m2: Numeric! @type(args: "12, 2")
  status: Varchar! @default(value: "'queued'")
  priority: Integer! @default(value: "3")
}

type corrugator_runs {
  id: Bigint! @id
  work_order_id: Bigint! @relation(type: work_orders, field: id)
  machine_id: Bigint! @relation(type: corrugator_machines, field: id)
  scheduled_start: TimestampWithTimeZone!
  planned_minutes: Integer!
  actual_minutes: Integer
  status: Varchar! @default(value: "'planned'")
  operator: Text!
}

type converting_jobs {
  id: Bigint! @id
  work_order_id: Bigint! @relation(type: work_orders, field: id)
  job_code: Varchar! @unique
  station: Text!
  status: Varchar! @default(value: "'queued'")
  planned_minutes: Integer!
}

type shipments {
  id: Bigint! @id
  account_id: Bigint!
  work_order_id: Bigint! @relation(type: work_orders, field: id)
  ship_date: Date!
  carrier: Text!
  status: Varchar! @default(value: "'pending'")
}

type downtime_events {
  id: Bigint! @id
  machine_id: Bigint! @relation(type: corrugator_machines, field: id)
  occurred_at: TimestampWithTimeZone!
  minutes_down: Integer!
  reason: Text!
  severity: Varchar! @default(value: "'normal'")
  status: Varchar! @default(value: "'open'")
}

type quality_holds {
  id: Bigint! @id
  account_id: Bigint!
  work_order_id: Bigint! @relation(type: work_orders, field: id)
  created_at: TimestampWithTimeZone!
  defect_type: Text!
  severity: Varchar! @default(value: "'normal'")
  status: Varchar! @default(value: "'open'")
}
