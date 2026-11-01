-- Fire pg_notify('work_run_event', run_id::text) on every work_run_event
-- insert so the SSE replay endpoint can wake immediately instead of
-- polling every 250ms. Listeners check the payload against their runId
-- and pull new rows; the polling loop in the SSE handler becomes a
-- keepalive backstop only.

create or replace function notify_work_run_event() returns trigger as $$
begin
  perform pg_notify('work_run_event', NEW.run_id::text);
  return NEW;
end;
$$ language plpgsql;

drop trigger if exists work_run_event_notify_trigger on work_run_event;
create trigger work_run_event_notify_trigger
  after insert on work_run_event
  for each row execute function notify_work_run_event();
