-- Deterministic saved-query metrics. Existing agent-authored metrics retain the
-- current execution mode; pack metrics opt into a validated declarative query.

alter table metric
  add column if not exists execution_mode text not null default 'agent',
  add column if not exists definition_json jsonb,
  add column if not exists definition_version integer,
  add column if not exists definition_hash text;

alter table metric
  add constraint metric_execution_mode_check
  check (execution_mode in ('agent', 'saved_query'));

alter table metric
  add constraint metric_saved_query_definition_check
  check (
    execution_mode = 'agent'
    or (
      definition_json is not null
      and definition_version is not null
      and definition_version > 0
      and definition_hash is not null
    )
  );

create index if not exists metric_saved_query_active_idx
  on metric (org_id, cadence, updated_at)
  where active = true and execution_mode = 'saved_query';

alter table metric_snapshot
  add column if not exists definition_hash text,
  add column if not exists source_freshness_at timestamptz,
  add column if not exists duration_ms integer,
  add column if not exists error_class text;
