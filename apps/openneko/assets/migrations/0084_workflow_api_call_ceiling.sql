-- A workflow can require more than 128 GraphJin/cache calls in a 30-minute run.
alter table workflow_api_access
  drop constraint if exists workflow_api_access_max_model_calls_check;
alter table workflow_api_access
  add constraint workflow_api_access_max_model_calls_check
  check (max_model_calls between 1 and 1024);

alter table workflow_api_access
  drop constraint if exists workflow_api_access_max_tool_calls_check;
alter table workflow_api_access
  add constraint workflow_api_access_max_tool_calls_check
  check (max_tool_calls between 1 and 1024);

create table if not exists workflow_api_org_limits (
  org_id text primary key references organization(id) on delete cascade,
  rolling_token_budget integer not null check (rolling_token_budget between 1000 and 100000000),
  rolling_cost_micros_budget bigint not null check (rolling_cost_micros_budget between 1000 and 10000000000),
  updated_at timestamptz not null default now()
);
