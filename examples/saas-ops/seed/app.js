const seedOptions = { source: "app", user_id: "demo-seed", role: "user" };

function insert(query, variables) {
  return graphql(query, variables, seedOptions);
}

// Dates are computed relative to the seed run (UTC) so demo questions like
// "which accounts are at churn risk?" keep returning data as the state ages.
// Day 0 is the demo day: Meridian Robotics is the churn-risk anchor (failed
// payments, breached SLA, usage collapse, renewal in 9 days); Solvex is the
// healthy top account. Active MRR sums to $19,800; churn-risk exposure is
// $5,200 (Meridian $4,800 + Quartzline $400); enterprise is ~81% of MRR.
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
    accounts(insert: $accounts) { id }
  }`,
  {
    accounts: [
      {
        id: 1,
        name: "Meridian Robotics",
        plan: "enterprise",
        status: "churn_risk",
        mrr_cents: 480000,
        renewal_date: demoDay(9),
        last_active_at: demoTime(-6, "16:00"),
      },
      {
        id: 2,
        name: "Solvex Dynamics",
        plan: "enterprise",
        status: "active",
        mrr_cents: 610000,
        renewal_date: demoDay(140),
        last_active_at: demoTime(0, "08:30"),
      },
      {
        id: 3,
        name: "Harborlight Systems",
        plan: "enterprise",
        status: "active",
        mrr_cents: 520000,
        renewal_date: demoDay(200),
        last_active_at: demoTime(-1, "11:00"),
      },
      {
        id: 4,
        name: "Quartzline Analytics",
        plan: "starter",
        status: "churn_risk",
        mrr_cents: 40000,
        renewal_date: demoDay(21),
        last_active_at: demoTime(-9, "10:00"),
      },
      {
        id: 5,
        name: "Ironvale Labs",
        plan: "growth",
        status: "active",
        mrr_cents: 160000,
        renewal_date: demoDay(90),
        last_active_at: demoTime(0, "09:15"),
      },
      {
        id: 6,
        name: "Copperbeam Software",
        plan: "growth",
        status: "active",
        mrr_cents: 140000,
        renewal_date: demoDay(75),
        last_active_at: demoTime(-2, "14:20"),
      },
      {
        id: 7,
        name: "Driftline Media",
        plan: "starter",
        status: "active",
        mrr_cents: 30000,
        renewal_date: demoDay(50),
        last_active_at: demoTime(-3, "12:00"),
      },
      {
        id: 8,
        name: "Tidegate Press",
        plan: "starter",
        status: "cancelled",
        mrr_cents: 0,
        renewal_date: demoDay(-30),
        last_active_at: demoTime(-45, "09:00"),
      },
    ],
  }
);

insert(
  `mutation {
    users(insert: $users) { id }
  }`,
  {
    users: [
      {
        id: 1,
        account_id: 1,
        name: "Dana Whitmore",
        email: "dana@meridianrobotics.example.com",
        role: "owner",
        last_login_at: demoTime(-6, "16:00"),
      },
      {
        id: 2,
        account_id: 1,
        name: "Leo Marchetti",
        email: "leo@meridianrobotics.example.com",
        role: "member",
        last_login_at: demoTime(-8, "13:30"),
      },
      {
        id: 3,
        account_id: 2,
        name: "Priya Raman",
        email: "priya@solvex.example.com",
        role: "owner",
        last_login_at: demoTime(0, "08:30"),
      },
      {
        id: 4,
        account_id: 2,
        name: "Owen Castillo",
        email: "owen@solvex.example.com",
        role: "member",
        last_login_at: demoTime(-1, "17:45"),
      },
      {
        id: 5,
        account_id: 3,
        name: "Freja Lindqvist",
        email: "freja@harborlight.example.com",
        role: "owner",
        last_login_at: demoTime(-1, "11:00"),
      },
      {
        id: 6,
        account_id: 4,
        name: "Sam Becker",
        email: "sam@quartzline.example.com",
        role: "owner",
        last_login_at: demoTime(-9, "10:00"),
      },
      {
        id: 7,
        account_id: 5,
        name: "Yuki Tanaka",
        email: "yuki@ironvale.example.com",
        role: "owner",
        last_login_at: demoTime(0, "09:15"),
      },
      {
        id: 8,
        account_id: 6,
        name: "Marcus Reid",
        email: "marcus@copperbeam.example.com",
        role: "owner",
        last_login_at: demoTime(-2, "14:20"),
      },
      {
        id: 9,
        account_id: 7,
        name: "Ana Petrova",
        email: "ana@driftline.example.com",
        role: "owner",
        last_login_at: demoTime(-3, "12:00"),
      },
      {
        id: 10,
        account_id: 8,
        name: "Cole Bannister",
        email: "cole@tidegate.example.com",
        role: "owner",
        last_login_at: demoTime(-45, "09:00"),
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
        account_id: 1,
        plan: "enterprise",
        seats: 45,
        mrr_cents: 480000,
        status: "past_due",
        started_at: demoTime(-290, "10:00"),
        renews_at: demoDay(9),
      },
      {
        id: 2,
        account_id: 2,
        plan: "enterprise",
        seats: 60,
        mrr_cents: 610000,
        status: "active",
        started_at: demoTime(-410, "10:00"),
        renews_at: demoDay(140),
      },
      {
        id: 3,
        account_id: 3,
        plan: "enterprise",
        seats: 50,
        mrr_cents: 520000,
        status: "active",
        started_at: demoTime(-350, "10:00"),
        renews_at: demoDay(200),
      },
      {
        id: 4,
        account_id: 4,
        plan: "starter",
        seats: 4,
        mrr_cents: 40000,
        status: "past_due",
        started_at: demoTime(-160, "10:00"),
        renews_at: demoDay(21),
      },
      {
        id: 5,
        account_id: 5,
        plan: "growth",
        seats: 16,
        mrr_cents: 160000,
        status: "active",
        started_at: demoTime(-200, "10:00"),
        renews_at: demoDay(90),
      },
      {
        id: 6,
        account_id: 6,
        plan: "growth",
        seats: 14,
        mrr_cents: 140000,
        status: "active",
        started_at: demoTime(-230, "10:00"),
        renews_at: demoDay(75),
      },
      {
        id: 7,
        account_id: 7,
        plan: "starter",
        seats: 3,
        mrr_cents: 30000,
        status: "active",
        started_at: demoTime(-120, "10:00"),
        renews_at: demoDay(50),
      },
      {
        id: 8,
        account_id: 8,
        plan: "starter",
        seats: 3,
        mrr_cents: 0,
        status: "cancelled",
        started_at: demoTime(-400, "10:00"),
        renews_at: demoDay(-30),
      },
    ],
  }
);

insert(
  `mutation {
    invoices(insert: $invoices) { id }
  }`,
  {
    invoices: [
      {
        id: 1,
        account_id: 2,
        subscription_id: 2,
        amount_cents: 610000,
        status: "paid",
        attempts: 1,
        due_at: demoDay(-12),
        last_attempt_at: demoTime(-12, "03:05"),
      },
      {
        id: 2,
        account_id: 3,
        subscription_id: 3,
        amount_cents: 520000,
        status: "paid",
        attempts: 1,
        due_at: demoDay(-10),
        last_attempt_at: demoTime(-10, "03:05"),
      },
      {
        id: 3,
        account_id: 5,
        subscription_id: 5,
        amount_cents: 160000,
        status: "paid",
        attempts: 1,
        due_at: demoDay(-8),
        last_attempt_at: demoTime(-8, "03:05"),
      },
      {
        id: 4,
        account_id: 6,
        subscription_id: 6,
        amount_cents: 140000,
        status: "paid",
        attempts: 1,
        due_at: demoDay(-6),
        last_attempt_at: demoTime(-6, "03:05"),
      },
      {
        id: 5,
        account_id: 7,
        subscription_id: 7,
        amount_cents: 30000,
        status: "paid",
        attempts: 1,
        due_at: demoDay(-5),
        last_attempt_at: demoTime(-5, "03:05"),
      },
      {
        id: 6,
        account_id: 1,
        subscription_id: 1,
        amount_cents: 480000,
        status: "paid",
        attempts: 1,
        due_at: demoDay(-67),
        last_attempt_at: demoTime(-67, "03:05"),
      },
      {
        id: 7,
        account_id: 4,
        subscription_id: 4,
        amount_cents: 40000,
        status: "paid",
        attempts: 1,
        due_at: demoDay(-40),
        last_attempt_at: demoTime(-40, "03:05"),
      },
      {
        id: 8,
        account_id: 1,
        subscription_id: 1,
        amount_cents: 480000,
        status: "failed",
        attempts: 3,
        due_at: demoDay(-37),
        last_attempt_at: demoTime(-9, "03:10"),
      },
      {
        id: 9,
        account_id: 1,
        subscription_id: 1,
        amount_cents: 480000,
        status: "failed",
        attempts: 2,
        due_at: demoDay(-7),
        last_attempt_at: demoTime(-2, "03:10"),
      },
      {
        id: 10,
        account_id: 4,
        subscription_id: 4,
        amount_cents: 40000,
        status: "failed",
        attempts: 3,
        due_at: demoDay(-10),
        last_attempt_at: demoTime(-3, "03:15"),
      },
    ],
  }
);

insert(
  `mutation {
    support_tickets(insert: $support_tickets) { id }
  }`,
  {
    support_tickets: [
      {
        id: 1,
        account_id: 1,
        user_id: 1,
        subject: "Production API returning 500s after upgrade",
        severity: "urgent",
        status: "open",
        opened_at: demoTime(-2, "09:00"),
        sla_due_at: demoTime(-1, "09:00"),
      },
      {
        id: 2,
        account_id: 3,
        user_id: 5,
        subject: "Webhook deliveries delayed by several minutes",
        severity: "high",
        status: "open",
        opened_at: demoTime(0, "08:00"),
        sla_due_at: demoTime(0, "17:00"),
      },
      {
        id: 3,
        account_id: 2,
        user_id: 3,
        subject: "Question about seat-based billing proration",
        severity: "normal",
        status: "open",
        opened_at: demoTime(-1, "10:00"),
        sla_due_at: demoTime(2, "10:00"),
      },
      {
        id: 4,
        account_id: 1,
        user_id: 2,
        subject: "Feature request: SSO group mapping",
        severity: "normal",
        status: "pending",
        opened_at: demoTime(-5, "15:00"),
        sla_due_at: demoTime(1, "15:00"),
      },
      {
        id: 5,
        account_id: 4,
        user_id: 6,
        subject: "Cannot update payment method from dashboard",
        severity: "high",
        status: "open",
        opened_at: demoTime(-3, "15:00"),
        sla_due_at: demoTime(1, "15:00"),
      },
      {
        id: 6,
        account_id: 5,
        user_id: 7,
        subject: "Export job stuck at 99 percent",
        severity: "normal",
        status: "resolved",
        opened_at: demoTime(-14, "09:30"),
        sla_due_at: demoTime(-11, "09:30"),
      },
      {
        id: 7,
        account_id: 6,
        user_id: 8,
        subject: "API key rotation docs unclear",
        severity: "low",
        status: "resolved",
        opened_at: demoTime(-12, "13:00"),
        sla_due_at: demoTime(-7, "13:00"),
      },
      {
        id: 8,
        account_id: 2,
        user_id: 4,
        subject: "Rate limit headers missing on burst traffic",
        severity: "high",
        status: "resolved",
        opened_at: demoTime(-9, "11:20"),
        sla_due_at: demoTime(-8, "11:20"),
      },
      {
        id: 9,
        account_id: 7,
        user_id: 9,
        subject: "Invite email landed in spam",
        severity: "low",
        status: "resolved",
        opened_at: demoTime(-20, "10:00"),
        sla_due_at: demoTime(-15, "10:00"),
      },
      {
        id: 10,
        account_id: 1,
        user_id: 1,
        subject: "Latency spikes on the events endpoint",
        severity: "high",
        status: "resolved",
        opened_at: demoTime(-25, "14:00"),
        sla_due_at: demoTime(-24, "14:00"),
      },
    ],
  }
);

insert(
  `mutation {
    usage_events(insert: $usage_events) { id }
  }`,
  {
    usage_events: [
      {
        id: 1,
        account_id: 1,
        kind: "api_call",
        quantity: 52000,
        occurred_at: demoTime(-24, "12:00"),
      },
      {
        id: 2,
        account_id: 1,
        kind: "api_call",
        quantity: 44000,
        occurred_at: demoTime(-17, "12:00"),
      },
      {
        id: 3,
        account_id: 1,
        kind: "api_call",
        quantity: 21000,
        occurred_at: demoTime(-10, "12:00"),
      },
      {
        id: 4,
        account_id: 1,
        kind: "api_call",
        quantity: 6500,
        occurred_at: demoTime(-3, "12:00"),
      },
      {
        id: 5,
        account_id: 1,
        kind: "login",
        quantity: 2,
        occurred_at: demoTime(-6, "16:00"),
      },
      {
        id: 6,
        account_id: 2,
        kind: "api_call",
        quantity: 20000,
        occurred_at: demoTime(-14, "12:00"),
      },
      {
        id: 7,
        account_id: 2,
        kind: "api_call",
        quantity: 26000,
        occurred_at: demoTime(-7, "12:00"),
      },
      {
        id: 8,
        account_id: 2,
        kind: "api_call",
        quantity: 31000,
        occurred_at: demoTime(-1, "12:00"),
      },
      {
        id: 9,
        account_id: 2,
        kind: "seat_added",
        quantity: 4,
        occurred_at: demoTime(-3, "10:00"),
      },
      {
        id: 10,
        account_id: 3,
        kind: "api_call",
        quantity: 18000,
        occurred_at: demoTime(-8, "12:00"),
      },
      {
        id: 11,
        account_id: 3,
        kind: "api_call",
        quantity: 19000,
        occurred_at: demoTime(-1, "12:00"),
      },
      {
        id: 12,
        account_id: 4,
        kind: "api_call",
        quantity: 1200,
        occurred_at: demoTime(-30, "12:00"),
      },
      {
        id: 13,
        account_id: 4,
        kind: "api_call",
        quantity: 150,
        occurred_at: demoTime(-9, "12:00"),
      },
      {
        id: 14,
        account_id: 5,
        kind: "api_call",
        quantity: 7400,
        occurred_at: demoTime(-2, "12:00"),
      },
      {
        id: 15,
        account_id: 6,
        kind: "export",
        quantity: 12,
        occurred_at: demoTime(-4, "12:00"),
      },
      {
        id: 16,
        account_id: 7,
        kind: "api_call",
        quantity: 900,
        occurred_at: demoTime(-5, "12:00"),
      },
    ],
  }
);
