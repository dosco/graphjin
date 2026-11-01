-- An organization-wide limit on scheduled metric refreshes. Null keeps each
-- card's own cadence.
alter table spend_limit add column if not exists metric_refresh text;
alter table spend_limit drop constraint if exists spend_limit_metric_refresh_check;
alter table spend_limit add constraint spend_limit_metric_refresh_check
  check (metric_refresh is null or (workflow_id is null and metric_refresh in ('daily', 'weekly', 'off')));
