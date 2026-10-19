const source = { source: "test_measurements" };

// Test-floor timestamps are anchored to the seed run (UTC): last night's
// runs just finished and the ICT probe calibration comes due tomorrow.
const DAY_MS = 86400000;
const seedNowMs = Date.now();

function demoDay(offsetDays) {
  return new Date(seedNowMs + offsetDays * DAY_MS).toISOString().slice(0, 10);
}

function demoTime(offsetDays, hhmm) {
  return demoDay(offsetDays) + "T" + hhmm + ":00Z";
}

seed.insert(
  "test_runs",
  [
    { id: 1, order_id: 1, station: "ICT-1", run_code: "TR-ALPHA-ICT-1", status: "fail", started_at: demoTime(-1, "18:30"), completed_at: demoTime(-1, "18:42") },
    { id: 2, order_id: 2, station: "AOI-2", run_code: "TR-BETA-AOI-4", status: "pass", started_at: demoTime(-1, "20:05"), completed_at: demoTime(-1, "20:21") },
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
    { id: 1, station: "ICT-1", calibrated_at: demoTime(-6, "12:00"), next_due_at: demoTime(1, "12:00"), status: "due_soon" },
    { id: 2, station: "AOI-2", calibrated_at: demoTime(-5, "09:30"), next_due_at: demoTime(9, "09:30"), status: "valid" },
  ],
  source
);
