const source = { source: "yield_warehouse" };

// Yield history is anchored to the seed run (UTC): week_start rows are the
// two completed Mondays before the current week.
const DAY_MS = 86400000;
const seedNowMs = Date.now();

function demoDay(offsetDays) {
  return new Date(seedNowMs + offsetDays * DAY_MS).toISOString().slice(0, 10);
}

function demoWeekStart(offsetWeeks) {
  const monday = (new Date(seedNowMs).getUTCDay() + 6) % 7;
  return new Date(seedNowMs + (offsetWeeks * 7 - monday) * DAY_MS).toISOString().slice(0, 10);
}

seed.insert(
  "yield_by_layer_count",
  [
    { id: 1, week_start: demoWeekStart(-2), layer_count: 4, started_panels: 820, passed_panels: 787, yield_pct: 95.98 },
    { id: 2, week_start: demoWeekStart(-2), layer_count: 6, started_panels: 640, passed_panels: 586, yield_pct: 91.56 },
    { id: 3, week_start: demoWeekStart(-2), layer_count: 8, started_panels: 420, passed_panels: 356, yield_pct: 84.76 },
    { id: 4, week_start: demoWeekStart(-1), layer_count: 4, started_panels: 880, passed_panels: 846, yield_pct: 96.14 },
    { id: 5, week_start: demoWeekStart(-1), layer_count: 6, started_panels: 710, passed_panels: 654, yield_pct: 92.11 },
    { id: 6, week_start: demoWeekStart(-1), layer_count: 8, started_panels: 460, passed_panels: 382, yield_pct: 83.04 },
  ],
  source
);

seed.insert(
  "defect_pareto",
  [
    { id: 1, week_start: demoWeekStart(-1), defect_code: "PLATING-VOID", station: "plating", defect_count: 38, escaped_count: 4 },
    { id: 2, week_start: demoWeekStart(-1), defect_code: "DRILL-WANDER", station: "drill", defect_count: 27, escaped_count: 2 },
    { id: 3, week_start: demoWeekStart(-1), defect_code: "SOLDER-MASK-SHORT", station: "mask", defect_count: 21, escaped_count: 1 },
  ],
  source
);

seed.insert(
  "process_capability",
  [
    { id: 1, process_step: "laser drill", metric: "microvia diameter", cpk: 1.18, sample_count: 96, status: "watch" },
    { id: 2, process_step: "electroless copper", metric: "plating thickness", cpk: 0.92, sample_count: 128, status: "at_risk" },
    { id: 3, process_step: "solder mask", metric: "registration", cpk: 1.41, sample_count: 120, status: "healthy" },
  ],
  source
);

seed.insert(
  "wip_aging",
  [
    { id: 1, order_code: "FO-260701-ALPHA", current_step: "CAM", age_hours: 18.5, blocker: "DFM signoff" },
    { id: 2, order_code: "FO-260702-BETA", current_step: "plating", age_hours: 11.0, blocker: "void containment" },
    { id: 3, order_code: "FO-260703-GAMMA", current_step: "kit", age_hours: 7.25, blocker: "awaiting stencil" },
  ],
  source
);
