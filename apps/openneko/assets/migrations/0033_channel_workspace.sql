-- CH2 — channel workspace → org mapping. Inbound updates resolved the
-- single admin org unconditionally; with the CH1 sender carrying a
-- workspace scope (Slack team_id, WhatsApp business number), each
-- workspace maps to its org. First contact auto-binds to the default
-- org (single-tenant semantics preserved); multi-tenant installs remap
-- rows explicitly.

CREATE TABLE IF NOT EXISTS channel_workspace (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  channel_plugin text NOT NULL,
  workspace_id text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS channel_workspace_plugin_ws_unique
  ON channel_workspace (channel_plugin, workspace_id);
CREATE INDEX IF NOT EXISTS channel_workspace_org_idx
  ON channel_workspace (org_id);
