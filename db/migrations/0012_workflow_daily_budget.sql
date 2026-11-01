-- M5 scale: per-workflow daily run budget. Null = unlimited (current
-- behaviour). Set by author via the workflow_definition CRUD path.

alter table workflow_definition
  add column if not exists daily_run_budget integer;
