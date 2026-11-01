-- OL4 — watchers (polling v1). A watcher runs a GraphJin query on a
-- cadence, evaluates a condition over one value in the result, and
-- fires its linked workflow when the condition holds — "what condition
-- matters?" instead of cron's "what time should I check?". Debounce
-- stops a persistent condition from re-firing every sweep.

CREATE TABLE IF NOT EXISTS watcher (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  workflow_id uuid NOT NULL REFERENCES workflow_definition(id) ON DELETE CASCADE,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  enabled boolean NOT NULL DEFAULT true,
  query text NOT NULL,
  value_path text NOT NULL,
  op text NOT NULL,
  threshold jsonb,
  cadence_seconds integer NOT NULL DEFAULT 300,
  debounce_seconds integer NOT NULL DEFAULT 3600,
  severity text NOT NULL DEFAULT 'medium',
  last_checked_at timestamptz,
  last_fired_at timestamptz,
  last_value jsonb,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS watcher_org_name_unique
  ON watcher (org_id, name);
CREATE INDEX IF NOT EXISTS watcher_org_enabled_idx
  ON watcher (org_id, enabled, last_checked_at);
