-- Run telemetry in API poll responses is opt-in per workflow.
alter table workflow_api_access
  add column if not exists include_telemetry boolean not null default false;
