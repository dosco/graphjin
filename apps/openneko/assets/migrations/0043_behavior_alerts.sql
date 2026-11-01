-- SEC7 — behavioral thresholds. A sweep over the SEC5 audit stream
-- (control_plane_audit), action_request and work_memory_event rates
-- raises alerts when an agent's behavior departs from its envelope —
-- the "would we know within an hour if an agent went rogue?" answer.

CREATE TABLE IF NOT EXISTS behavior_alert (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  kind text NOT NULL,
  subject text NOT NULL DEFAULT '',
  observed integer NOT NULL,
  threshold integer NOT NULL,
  window_seconds integer NOT NULL,
  details jsonb,
  acknowledged_at timestamptz,
  acknowledged_by text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS behavior_alert_org_idx
  ON behavior_alert (org_id, created_at DESC);
CREATE INDEX IF NOT EXISTS behavior_alert_org_kind_subject_idx
  ON behavior_alert (org_id, kind, subject, created_at DESC);
