const seedOptions = { source: "app", user_id: "demo-seed", role: "user" };

function insert(query, variables) {
  return graphql(query, variables, seedOptions);
}

// Dates are computed relative to the seed run (UTC) so demo questions like
// "who is on today's schedule?" keep returning data as the state ages.
// Day 0 is the demo day: past visits completed or missed, today and the
// coming days fully booked, and the waitlist waiting.
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
    patients(insert: $patients) { id }
  }`,
  {
    patients: [
      {
        id: 1,
        name: "Rosa Alvarez",
        phone: "555-0101",
        email: "rosa.alvarez@example.com",
        account_id: 1,
      },
      {
        id: 2,
        name: "Miles Okafor",
        phone: "555-0102",
        email: "miles.okafor@example.com",
        account_id: 1,
      },
      {
        id: 3,
        name: "Priya Natarajan",
        phone: "555-0103",
        email: "priya.natarajan@example.com",
        account_id: 1,
      },
      {
        id: 4,
        name: "Ethan Whitfield",
        phone: "555-0104",
        email: "ethan.whitfield@example.com",
        account_id: 1,
      },
      {
        id: 5,
        name: "Grace Liu",
        phone: "555-0105",
        email: "grace.liu@example.com",
        account_id: 1,
      },
      {
        id: 6,
        name: "Samuel Ortega",
        phone: "555-0106",
        email: "samuel.ortega@example.com",
        account_id: 1,
      },
      {
        id: 7,
        name: "Nadia Haddad",
        phone: "555-0107",
        email: "nadia.haddad@example.com",
        account_id: 1,
      },
      {
        id: 8,
        name: "Tomas Rivera",
        phone: "555-0108",
        email: "tomas.rivera@example.com",
        account_id: 1,
      },
    ],
  }
);

insert(
  `mutation {
    providers(insert: $providers) { id }
  }`,
  {
    providers: [
      { id: 1, name: "Dr. Alice Chen", specialty: "cardiology" },
      { id: 2, name: "Dr. Ben Osei", specialty: "dermatology" },
      { id: 3, name: "Dr. Carmen Diaz", specialty: "pediatrics" },
      { id: 4, name: "Dr. David Klein", specialty: "orthopedics" },
    ],
  }
);

insert(
  `mutation {
    rooms(insert: $rooms) { id }
  }`,
  {
    rooms: [
      { id: 1, name: "Exam 1", kind: "exam" },
      { id: 2, name: "Exam 2", kind: "exam" },
      { id: 3, name: "Procedure A", kind: "procedure" },
    ],
  }
);

insert(
  `mutation {
    appointments(insert: $appointments) { id }
  }`,
  {
    appointments: [
      {
        id: 1,
        patient_id: 1,
        provider_id: 1,
        room_id: 1,
        scheduled_at: demoTime(-5, "09:00"),
        duration_minutes: 30,
        status: "completed",
      },
      {
        id: 2,
        patient_id: 2,
        provider_id: 2,
        room_id: 2,
        scheduled_at: demoTime(-5, "10:00"),
        duration_minutes: 20,
        status: "completed",
      },
      {
        id: 3,
        patient_id: 3,
        provider_id: 3,
        room_id: 1,
        scheduled_at: demoTime(-4, "09:30"),
        duration_minutes: 30,
        status: "completed",
      },
      {
        id: 4,
        patient_id: 4,
        provider_id: 4,
        room_id: 3,
        scheduled_at: demoTime(-4, "11:00"),
        duration_minutes: 45,
        status: "completed",
      },
      {
        id: 5,
        patient_id: 5,
        provider_id: 1,
        room_id: 1,
        scheduled_at: demoTime(-3, "09:00"),
        duration_minutes: 30,
        status: "no_show",
      },
      {
        id: 6,
        patient_id: 6,
        provider_id: 2,
        room_id: 2,
        scheduled_at: demoTime(-3, "10:30"),
        duration_minutes: 20,
        status: "no_show",
      },
      {
        id: 7,
        patient_id: 7,
        provider_id: 3,
        room_id: 1,
        scheduled_at: demoTime(-2, "14:00"),
        duration_minutes: 30,
        status: "no_show",
      },
      {
        id: 8,
        patient_id: 8,
        provider_id: 4,
        room_id: 3,
        scheduled_at: demoTime(-1, "15:00"),
        duration_minutes: 45,
        status: "no_show",
      },
      {
        id: 9,
        patient_id: 1,
        provider_id: 2,
        room_id: 2,
        scheduled_at: demoTime(0, "09:00"),
        duration_minutes: 20,
        status: "scheduled",
      },
      {
        id: 10,
        patient_id: 2,
        provider_id: 1,
        room_id: 1,
        scheduled_at: demoTime(0, "10:00"),
        duration_minutes: 30,
        status: "scheduled",
      },
      {
        id: 11,
        patient_id: 3,
        provider_id: 1,
        room_id: 1,
        scheduled_at: demoTime(1, "09:00"),
        duration_minutes: 30,
        status: "scheduled",
      },
      {
        id: 12,
        patient_id: 4,
        provider_id: 3,
        room_id: 2,
        scheduled_at: demoTime(1, "11:30"),
        duration_minutes: 30,
        status: "scheduled",
      },
      {
        id: 13,
        patient_id: 5,
        provider_id: 4,
        room_id: 3,
        scheduled_at: demoTime(2, "13:00"),
        duration_minutes: 60,
        status: "scheduled",
      },
      {
        id: 14,
        patient_id: 6,
        provider_id: 2,
        room_id: 2,
        scheduled_at: demoTime(3, "10:00"),
        duration_minutes: 20,
        status: "scheduled",
      },
      {
        id: 15,
        patient_id: 7,
        provider_id: 1,
        room_id: 1,
        scheduled_at: demoTime(4, "09:30"),
        duration_minutes: 30,
        status: "scheduled",
      },
    ],
  }
);

insert(
  `mutation {
    waitlist_entries(insert: $waitlist_entries) { id }
  }`,
  {
    waitlist_entries: [
      {
        id: 1,
        patient_id: 1,
        provider_id: 1,
        requested_specialty: "cardiology",
        priority: "high",
        status: "waiting",
        created_at: demoTime(-4, "12:00"),
      },
      {
        id: 2,
        patient_id: 5,
        provider_id: 4,
        requested_specialty: "orthopedics",
        priority: "high",
        status: "waiting",
        created_at: demoTime(-3, "16:00"),
      },
      {
        id: 3,
        patient_id: 3,
        provider_id: 2,
        requested_specialty: "dermatology",
        priority: "medium",
        status: "waiting",
        created_at: demoTime(-2, "09:15"),
      },
      {
        id: 4,
        patient_id: 6,
        provider_id: 3,
        requested_specialty: "pediatrics",
        priority: "low",
        status: "waiting",
        created_at: demoTime(-2, "13:45"),
      },
      {
        id: 5,
        patient_id: 2,
        provider_id: 4,
        requested_specialty: "orthopedics",
        priority: "medium",
        status: "promoted",
        created_at: demoTime(-8, "10:00"),
      },
      {
        id: 6,
        patient_id: 7,
        provider_id: 2,
        requested_specialty: "dermatology",
        priority: "low",
        status: "cancelled",
        created_at: demoTime(-11, "11:30"),
      },
    ],
  }
);

insert(
  `mutation {
    no_show_events(insert: $no_show_events) { id }
  }`,
  {
    no_show_events: [
      {
        id: 1,
        appointment_id: 5,
        patient_id: 5,
        occurred_at: demoTime(-3, "09:20"),
        reason: "no call, no show",
      },
      {
        id: 2,
        appointment_id: 6,
        patient_id: 6,
        occurred_at: demoTime(-3, "10:50"),
        reason: "transport fell through",
      },
      {
        id: 3,
        appointment_id: 7,
        patient_id: 7,
        occurred_at: demoTime(-2, "14:20"),
        reason: "forgot appointment",
      },
      {
        id: 4,
        appointment_id: 8,
        patient_id: 8,
        occurred_at: demoTime(-1, "15:20"),
        reason: "double-booked at work",
      },
    ],
  }
);
