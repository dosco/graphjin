-- Dollar budgets on all model spend. Each organization has one limit row;
-- a workflow row overrides only the two workflow budgets. A reservation holds
-- the per-run cap while its run is queued or running. The ledger records every
-- priced turn.

create table if not exists spend_limit (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  workflow_id uuid references workflow_definition(id) on delete cascade,
  run_cap_micros bigint,
  org_hourly_micros bigint,
  org_daily_micros bigint,
  workflow_hourly_micros bigint,
  workflow_daily_micros bigint,
  warn_percent integer,
  updated_by_user_id text references app_user(id) on delete set null,
  updated_at timestamptz not null default now(),
  check (run_cap_micros is null or run_cap_micros > 0),
  check (org_hourly_micros is null or org_hourly_micros > 0),
  check (org_daily_micros is null or org_daily_micros > 0),
  check (workflow_hourly_micros is null or workflow_hourly_micros > 0),
  check (workflow_daily_micros is null or workflow_daily_micros > 0),
  check (warn_percent is null or warn_percent between 50 and 99),
  check (
    (workflow_id is null and run_cap_micros is not null and org_hourly_micros is not null
      and org_daily_micros is not null and workflow_hourly_micros is not null
      and workflow_daily_micros is not null and warn_percent is not null)
    or
    (workflow_id is not null and run_cap_micros is null and org_hourly_micros is null
      and org_daily_micros is null and warn_percent is null)
  )
);

create unique index if not exists spend_limit_org_unique
  on spend_limit (org_id) where workflow_id is null;
create unique index if not exists spend_limit_workflow_unique
  on spend_limit (org_id, workflow_id) where workflow_id is not null;

create table if not exists spend_reservation (
  id uuid primary key default gen_random_uuid(),
  org_id text not null references organization(id) on delete cascade,
  work_run_id uuid unique references work_run(id) on delete cascade,
  workflow_id uuid references workflow_definition(id) on delete set null,
  source text not null check (source in (
    'chat', 'channel', 'cron', 'trigger', 'api', 'webhook', 'metric', 'system')),
  reserved_micros bigint not null check (reserved_micros >= 0),
  created_at timestamptz not null default now(),
  released_at timestamptz
);

create index if not exists spend_reservation_open_idx
  on spend_reservation (org_id) where released_at is null;

create table if not exists spend_ledger (
  id bigint generated always as identity primary key,
  org_id text not null references organization(id) on delete cascade,
  reservation_id uuid references spend_reservation(id) on delete set null,
  work_run_id uuid references work_run(id) on delete set null,
  workflow_id uuid references workflow_definition(id) on delete set null,
  source text not null check (source in (
    'chat', 'channel', 'cron', 'trigger', 'api', 'webhook', 'metric', 'system')),
  provider text,
  model text,
  cost_micros bigint not null check (cost_micros >= 0),
  tokens bigint check (tokens is null or tokens >= 0),
  priced text not null check (priced in ('billed', 'estimated', 'included', 'fallback')),
  cost_source text,
  pricing_version text,
  created_at timestamptz not null default now()
);

create index if not exists spend_ledger_org_created_idx
  on spend_ledger (org_id, created_at);
create index if not exists spend_ledger_workflow_created_idx
  on spend_ledger (workflow_id, created_at) where workflow_id is not null;
create index if not exists spend_ledger_reservation_idx
  on spend_ledger (reservation_id) where reservation_id is not null;

create or replace function seed_spend_limit(p_org_id text) returns void as $$
begin
  insert into spend_limit (
    org_id, run_cap_micros, org_hourly_micros, org_daily_micros,
    workflow_hourly_micros, workflow_daily_micros, warn_percent
  ) values (p_org_id, 5000000, 200000000, 500000000, 50000000, 200000000, 80)
  on conflict (org_id) where workflow_id is null do nothing;
end;
$$ language plpgsql;

create or replace function organization_seed_spend_limit() returns trigger as $$
begin
  perform seed_spend_limit(NEW.id);
  return NEW;
end;
$$ language plpgsql;

drop trigger if exists organization_seed_spend_limit_trigger on organization;
create trigger organization_seed_spend_limit_trigger
  after insert on organization
  for each row execute function organization_seed_spend_limit();

select seed_spend_limit(id) from organization;
