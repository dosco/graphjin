const seedOptions = { source: "mes", user_id: "demo-seed", role: "fab_planner" };

function insert(query, variables) {
  return graphql(query, variables, seedOptions);
}

insert(
  `mutation {
    customers(insert: $customers) { id }
  }`,
  {
    customers: [
      { id: 1, account_id: 1, name: "Lumen Medical Devices", segment: "medical", priority_tier: "strategic" },
      { id: 2, account_id: 1, name: "Vector Robotics", segment: "industrial", priority_tier: "priority" },
      { id: 3, account_id: 1, name: "Harbor IoT", segment: "consumer", priority_tier: "standard" },
    ],
  }
);

insert(
  `mutation {
    pcb_designs(insert: $pcb_designs) { id }
  }`,
  {
    pcb_designs: [
      { id: 1, account_id: 1, customer_id: 1, design_code: "LM-8CH-CTRL", product_name: "Infusion controller board", layer_count: 8, material: "FR408HR", target_impedance_ohms: 50.0 },
      { id: 2, account_id: 1, customer_id: 2, design_code: "VR-MOTOR-6L", product_name: "Motor drive sensor board", layer_count: 6, material: "FR4 TG170", target_impedance_ohms: 90.0 },
      { id: 3, account_id: 1, customer_id: 3, design_code: "HIOT-GW-4L", product_name: "Gateway radio board", layer_count: 4, material: "FR4 TG150", target_impedance_ohms: 50.0 },
    ],
  }
);

insert(
  `mutation {
    design_revisions(insert: $design_revisions) { id }
  }`,
  {
    design_revisions: [
      { id: 1, design_id: 1, revision: "B", status: "dfm_review", gerber_path: "rev_b/", stackup_note: "8L controlled impedance, HDI microvias on L1-L2/L7-L8", created_at: "2026-07-03 09:15:00" },
      { id: 2, design_id: 2, revision: "A", status: "released", gerber_path: "vr_a/", stackup_note: "6L standard stackup", created_at: "2026-07-02 13:20:00" },
      { id: 3, design_id: 3, revision: "C", status: "released", gerber_path: "hiot_c/", stackup_note: "4L radio keepout revision", created_at: "2026-07-01 10:45:00" },
    ],
  }
);

insert(
  `mutation {
    fab_orders(insert: $fab_orders) { id }
  }`,
  {
    fab_orders: [
      { id: 1, account_id: 1, customer_id: 1, design_id: 1, revision_id: 1, order_code: "FO-260701-ALPHA", due_date: "2026-07-11", panel_qty: 180, status: "engineering_hold", priority: 1 },
      { id: 2, account_id: 1, customer_id: 2, design_id: 2, revision_id: 2, order_code: "FO-260702-BETA", due_date: "2026-07-13", panel_qty: 240, status: "released", priority: 2 },
      { id: 3, account_id: 1, customer_id: 3, design_id: 3, revision_id: 3, order_code: "FO-260703-GAMMA", due_date: "2026-07-18", panel_qty: 320, status: "queued", priority: 4 },
    ],
  }
);

insert(
  `mutation {
    panels(insert: $panels) { id }
  }`,
  {
    panels: [
      { id: 1, account_id: 1, order_id: 1, panel_code: "PNL-ALPHA-001", current_step: "CAM", status: "hold", scrap_flag: false },
      { id: 2, account_id: 1, order_id: 1, panel_code: "PNL-ALPHA-002", current_step: "CAM", status: "hold", scrap_flag: false },
      { id: 3, account_id: 1, order_id: 2, panel_code: "PNL-BETA-014", current_step: "drill", status: "in_process", scrap_flag: false },
      { id: 4, account_id: 1, order_id: 2, panel_code: "PNL-BETA-015", current_step: "plating", status: "in_process", scrap_flag: true },
      { id: 5, account_id: 1, order_id: 3, panel_code: "PNL-GAMMA-003", current_step: "kit", status: "queued", scrap_flag: false },
    ],
  }
);

insert(
  `mutation {
    fab_runs(insert: $fab_runs) { id }
  }`,
  {
    fab_runs: [
      { id: 1, order_id: 1, line: "CAM-1", scheduled_start: "2026-07-07 08:00:00", planned_minutes: 90, status: "planned", engineer: "Mira Chen" },
      { id: 2, order_id: 2, line: "FAB-2", scheduled_start: "2026-07-07 13:30:00", planned_minutes: 210, status: "started", engineer: "Owen Price" },
      { id: 3, order_id: 3, line: "KIT-1", scheduled_start: "2026-07-08 09:30:00", planned_minutes: 70, status: "planned", engineer: "Nadia Singh" },
    ],
  }
);

insert(
  `mutation {
    bom_items(insert: $bom_items) { id }
  }`,
  {
    bom_items: [
      { id: 1, account_id: 1, design_id: 1, sku: "CAP-0402-X7R-100N", description: "100 nF X7R capacitor", qty_per_board: 22.0, criticality: "standard" },
      { id: 2, account_id: 1, design_id: 1, sku: "IC-AFE-8CH-MED", description: "8-channel analog front end", qty_per_board: 1.0, criticality: "critical" },
      { id: 3, account_id: 1, design_id: 1, sku: "CONN-MICRO-40P", description: "40-pin micro connector", qty_per_board: 2.0, criticality: "critical" },
      { id: 4, account_id: 1, design_id: 2, sku: "MOSFET-60V-30A", description: "60V motor FET", qty_per_board: 6.0, criticality: "critical" },
      { id: 5, account_id: 1, design_id: 3, sku: "RF-MOD-2G4", description: "2.4 GHz radio module", qty_per_board: 1.0, criticality: "standard" },
    ],
  }
);

insert(
  `mutation {
    shipments(insert: $shipments) { id }
  }`,
  {
    shipments: [
      { id: 1, account_id: 1, order_id: 1, ship_date: "2026-07-11", carrier: "MedSecure Freight", status: "pending" },
      { id: 2, account_id: 1, order_id: 2, ship_date: "2026-07-13", carrier: "NW Freight", status: "pending" },
      { id: 3, account_id: 1, order_id: 3, ship_date: "2026-07-19", carrier: "Parcel Air", status: "planned" },
    ],
  }
);

insert(
  `mutation {
    ncr_reports(insert: $ncr_reports) { id }
  }`,
  {
    ncr_reports: [
      { id: 1, account_id: 1, order_id: 2, opened_at: "2026-07-06 16:40:00", defect_code: "PLATING-VOID", severity: "high", status: "open" },
      { id: 2, account_id: 1, order_id: 1, opened_at: "2026-07-05 14:05:00", defect_code: "DRILL-WANDER", severity: "medium", status: "contained" },
    ],
  }
);
