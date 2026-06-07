const seedOptions = { source: "ops", user_id: "demo-seed", role: "user" };

function insert(query, variables) {
  return graphql(query, variables, seedOptions);
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
        created_at: "2026-05-01T09:00:00Z",
      },
      {
        id: 2,
        name: "Harbor Cafe Group",
        email: "ops@harborcafe.example",
        segment: "cafe",
        account_tier: "growth",
        preferred_origin: "Ethiopia",
        created_at: "2026-05-04T10:30:00Z",
      },
      {
        id: 3,
        name: "Dawn Patrol Coffee Club",
        email: "hello@dawnpatrol.example",
        segment: "subscription",
        account_tier: "standard",
        preferred_origin: "Colombia",
        created_at: "2026-05-10T08:15:00Z",
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
        lot_code: "GUA-HUE-2026-04",
        origin: "Guatemala Huehuetenango",
        producer: "Finca Los Pinos",
        process: "washed",
        arrival_date: "2026-04-12",
        remaining_kg: 1180.0,
        target_cost_per_kg: 6.4,
        cupping_score: 86.5,
      },
      {
        id: 2,
        lot_code: "ETH-YIR-2026-03",
        origin: "Ethiopia Yirgacheffe",
        producer: "Konga Cooperative",
        process: "natural",
        arrival_date: "2026-03-30",
        remaining_kg: 720.0,
        target_cost_per_kg: 7.9,
        cupping_score: 88.25,
      },
      {
        id: 3,
        lot_code: "COL-HUI-2026-05",
        origin: "Colombia Huila",
        producer: "Familia Ramos",
        process: "washed",
        arrival_date: "2026-05-15",
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
        scheduled_for: "2026-06-06T15:00:00Z",
        machine_id: "loring-s35",
        target_output_kg: 180.0,
        status: "planned",
        operator: "Maya",
      },
      {
        id: 2,
        profile_id: 2,
        scheduled_for: "2026-06-06T18:00:00Z",
        machine_id: "probats-p25",
        target_output_kg: 96.0,
        status: "planned",
        operator: "Theo",
      },
      {
        id: 3,
        profile_id: 3,
        scheduled_for: "2026-06-07T16:00:00Z",
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
        requested_ship_date: "2026-06-08",
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
        requested_ship_date: "2026-06-09",
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
        requested_ship_date: "2026-06-10",
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
        next_ship_date: "2026-06-10",
        bags_per_shipment: 2,
        active: true,
      },
      {
        id: 2,
        customer_id: 1,
        product_name: "Northstar House Blend 340g",
        cadence_days: 7,
        next_ship_date: "2026-06-08",
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
        created_at: "2026-06-05T17:20:00Z",
      },
      {
        id: 2,
        customer_id: 3,
        production_order_id: 3,
        subject: "Subscription note",
        body: "Customer asked for a brighter cup if the Colombia roast allows it.",
        severity: "normal",
        status: "open",
        created_at: "2026-06-05T18:05:00Z",
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
        created_at: "2026-06-04T12:00:00Z",
      },
    ],
  }
);
