-- One spend alert per kind, subject and window, even when writers race.
create unique index if not exists behavior_alert_spend_window_unique
  on behavior_alert (org_id, kind, subject, (details->>'windowStart'))
  where kind like 'spend.%';
