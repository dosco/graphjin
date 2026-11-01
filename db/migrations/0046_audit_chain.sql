-- SEC10 — tamper-resistant audit log. An append-only per-org hash chain
-- over the governance entities (action_request transitions, action
-- executions, work-run lifecycle). Each link binds the previous link's
-- hash, so any retroactive edit or deletion breaks every later link;
-- verifyAuditChain recomputes the chain and reports the first break.
-- Rows carry the recorded payload for SIEM export.

CREATE TABLE IF NOT EXISTS audit_chain (
  id bigserial PRIMARY KEY,
  org_id text NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
  seq integer NOT NULL,
  entity_kind text NOT NULL,
  entity_id text NOT NULL,
  event text NOT NULL,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  payload_hash text NOT NULL,
  prev_hash text NOT NULL,
  chain_hash text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS audit_chain_org_seq_unique
  ON audit_chain (org_id, seq);
CREATE INDEX IF NOT EXISTS audit_chain_org_entity_idx
  ON audit_chain (org_id, entity_kind, entity_id);
