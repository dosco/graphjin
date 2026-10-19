# GraphJin DDL for the writable SaaS ops source (SQLite, in-process).
# Applied by `graphjin serve --demo --path examples/saas-ops`.

type accounts {
  id: Bigint! @id
  name: Text! @unique
  plan: Text!
  status: Text! @default(value: "'active'")
  mrr_cents: Bigint!
  renewal_date: Date!
  last_active_at: TimestampWithTimeZone!
}

type users {
  id: Bigint! @id
  account_id: Bigint! @relation(type: accounts, field: id)
  name: Text!
  email: Text! @unique
  role: Text!
  last_login_at: TimestampWithTimeZone!
}

type subscriptions {
  id: Bigint! @id
  account_id: Bigint! @relation(type: accounts, field: id)
  plan: Text!
  seats: Integer!
  mrr_cents: Bigint!
  status: Text! @default(value: "'active'")
  started_at: TimestampWithTimeZone!
  renews_at: Date!
}

type invoices {
  id: Bigint! @id
  account_id: Bigint! @relation(type: accounts, field: id)
  subscription_id: Bigint! @relation(type: subscriptions, field: id)
  amount_cents: Bigint!
  status: Text!
  attempts: Integer!
  due_at: Date!
  last_attempt_at: TimestampWithTimeZone!
}

type payments {
  id: Bigint! @id
  invoice_id: Bigint! @relation(type: invoices, field: id)
  amount_cents: Bigint!
  reference: Text! @unique
  recorded_at: TimestampWithTimeZone!
}

type support_tickets {
  id: Bigint! @id
  account_id: Bigint! @relation(type: accounts, field: id)
  user_id: Bigint! @relation(type: users, field: id)
  subject: Text!
  severity: Text!
  status: Text! @default(value: "'open'")
  resolution_note: Text
  resolved_at: TimestampWithTimeZone
  opened_at: TimestampWithTimeZone!
  sla_due_at: TimestampWithTimeZone!
}

type usage_events {
  id: Bigint! @id
  account_id: Bigint! @relation(type: accounts, field: id)
  kind: Text!
  quantity: Integer!
  occurred_at: TimestampWithTimeZone!
}
