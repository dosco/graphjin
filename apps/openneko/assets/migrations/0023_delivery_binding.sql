-- delivery_binding: routes a workflow output (for an audience) to a channel
-- plugin's deliver RPC. The web channel is implicit/always-on; rows here add
-- additional membranes (Telegram, Slack, …) per the V2 frontend-as-capability
-- design. `recipient` is the channel-native address minted at config time.
CREATE TABLE IF NOT EXISTS delivery_binding (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id TEXT NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  audience TEXT NOT NULL DEFAULT '*',
  channel_plugin TEXT NOT NULL,
  recipient JSONB NOT NULL DEFAULT '{}'::jsonb,
  filter JSONB,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS delivery_binding_org_enabled_idx
  ON delivery_binding (org_id, enabled);
