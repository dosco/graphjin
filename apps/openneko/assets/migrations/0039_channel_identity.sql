-- CH3 — channel→app_user mapping. One row per channel-native identity
-- seen by an org; linking binds it to an app_user (SSO email match or
-- admin-map). Unlinked identities stay anonymous member-grade.

CREATE TABLE IF NOT EXISTS channel_identity (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  channel_plugin text NOT NULL,
  workspace_id text NOT NULL DEFAULT '',
  channel_user_id text NOT NULL,
  app_user_id text REFERENCES app_user(id) ON DELETE CASCADE,
  display_name text,
  email text,
  status text NOT NULL DEFAULT 'unverified',
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  verified_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS channel_identity_tuple_unique
  ON channel_identity (org_id, channel_plugin, workspace_id, channel_user_id);
CREATE INDEX IF NOT EXISTS channel_identity_org_user_idx
  ON channel_identity (org_id, app_user_id);
