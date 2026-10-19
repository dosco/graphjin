# GraphJin DDL for the read-only Snowflake yield warehouse simulator.

type yield_by_layer_count {
  id: Bigint! @id
  week_start: Date!
  layer_count: Integer!
  started_panels: Integer!
  passed_panels: Integer!
  yield_pct: Numeric! @type(args: "6, 2")
}

type defect_pareto {
  id: Bigint! @id
  week_start: Date!
  defect_code: Text!
  station: Text!
  defect_count: Integer!
  escaped_count: Integer!
}

type process_capability {
  id: Bigint! @id
  process_step: Text!
  metric: Text!
  cpk: Numeric! @type(args: "6, 2")
  sample_count: Integer!
  status: Text!
}

type wip_aging {
  id: Bigint! @id
  order_code: Text!
  current_step: Text!
  age_hours: Numeric! @type(args: "8, 2")
  blocker: Text!
}
