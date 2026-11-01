-- CV0 — config-vcs: invisible auto-versioning of skills/workflows/memory
-- snapshots in a git repo per org (working tree = the org agents dir).
-- The DB holds ref pointers; git holds content. Phase 0 writes only the
-- team layer (scope='team', user_id=''); user/<id> layers arrive with
-- CV4 under the docs/DATA_LIFECYCLE.md §3 constraints.
-- user_id is '' (not NULL) for the team layer so the unique index is a
-- plain column tuple both writers can upsert against.

CREATE TABLE IF NOT EXISTS config_ref (
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  scope text NOT NULL DEFAULT 'team',
  user_id text NOT NULL DEFAULT '',
  commit_sha text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS config_ref_org_scope_user_unique
  ON config_ref (org_id, scope, user_id);
