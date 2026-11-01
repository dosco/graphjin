-- CV3 — personas. One operator_profile per (org, user): a role template
-- (free text — the closed exec enum stops being the only shape), focus
-- areas, raw onboarding answers, and the compiled brief the agent reads
-- as an <operator-profile> prompt block. user_id = '' is the org-default
-- persona (solo profile / unlinked channels); per-user rows arrive with
-- auth. No FK on user_id so the '' default row is representable;
-- offboarding cascade handles cleanup (docs/DATA_LIFECYCLE.md §2).
--
-- metric.user_id: per-persona briefing cards — NULL = org-shared card.

CREATE TABLE IF NOT EXISTS operator_profile (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  user_id text NOT NULL DEFAULT '',
  display_name text,
  role_template text NOT NULL DEFAULT '',
  focus_areas text[] NOT NULL DEFAULT '{}',
  answers jsonb NOT NULL DEFAULT '{}'::jsonb,
  brief_md text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS operator_profile_org_user_unique
  ON operator_profile (org_id, user_id);

ALTER TABLE metric
  ADD COLUMN IF NOT EXISTS user_id text;

CREATE INDEX IF NOT EXISTS metric_org_user_idx
  ON metric (org_id, user_id);
