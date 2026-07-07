const source = { source: "demand_warehouse" };

seed.insert(
  "demand_history",
  [
    { id: 1, customer_id: 1, board_grade_code: "B32-K", week_start: "2026-05-18", ordered_m_2: 16200.0, rush_m_2: 1200.0 },
    { id: 2, customer_id: 1, board_grade_code: "B32-K", week_start: "2026-05-25", ordered_m_2: 17100.0, rush_m_2: 1600.0 },
    { id: 3, customer_id: 1, board_grade_code: "B32-K", week_start: "2026-06-01", ordered_m_2: 18400.0, rush_m_2: 2200.0 },
    { id: 4, customer_id: 1, board_grade_code: "B32-K", week_start: "2026-06-08", ordered_m_2: 19050.0, rush_m_2: 2600.0 },
    { id: 5, customer_id: 3, board_grade_code: "E26-R", week_start: "2026-06-15", ordered_m_2: 8800.0, rush_m_2: 900.0 },
    { id: 6, customer_id: 5, board_grade_code: "B32-K", week_start: "2026-06-22", ordered_m_2: 12100.0, rush_m_2: 1400.0 },
    { id: 7, customer_id: 2, board_grade_code: "C44-H", week_start: "2026-06-29", ordered_m_2: 24900.0, rush_m_2: 800.0 },
    { id: 8, customer_id: 4, board_grade_code: "C44-H", week_start: "2026-07-06", ordered_m_2: 21100.0, rush_m_2: 500.0 },
  ],
  source
);

seed.insert(
  "material_price_index",
  [
    { id: 1, paper_type: "kraft liner", week_start: "2026-06-22", price_per_kg: 0.86, trend: "up" },
    { id: 2, paper_type: "recycled medium", week_start: "2026-06-22", price_per_kg: 0.62, trend: "flat" },
    { id: 3, paper_type: "heavy kraft liner", week_start: "2026-06-29", price_per_kg: 0.94, trend: "up" },
    { id: 4, paper_type: "semi-chemical medium", week_start: "2026-06-29", price_per_kg: 0.71, trend: "up" },
    { id: 5, paper_type: "light kraft liner", week_start: "2026-07-06", price_per_kg: 0.79, trend: "flat" },
  ],
  source
);

seed.insert(
  "oee_daily",
  [
    { id: 1, machine_code: "CORR-1", production_date: "2026-07-03", availability_pct: 92.5, performance_pct: 88.0, quality_pct: 96.0, oee_pct: 78.1 },
    { id: 2, machine_code: "CORR-2", production_date: "2026-07-03", availability_pct: 95.0, performance_pct: 84.5, quality_pct: 97.0, oee_pct: 77.8 },
    { id: 3, machine_code: "CORR-1", production_date: "2026-07-04", availability_pct: 86.0, performance_pct: 82.0, quality_pct: 94.0, oee_pct: 66.3 },
    { id: 4, machine_code: "CORR-1", production_date: "2026-07-06", availability_pct: 78.0, performance_pct: 80.0, quality_pct: 91.0, oee_pct: 56.8 },
  ],
  source
);

seed.insert(
  "scrap_rates",
  [
    { id: 1, machine_code: "CORR-1", production_date: "2026-07-03", board_grade_code: "B32-K", scrap_pct: 3.8, cause: "trim" },
    { id: 2, machine_code: "CORR-2", production_date: "2026-07-03", board_grade_code: "E26-R", scrap_pct: 2.9, cause: "setup" },
    { id: 3, machine_code: "CORR-1", production_date: "2026-07-04", board_grade_code: "C44-H", scrap_pct: 5.4, cause: "warp" },
    { id: 4, machine_code: "CORR-1", production_date: "2026-07-06", board_grade_code: "B32-K", scrap_pct: 6.2, cause: "splicer" },
  ],
  source
);
