-- Reckon-compatible webhook runs need more tokens per run than the original
-- API ceiling allowed. The default stays where it is.
alter table workflow_api_access
  drop constraint if exists workflow_api_access_max_tokens_per_run_check;
alter table workflow_api_access
  add constraint workflow_api_access_max_tokens_per_run_check
  check (max_tokens_per_run between 1000 and 3000000);
