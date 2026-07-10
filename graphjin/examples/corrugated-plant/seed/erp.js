const seedOptions = { source: "erp", user_id: "demo-seed", role: "warehouse_manager" };

function insert(query, variables) {
  return graphql(query, variables, seedOptions);
}

// Dates are computed relative to the seed run (UTC) so demo questions like
// "what runs today?" keep returning data as the state ages. Day 0 is the
// demo day: corrugator runs are planned today and tomorrow, orders come
// due over the next week, and recent downtime needs follow-up.
const DAY_MS = 86400000;
const seedNowMs = Date.now();

function demoDay(offsetDays) {
  return new Date(seedNowMs + offsetDays * DAY_MS).toISOString().slice(0, 10);
}

function demoStamp(offsetDays, hhmmss) {
  return demoDay(offsetDays) + " " + hhmmss;
}

insert(
  `mutation {
    customers(insert: $customers) { id }
  }`,
  {
    customers: [
      { id: 1, account_id: 1, name: "Cedar Bend Produce", segment: "food", ship_city: "Salem", priority_tier: "strategic" },
      { id: 2, account_id: 1, name: "Northline Appliance", segment: "durable goods", ship_city: "Tacoma", priority_tier: "standard" },
      { id: 3, account_id: 1, name: "Riverwest Nursery", segment: "horticulture", ship_city: "Eugene", priority_tier: "priority" },
      { id: 4, account_id: 1, name: "MetroMove Logistics", segment: "3pl", ship_city: "Portland", priority_tier: "standard" },
      { id: 5, account_id: 1, name: "Summit Foods", segment: "food", ship_city: "Boise", priority_tier: "priority" },
    ],
  }
);

insert(
  `mutation {
    board_grades(insert: $board_grades) { id }
  }`,
  {
    board_grades: [
      { id: 1, grade_code: "B32-K", flute: "B", liner_gsm: 175, medium_gsm: 125, target_strength_ect: 32.0 },
      { id: 2, grade_code: "C44-H", flute: "C", liner_gsm: 205, medium_gsm: 150, target_strength_ect: 44.0 },
      { id: 3, grade_code: "E26-R", flute: "E", liner_gsm: 150, medium_gsm: 112, target_strength_ect: 26.0 },
    ],
  }
);

insert(
  `mutation {
    paper_rolls(insert: $paper_rolls) { id }
  }`,
  {
    paper_rolls: [
      { id: 1, account_id: 1, roll_code: "LNR-175-001", paper_type: "kraft liner", gsm: 175, remaining_kg: 980.5, reorder_point_kg: 1500.0, supplier: "Cascade Paper", status: "available" },
      { id: 2, account_id: 1, roll_code: "MED-125-018", paper_type: "recycled medium", gsm: 125, remaining_kg: 740.0, reorder_point_kg: 1200.0, supplier: "GreenFiber", status: "available" },
      { id: 3, account_id: 1, roll_code: "LNR-205-041", paper_type: "heavy kraft liner", gsm: 205, remaining_kg: 4200.0, reorder_point_kg: 2200.0, supplier: "Cascade Paper", status: "available" },
      { id: 4, account_id: 1, roll_code: "MED-150-009", paper_type: "semi-chemical medium", gsm: 150, remaining_kg: 3600.0, reorder_point_kg: 1800.0, supplier: "Pacific Pulp", status: "available" },
      { id: 5, account_id: 1, roll_code: "LNR-150-027", paper_type: "light kraft liner", gsm: 150, remaining_kg: 1150.0, reorder_point_kg: 1000.0, supplier: "Northwest Paper", status: "available" },
    ],
  }
);

insert(
  `mutation {
    corrugator_machines(insert: $corrugator_machines) { id }
  }`,
  {
    corrugator_machines: [
      { id: 1, machine_code: "CORR-1", name: "2.5m Corrugator", max_m_per_min: 220, status: "ready" },
      { id: 2, machine_code: "CORR-2", name: "Mini Corrugator", max_m_per_min: 140, status: "ready" },
    ],
  }
);

