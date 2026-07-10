const seedOptions = { source: "ops", user_id: "demo-seed", role: "user" };

function insert(query, variables) {
  return graphql(query, variables, seedOptions);
}

// All dates are computed relative to the seed run (UTC) so demo questions
// like "what should we roast today?" keep returning data as the state ages.
// Day 0 is the demo day: yesterday's batches were roasted, today's roasts
// are planned, and shipments are due over the next few days.
const DAY_MS = 86400000;
const seedNowMs = Date.now();

function demoDay(offsetDays) {
  return new Date(seedNowMs + offsetDays * DAY_MS).toISOString().slice(0, 10);
}

function demoTime(offsetDays, hhmm) {
  return demoDay(offsetDays) + "T" + hhmm + ":00Z";
}

insert(
  `mutation {
    customers(insert: $customers) { id }
  }`,
  {
    customers: [
      {
        id: 1,
        name: "Northstar Grocers",
        email: "buyers@northstar.example",
        segment: "grocery",
        account_tier: "enterprise",
        preferred_origin: "Guatemala",
        created_at: demoTime(-36, "09:00"),
      },
      {
        id: 2,
        name: "Harbor Cafe Group",
        email: "ops@harborcafe.example",
        segment: "cafe",
        account_tier: "growth",
        preferred_origin: "Ethiopia",
        created_at: demoTime(-33, "10:30"),
      },
      {
        id: 3,
        name: "Dawn Patrol Coffee Club",
        email: "hello@dawnpatrol.example",
        segment: "subscription",
        account_tier: "standard",
        preferred_origin: "Colombia",
        created_at: demoTime(-27, "08:15"),
      },
    ],
  }
);

insert(
  `mutation {
    green_lots(insert: $green_lots) { id }
  }`,
  {
    green_lots: [
      {
        id: 1,
        lot_code: "GUA-HUE-LOT-04",
        origin: "Guatemala Huehuetenango",
        producer: "Finca Los Pinos",
        process: "washed",
        arrival_date: demoDay(-55),
        remaining_kg: 1180.0,
        target_cost_per_kg: 6.4,
        cupping_score: 86.5,
      },
      {
        id: 2,
        lot_code: "ETH-YIR-LOT-03",
        origin: "Ethiopia Yirgacheffe",
        producer: "Konga Cooperative",
        process: "natural",
        arrival_date: demoDay(-68),
        remaining_kg: 720.0,
        target_cost_per_kg: 7.9,
        cupping_score: 88.25,
      },
      {
        id: 3,
        lot_code: "COL-HUI-LOT-05",
        origin: "Colombia Huila",
        producer: "Familia Ramos",
        process: "washed",
        arrival_date: demoDay(-22),
        remaining_kg: 940.0,
        target_cost_per_kg: 6.85,
        cupping_score: 87.1,
      },
    ],
  }
);

insert(
  `mutation {
    roast_profiles(insert: $roast_profiles) { id }
  }`,
  {
    roast_profiles: [
      {
        id: 1,
        profile_name: "Northstar house medium",
        green_lot_id: 1,
        charge_temp_c: 202.0,
        development_target_seconds: 105,
        drop_temp_c: 214.5,
        notes: "Chocolate, almond, citrus finish",
      },
      {
        id: 2,
        profile_name: "Harbor fruit-forward espresso",
        green_lot_id: 2,
        charge_temp_c: 198.0,
        development_target_seconds: 92,
        drop_temp_c: 211.0,
        notes: "Keep rate-of-rise soft after first crack",
      },
      {
        id: 3,
        profile_name: "Dawn Patrol filter",
        green_lot_id: 3,
        charge_temp_c: 200.0,
        development_target_seconds: 98,
        drop_temp_c: 212.25,
        notes: "Clean sweetness, avoid baked finish",
      },
    ],
  }
);

insert(
  `mutation {
    roast_schedule(insert: $roast_schedule) { id }
  }`,
  {
    roast_schedule: [
      {
        id: 1,
        profile_id: 1,
        scheduled_for: demoTime(0, "15:00"),
        machine_id: "loring-s35",
        target_output_kg: 180.0,
        status: "planned",
        operator: "Maya",
      },
      {
        id: 2,
        profile_id: 2,
        scheduled_for: demoTime(0, "18:00"),
        machine_id: "probats-p25",
        target_output_kg: 96.0,
        status: "planned",
        operator: "Theo",
      },
      {
        id: 3,
        profile_id: 3,
        scheduled_for: demoTime(1, "16:00"),
        machine_id: "loring-s35",
        target_output_kg: 120.0,
        status: "planned",
        operator: "Maya",
      },
    ],
  }
);

insert(
  `mutation {
    production_orders(insert: $production_orders) { id }
  }`,
  {
    production_orders: [
      {
        id: 1,
        customer_id: 1,
        roast_schedule_id: 1,
        product_name: "Northstar House Blend 340g",
        quantity_bags: 420,
        bag_size_g: 340,
        requested_ship_date: demoDay(2),
        status: "queued",
        priority: 1,
      },
      {
        id: 2,
        customer_id: 2,
        roast_schedule_id: 2,
        product_name: "Harbor Espresso 1kg",
        quantity_bags: 80,
        bag_size_g: 1000,
        requested_ship_date: demoDay(3),
        status: "queued",
        priority: 2,
      },
      {
        id: 3,
        customer_id: 3,
        roast_schedule_id: 3,
        product_name: "Dawn Patrol Single Origin 250g",
        quantity_bags: 160,
        bag_size_g: 250,
        requested_ship_date: demoDay(4),
        status: "queued",
        priority: 3,
      },
    ],
  }
);

insert(
  `mutation {
    subscriptions(insert: $subscriptions) { id }
  }`,
  {
    subscriptions: [
      {
        id: 1,
        customer_id: 3,
        product_name: "Dawn Patrol Single Origin 250g",
        cadence_days: 14,
        next_ship_date: demoDay(4),
        bags_per_shipment: 2,
        active: true,
      },
      {
        id: 2,
        customer_id: 1,
        product_name: "Northstar House Blend 340g",
        cadence_days: 7,
        next_ship_date: demoDay(2),
        bags_per_shipment: 120,
        active: true,
      },
    ],
  }
);

insert(
  `mutation {
    customer_tickets(insert: $customer_tickets) { id }
  }`,
  {
    customer_tickets: [
      {
        id: 1,
        customer_id: 2,
        production_order_id: 2,
        subject: "Espresso running faster than target",
        body: "Last batch extracted in 21 seconds on the same grinder setting. Please check whether the next roast should develop longer.",
        severity: "high",
        status: "open",
        created_at: demoTime(-1, "17:20"),
      },
      {
        id: 2,
        customer_id: 3,
        production_order_id: 3,
        subject: "Subscription note",
        body: "Customer asked for a brighter cup if the Colombia roast allows it.",
        severity: "normal",
        status: "open",
        created_at: demoTime(-1, "18:05"),
      },
    ],
  }
);

insert(
  `mutation {
    inventory_adjustments(insert: $inventory_adjustments) { id }
  }`,
  {
    inventory_adjustments: [
      {
        id: 1,
        green_lot_id: 2,
        adjustment_kg: -12.0,
        reason: "sample roasting and QC calibration",
        created_at: demoTime(-2, "12:00"),
      },
    ],
  }
);
