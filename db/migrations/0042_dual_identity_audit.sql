-- SEC5 — dual-identity audit. Every action request snapshots its acting
-- principal (the human side: actor_user_id/actor_role, K1-style) AND the
-- agent side (actor_backend) at creation, so the audit trail answers
-- "who, via which agent" without joins against mutable rows.
-- control_plane_audit records every authenticated gateway (broker) call
-- a sandboxed agent makes, with the same dual identity.

ALTER TABLE action_request
  ADD COLUMN IF NOT EXISTS actor_user_id text,
  ADD COLUMN IF NOT EXISTS actor_role text,
  ADD COLUMN IF NOT EXISTS actor_backend text;

CREATE TABLE IF NOT EXISTS control_plane_audit (
  id bigserial PRIMARY KEY,
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  run_id uuid,
  path text NOT NULL,
  actor_user_id text,
  actor_role text,
  backend text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS control_plane_audit_org_idx
  ON control_plane_audit (org_id, created_at DESC);
CREATE INDEX IF NOT EXISTS control_plane_audit_run_idx
  ON control_plane_audit (run_id);