insert(
  `mutation {
    work_orders(insert: $work_orders) { id }
  }`,
  {
    work_orders: [
      { id: 1, account_id: 1, customer_id: 1, board_grade_id: 1, order_code: "WO-260701-CEDAR", due_date: demoDay(1), quantity_m2: 18500.0, status: "queued", priority: 1 },
      { id: 2, account_id: 1, customer_id: 3, board_grade_id: 3, order_code: "WO-260702-RIVER", due_date: demoDay(2), quantity_m2: 9200.0, status: "queued", priority: 2 },
      { id: 3, account_id: 1, customer_id: 2, board_grade_id: 2, order_code: "WO-260703-NORTH", due_date: demoDay(5), quantity_m2: 26400.0, status: "planned", priority: 3 },
      { id: 4, account_id: 1, customer_id: 5, board_grade_id: 1, order_code: "WO-260704-SUMMIT", due_date: demoDay(4), quantity_m2: 13250.0, status: "at_risk", priority: 2 },
      { id: 5, account_id: 1, customer_id: 4, board_grade_id: 2, order_code: "WO-260705-METRO", due_date: demoDay(8), quantity_m2: 21400.0, status: "queued", priority: 4 },
    ],
  }
);

insert(
  `mutation {
    corrugator_runs(insert: $corrugator_runs) { id }
  }`,
  {
    corrugator_runs: [
      { id: 1, work_order_id: 1, machine_id: 1, scheduled_start: demoStamp(0, "08:00:00"), planned_minutes: 115, actual_minutes: null, status: "planned", operator: "Mina Patel" },
      { id: 2, work_order_id: 2, machine_id: 2, scheduled_start: demoStamp(0, "10:30:00"), planned_minutes: 78, actual_minutes: null, status: "planned", operator: "Theo Ramirez" },
      { id: 3, work_order_id: 3, machine_id: 1, scheduled_start: demoStamp(0, "13:00:00"), planned_minutes: 150, actual_minutes: null, status: "planned", operator: "Mina Patel" },
      { id: 4, work_order_id: 4, machine_id: 1, scheduled_start: demoStamp(1, "08:00:00"), planned_minutes: 96, actual_minutes: null, status: "planned", operator: "Ari Kim" },
    ],
  }
);

insert(
  `mutation {
    converting_jobs(insert: $converting_jobs) { id }
  }`,
  {
    converting_jobs: [
      { id: 1, work_order_id: 1, job_code: "CV-260701-A", station: "die-cut", status: "queued", planned_minutes: 80 },
      { id: 2, work_order_id: 1, job_code: "CV-260701-B", station: "folder-gluer", status: "queued", planned_minutes: 65 },
      { id: 3, work_order_id: 2, job_code: "CV-260702-A", station: "printer", status: "queued", planned_minutes: 55 },
      { id: 4, work_order_id: 4, job_code: "CV-260704-A", station: "die-cut", status: "hold", planned_minutes: 70 },
    ],
  }
);

insert(
  `mutation {
    shipments(insert: $shipments) { id }
  }`,
  {
    shipments: [
      { id: 1, account_id: 1, work_order_id: 1, ship_date: demoDay(1), carrier: "NW Freight", status: "pending" },
      { id: 2, account_id: 1, work_order_id: 2, ship_date: demoDay(2), carrier: "Valley Express", status: "pending" },
      { id: 3, account_id: 1, work_order_id: 3, ship_date: demoDay(6), carrier: "NW Freight", status: "pending" },
      { id: 4, account_id: 1, work_order_id: 4, ship_date: demoDay(4), carrier: "ColdChain Local", status: "pending" },
    ],
  }
);

insert(
  `mutation {
    downtime_events(insert: $downtime_events) { id }
  }`,
  {
    downtime_events: [
      { id: 1, machine_id: 1, occurred_at: demoStamp(-1, "15:30:00"), minutes_down: 42, reason: "splicer jam", severity: "high", status: "open" },
      { id: 2, machine_id: 2, occurred_at: demoStamp(-2, "11:10:00"), minutes_down: 18, reason: "heater reset", severity: "normal", status: "closed" },
      { id: 3, machine_id: 1, occurred_at: demoStamp(-3, "09:45:00"), minutes_down: 26, reason: "roll stand alignment", severity: "medium", status: "open" },
    ],
  }
);

insert(
  `mutation {
    quality_holds(insert: $quality_holds) { id }
  }`,
  {
    quality_holds: [
      { id: 1, account_id: 1, work_order_id: 4, created_at: demoStamp(-1, "17:20:00"), defect_type: "warp above spec", severity: "high", status: "open" },
      { id: 2, account_id: 1, work_order_id: 3, created_at: demoStamp(-2, "13:15:00"), defect_type: "print registration", severity: "medium", status: "released" },
    ],
  }
);
