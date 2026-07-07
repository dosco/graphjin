const source = { source: "test_measurements" };

seed.insert(
  "test_runs",
  [
    { id: 1, order_id: 1, station: "ICT-1", run_code: "TR-ALPHA-ICT-1", status: "fail", started_at: "2026-07-06T18:30:00Z", completed_at: "2026-07-06T18:42:00Z" },
    { id: 2, order_id: 2, station: "AOI-2", run_code: "TR-BETA-AOI-4", status: "pass", started_at: "2026-07-06T20:05:00Z", completed_at: "2026-07-06T20:21:00Z" },
  ],
  source
);

seed.insert(
  "test_measurements",
  [
    { id: 1, run_id: 1, measurement: "impedance_lane_3", value: 46.8, unit: "ohm", lower_limit: 47.0, upper_limit: 53.0, status: "fail" },
    { id: 2, run_id: 1, measurement: "microvia_resistance", value: 91.2, unit: "milliohm", lower_limit: 0.0, upper_limit: 80.0, status: "fail" },
    { id: 3, run_id: 2, measurement: "aoi_defects", value: 0.0, unit: "count", lower_limit: 0.0, upper_limit: 0.0, status: "pass" },
  ],
  source
);

seed.insert(
  "probe_calibrations",
  [
    { id: 1, station: "ICT-1", calibrated_at: "2026-07-01T12:00:00Z", next_due_at: "2026-07-08T12:00:00Z", status: "due_soon" },
    { id: 2, station: "AOI-2", calibrated_at: "2026-07-02T09:30:00Z", next_due_at: "2026-07-16T09:30:00Z", status: "valid" },
  ],
  source
);
