-- Personal OAuth credentials never share a pack's deployment token slot.
CREATE TABLE IF NOT EXISTS pack_user_connection (
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  user_id text NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
  pack_install_id uuid NOT NULL REFERENCES pack_install(id) ON DELETE CASCADE,
  connection_key text NOT NULL,
  revision uuid NOT NULL DEFAULT gen_random_uuid(),
  account_id text,
  account_label text,
  credentials text,
  pending_state_hash text,
  pending_expires_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, user_id, pack_install_id, connection_key),
  CHECK (credentials IS NULL OR credentials LIKE 'enc:v1:%')
);
