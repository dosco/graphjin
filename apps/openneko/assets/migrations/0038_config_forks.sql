-- CV4 — git forks/branches. memory_fork = per-member fork baseline for
-- 3-way pulls; config_change = audit + admin adopt inbox for config
-- artifacts (attribution lives here, DB-deletable, never in git — see
-- docs/DATA_LIFECYCLE.md §3).

CREATE TABLE IF NOT EXISTS memory_fork (
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  user_id text NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
  baseline_sha text NOT NULL DEFAULT '',
  baseline_at timestamptz NOT NULL DEFAULT now(),
  frozen boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, user_id)
);

CREATE TABLE IF NOT EXISTS config_change (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  artifact_kind text NOT NULL,
  artifact_ref text NOT NULL,
  scope text NOT NULL DEFAULT 'team',
  user_id text NOT NULL DEFAULT '',
  actor_user_id text REFERENCES app_user(id) ON DELETE SET NULL,
  commit_sha text,
  summary text NOT NULL DEFAULT '',
  semantic_diff jsonb,
  status text NOT NULL DEFAULT 'recorded',
  decided_by text,
  decided_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS config_change_org_idx
  ON config_change (org_id, created_at DESC);
CREATE INDEX IF NOT EXISTS config_change_org_status_idx
  ON config_change (org_id, status, created_at DESC);
