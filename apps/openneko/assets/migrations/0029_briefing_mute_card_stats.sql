-- OL7 — operator-controlled muting. A muted scope filters matching
-- workflow_output cards out of the Briefing tributaries until
-- muted_until passes; workflow "pause for today" parks enabled=false
-- with a re-enable timer the cron sweep honors.
--
-- OL2 — briefing_card: observation-elevation. An observation (consumer-
-- side row; may have no producing output at all, e.g. external events)
-- can be promoted onto the Briefing as a first-class card keyed by
-- source_observation_id, instead of riding piggyback on an output pin.

CREATE TABLE IF NOT EXISTS muted_scope (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  scope text NOT NULL,
  muted_until timestamptz NOT NULL,
  muted_by_user_id text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS muted_scope_org_scope_unique
  ON muted_scope (org_id, scope);
CREATE INDEX IF NOT EXISTS muted_scope_org_until_idx
  ON muted_scope (org_id, muted_until DESC);

ALTER TABLE workflow_definition
  ADD COLUMN IF NOT EXISTS paused_until timestamptz;

CREATE TABLE IF NOT EXISTS briefing_card (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  source_observation_id uuid NOT NULL REFERENCES observation(id) ON DELETE CASCADE,
  title text,
  body text,
  mood text,
  status text NOT NULL DEFAULT 'active',
  elevated_by text NOT NULL DEFAULT 'system',
  elevated_by_user_id text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS briefing_card_org_observation_unique
  ON briefing_card (org_id, source_observation_id);
CREATE INDEX IF NOT EXISTS briefing_card_org_status_idx
  ON briefing_card (org_id, status, created_at DESC);
