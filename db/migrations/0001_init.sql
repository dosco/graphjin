-- =========================================================
-- Metadata DB — squashed baseline
-- Current schema as of pre-ship migration squash.
-- =========================================================

-- ---------- 1. Tenant ----------
create table organization (
  id              text primary key,
  scalekit_org_id text,
  name            text not null,
  domain          text,
  features        text not null default '',
  status          text not null default 'active',
  created_at      timestamptz not null default now(),
  updated_at      timestamptz not null default now()
);
create unique index organization_domain_unique
  on organization(domain) where domain is not null;

-- ---------- 2. Users ----------
create table app_user (
  id            text primary key,
  sub           text,
  email         text not null,
  name          text,
  org_id        text not null references organization(id) on delete cascade,
  role          text not null,
  created_at    timestamptz not null default now(),
  updated_at    timestamptz not null default now(),
  last_login_at timestamptz
);
create index app_user_org_idx on app_user(org_id);

-- ---------- 3. Org data source ----------
create table data_source (
  id               uuid primary key default gen_random_uuid(),
  org_id           text not null references organization(id) on delete cascade,
  kind             text not null,
  graphql_url      text not null,
  subscription_url text,
  label            text,
  created_at       timestamptz not null default now(),
  updated_at       timestamptz not null default now(),
  mcp_url          text
);
create index data_source_org_idx on data_source(org_id);

-- ---------- 4. Onboarding wizard ----------
create table onboarding_wizard (
  org_id                  text primary key references organization(id) on delete cascade,
  company_note            text,
  fiscal_year_start_month smallint,
  active_seats            text[] not null default '{}',
  priorities              text[] not null default '{}',
  step                    text not null default 'company',
  submitted_at            timestamptz,
  created_at              timestamptz not null default now(),
  updated_at              timestamptz not null default now()
);

-- ---------- 5. Processing jobs ----------
create table processing_job (
  id              uuid primary key default gen_random_uuid(),
  org_id          text not null references organization(id) on delete cascade,
  kind            text not null,
  status          text not null default 'queued',
  trigger         text not null,
  trigger_payload jsonb,
  progress        jsonb not null default '{}',
  result          jsonb,
  error           text,
  started_at      timestamptz,
  finished_at     timestamptz,
  created_at      timestamptz not null default now(),
  updated_at      timestamptz not null default now()
);
create index processing_job_org_status_idx on processing_job(org_id, kind, status);
create index processing_job_org_recent_idx on processing_job(org_id, created_at desc);
comment on column processing_job.kind is
  'business_profile_build | industry_insights_build | metric_refresh';

-- ---------- 6. Customer profile ----------
create table customer_profile (
  id                               uuid primary key default gen_random_uuid(),
  org_id                           text not null references organization(id) on delete cascade,
  version                          int not null,
  is_current                       boolean not null default false,
  company_note                     text,
  fiscal_year_start_month          smallint,
  declared_priorities              text[] not null default '{}',
  industry_insights                text,
  built_at                         timestamptz not null default now(),
  built_by_job                     uuid,
  created_at                       timestamptz not null default now(),
  updated_at                       timestamptz not null default now(),
  business_profile                 text,
  industry_insights_research_task  text,
  unique (org_id, version)
);
create unique index one_current_profile_per_org
  on customer_profile(org_id) where is_current;
comment on column customer_profile.industry_insights_research_task is
  'Mission charter handed to Perplexity sonar-deep-research to produce industry_insights. Written by industry_insights_build.';

-- ---------- 7. Metrics ----------
create table metric (
  id             uuid primary key default gen_random_uuid(),
  org_id         text not null references organization(id) on delete cascade,
  role           text not null,
  slug           text not null,
  source         text not null,
  title          text not null,
  description    text,
  why            text,
  chart_hint     text,
  unit           text,
  direction_good text,
  cadence        text not null default 'daily',
  active         boolean not null default true,
  created_by_job uuid references processing_job(id),
  created_at     timestamptz not null default now(),
  updated_at     timestamptz not null default now(),
  unique (org_id, role, slug)
);
create index metric_org_role_active_idx
  on metric(org_id, role) where active;

create table metric_snapshot (
  id          uuid primary key default gen_random_uuid(),
  metric_id   uuid not null references metric(id) on delete cascade,
  captured_at timestamptz not null default now(),
  value       numeric,
  value_json  jsonb,
  baseline    numeric,
  delta_pct   numeric,
  status      text,
  created_at  timestamptz not null default now(),
  payload     jsonb
);
create index metric_snapshot_metric_recent_idx
  on metric_snapshot(metric_id, captured_at desc);

create table dashboard_pin (
  id          uuid primary key default gen_random_uuid(),
  org_id      text not null,
  role        text not null,
  metric_id   uuid not null references metric(id) on delete cascade,
  sort_order  int not null default 0,
  created_at  timestamptz not null default now(),
  unique (org_id, role, metric_id)
);

-- ---------- 8. Briefings ----------
create table briefing (
  id               uuid primary key default gen_random_uuid(),
  org_id           text not null references organization(id) on delete cascade,
  role             text not null,
  for_date         date not null,
  profile_version  int not null,
  summary_md       text not null,
  insights         jsonb not null default '[]',
  generated_by_job uuid references processing_job(id),
  created_at       timestamptz not null default now(),
  updated_at       timestamptz not null default now(),
  unique (org_id, role, for_date)
);
create index briefing_org_recent_idx on briefing(org_id, for_date desc);

-- ---------- 9. Source-change tracking ----------
create table source_change_log (
  id          bigserial primary key,
  org_id      text not null references organization(id) on delete cascade,
  source_id   uuid not null references data_source(id) on delete cascade,
  table_name  text not null,
  change_kind text not null,
  observed_at timestamptz not null default now(),
  payload     jsonb
);
create index source_change_log_org_recent_idx
  on source_change_log(org_id, observed_at desc);

create table reprofile_debounce (
  org_id         text primary key references organization(id) on delete cascade,
  last_change_at timestamptz not null,
  last_built_at  timestamptz,
  pending_job_id uuid references processing_job(id),
  updated_at     timestamptz not null default now()
);

-- ---------- 10. Provider configuration ----------
create table llm_provider_config (
  id          uuid primary key default gen_random_uuid(),
  org_id      text not null references organization(id) on delete cascade,
  scope       text not null,
  provider    text not null,
  model       text,
  label       text,
  enabled     boolean not null default true,
  config      jsonb not null default '{}',
  secrets     jsonb not null default '{}',
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now(),
  unique (org_id, scope)
);
create index llm_provider_config_org_idx on llm_provider_config(org_id);
comment on table llm_provider_config is
  'Per-org model provider settings. Secrets may be plaintext or app-encrypted depending on APP_SECRET_KEY.';
