# GraphJin DDL for the writable clinic scheduling source (SQLite, in-process).
# Applied by `graphjin serve --demo --path examples/clinic-scheduler`.

type patients {
  id: Bigint! @id
  name: Text!
  phone: Text!
  email: Text! @unique
  account_id: Bigint!
}

type providers {
  id: Bigint! @id
  name: Text!
  specialty: Text!
}

type rooms {
  id: Bigint! @id
  name: Text!
  kind: Text!
}

type appointments {
  id: Bigint! @id
  patient_id: Bigint! @relation(type: patients, field: id)
  provider_id: Bigint! @relation(type: providers, field: id)
  room_id: Bigint! @relation(type: rooms, field: id)
  scheduled_at: TimestampWithTimeZone!
  duration_minutes: Integer!
  status: Text! @default(value: "'scheduled'")
}

type waitlist_entries {
  id: Bigint! @id
  patient_id: Bigint! @relation(type: patients, field: id)
  provider_id: Bigint! @relation(type: providers, field: id)
  requested_specialty: Text!
  priority: Text!
  status: Text! @default(value: "'waiting'")
  created_at: TimestampWithTimeZone! @default(value: "CURRENT_TIMESTAMP")
}

type no_show_events {
  id: Bigint! @id
  appointment_id: Bigint! @relation(type: appointments, field: id)
  patient_id: Bigint! @relation(type: patients, field: id)
  occurred_at: TimestampWithTimeZone!
  reason: Text!
}
