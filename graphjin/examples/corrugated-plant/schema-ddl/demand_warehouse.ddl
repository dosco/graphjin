# GraphJin DDL for the read-only demand warehouse simulator.

type demand_history {
  id: Bigint! @id
  customer_id: Bigint!
  board_grade_code: Text!
  week_start: Date!
  ordered_m_2: Numeric! @type(args: "12, 2")
  rush_m_2: Numeric! @type(args: "12, 2")
}

type material_price_index {
  id: Bigint! @id
  paper_type: Text!
  week_start: Date!
  price_per_kg: Numeric! @type(args: "10, 2")
  trend: Text!
}

type oee_daily {
  id: Bigint! @id
  machine_code: Text!
  production_date: Date!
  availability_pct: Numeric! @type(args: "6, 2")
  performance_pct: Numeric! @type(args: "6, 2")
  quality_pct: Numeric! @type(args: "6, 2")
  oee_pct: Numeric! @type(args: "6, 2")
}

type scrap_rates {
  id: Bigint! @id
  machine_code: Text!
  production_date: Date!
  board_grade_code: Text!
  scrap_pct: Numeric! @type(args: "6, 2")
  cause: Text!
}
